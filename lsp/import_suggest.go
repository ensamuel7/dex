package lsp

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/lexer"
	"github.com/ensamuel7/dex/parser"
	"github.com/ensamuel7/dex/stdlib"
)

// An unqualified call in Dex is only ever a local function, so `println("hi")`
// or `userService.init()` without the matching import is a hard error even
// though the name exists one import line away. The helpers here find the module
// that would make such a name resolve, so completion can offer it and a code
// action can insert the import.

// importCandidate is one module that exports a name the document refers to.
type importCandidate struct {
	path     string // import path, as it appears in the import line
	name     string // how the module is referenced in code (its last segment)
	isStdlib bool
	detail   string // the function's signature, for the completion detail line
	doc      string
}

// maxModuleScanFiles bounds the .dx walk so a stray deep tree cannot stall the
// editor on every keystroke.
const maxModuleScanFiles = 400

// userModuleFiles maps every .dx file under sourceDir to the import path that
// reaches it, which is its path relative to sourceDir without the extension —
// the same spelling resolve.ResolveUserModules expects.
func userModuleFiles(sourceDir string) map[string]string {
	out := map[string]string{}
	if sourceDir == "" {
		return out
	}
	filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			base := d.Name()
			if path != sourceDir && (strings.HasPrefix(base, ".") || base == "build" || base == "node_modules") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".dx") {
			return nil
		}
		if len(out) >= maxModuleScanFiles {
			return fs.SkipAll
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return nil
		}
		out[strings.TrimSuffix(filepath.ToSlash(rel), ".dx")] = path
		return nil
	})
	return out
}

// parseModuleFile reads and parses one .dx file, returning nil if it does not
// parse — a half-written module should not produce half-built suggestions.
func parseModuleFile(path string) *ast.Program {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lex := lexer.New(string(source))
	tokens, err := lex.Tokenize()
	if err != nil {
		return nil
	}
	p := parser.New(tokens)
	seedParserModuleTypes(p, tokens, filepath.Dir(path))
	program, errs := p.Parse()
	if len(errs) > 0 {
		return nil
	}
	return program
}

// exportsFunc reports whether a parsed module offers fnName to importers.
func exportsFunc(program *ast.Program, fnName string) (*ast.Function, bool) {
	for i := range program.Functions {
		fn := &program.Functions[i]
		if fn.Name == fnName && fn.Name != "main" && !fn.IsPrivate {
			return fn, true
		}
	}
	return nil, false
}

// stdlibSignature renders a stdlib function's parameter list and return type,
// honouring the hand-written signatures polymorphic builtins carry.
func stdlibSignature(moduleName, fnName string, fdef *stdlib.FuncDef) string {
	paramStr := ""
	retStr := typeName(fdef.ReturnType)
	if sp, sr, ok := stdlib.SpecialSignature(moduleName, fnName, fdef); ok {
		paramStr = sp
		retStr = sr
	} else {
		var params []string
		for i, p := range fdef.Params {
			pname := fmt.Sprintf("arg%d", i+1)
			if i < len(fdef.ParamNames) {
				pname = fdef.ParamNames[i]
			}
			params = append(params, fmt.Sprintf("%s: %s", pname, typeName(p)))
		}
		paramStr = strings.Join(params, ", ")
	}
	return fmt.Sprintf("(%s): %s", paramStr, retStr)
}

// sortedKeys returns a map's keys in a stable order, so the same unqualified
// name always suggests the same module first.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// modulesExporting returns every module that would make fnName resolve: the
// stdlib modules defining it, then the user modules under sourceDir that export
// it. selfPath is the open document, excluded because its own functions are
// already in scope.
func modulesExporting(fnName, sourceDir, selfPath string) []importCandidate {
	var out []importCandidate

	modNames := make([]string, 0, len(stdlib.AllModules()))
	for name := range stdlib.AllModules() {
		modNames = append(modNames, name)
	}
	sort.Strings(modNames)
	for _, name := range modNames {
		mod := stdlib.Lookup(name)
		fdef, ok := mod.Funcs[fnName]
		if !ok {
			continue
		}
		out = append(out, importCandidate{
			path:     name,
			name:     name,
			isStdlib: true,
			detail:   stdlibSignature(name, fnName, &fdef),
			doc:      fdef.Doc,
		})
	}

	files := userModuleFiles(sourceDir)
	for _, impPath := range sortedKeys(files) {
		file := files[impPath]
		if sameFile(file, selfPath) {
			continue
		}
		program := parseModuleFile(file)
		if program == nil {
			continue
		}
		fn, ok := exportsFunc(program, fnName)
		if !ok {
			continue
		}
		out = append(out, importCandidate{
			path:   impPath,
			name:   filepath.Base(impPath),
			detail: formatFuncSignature(fn),
		})
	}

	return out
}

// userModuleNamed finds the .dx file a module name refers to, preferring the
// shallowest path so `config` means ./config.dx rather than a/b/config.dx.
func userModuleNamed(moduleName, sourceDir, selfPath string) (impPath string, file string, ok bool) {
	files := userModuleFiles(sourceDir)
	for _, candidate := range sortedKeys(files) {
		if filepath.Base(candidate) != moduleName {
			continue
		}
		if sameFile(files[candidate], selfPath) {
			continue
		}
		if !ok || strings.Count(candidate, "/") < strings.Count(impPath, "/") {
			impPath, file, ok = candidate, files[candidate], true
		}
	}
	return impPath, file, ok
}

// sameFile compares two paths after resolving them, so the open document is
// recognised however the client spelled its URI.
func sameFile(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return absA == absB
}

// stdlibModuleExists reports whether a name is a stdlib module.
func stdlibModuleExists(name string) bool {
	return stdlib.Lookup(name) != nil
}
