package resolve

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/lexer"
	"github.com/ensamuel7/dex/parser"
	"github.com/ensamuel7/dex/stdlib"
	"github.com/ensamuel7/dex/token"
)

// ExtractImportPaths scans raw tokens for import declarations and returns their paths.
func ExtractImportPaths(tokens []token.Token) []string {
	var paths []string
	for _, ref := range ExtractImports(tokens) {
		paths = append(paths, ref.Path)
	}
	return paths
}

// ImportRef is an import's path together with the module name it asked to be
// known by, when it gave one.
type ImportRef struct {
	Path  string
	Alias string
	Line  int
	Col   int
}

// ExtractImports scans raw tokens for import declarations, keeping any alias.
// A module's own imports are read from tokens rather than from a parsed program,
// which is why dropping the alias here used to make `import "x" as "y"` work in
// the main file and nowhere else.
func ExtractImports(tokens []token.Token) []ImportRef {
	var refs []ImportRef
	for i := 0; i < len(tokens)-1; i++ {
		if tokens[i].Kind != token.TokenImport || tokens[i+1].Kind != token.TokenString {
			continue
		}
		ref := ImportRef{Path: tokens[i+1].Value, Line: tokens[i].Line, Col: tokens[i].Col}
		if i+3 < len(tokens) && tokens[i+2].Kind == token.TokenAs && tokens[i+3].Kind == token.TokenString {
			ref.Alias = tokens[i+3].Value
		}
		refs = append(refs, ref)
	}
	return refs
}

// own records which file a merged global came from. Two modules may share a
// name — `model/charging` and `service/charging` both merge under `charging_`
// — and that is only a problem when both define the same thing. Catching it
// here names the two files and the symbol; letting it through named neither,
// because it surfaced as a redefinition in the generated C.
func own(owners map[string]string, flatName, absPath, moduleName string) error {
	if prev, taken := owners[flatName]; taken && prev != absPath {
		bare := strings.TrimPrefix(flatName, moduleName+"_")
		return fmt.Errorf("module '%s' is provided by two files and both define '%s':\n  %s\n  %s\n"+
			"a module's name is its file's base name, so give one of them another: "+
			"import \"...\" as \"<name>\"", moduleName, bare, prev, absPath)
	}
	owners[flatName] = absPath
	return nil
}

// A ModuleType is a type a user module declares: the module that declares it,
// and the name as written there. A parser is told about these so a reference
// resolves — qualified, or unqualified when nothing else in scope declares the
// same name.
type ModuleType struct {
	Module string
	Name   string
	// Kind is "struct", "enum" or "interface". Empty means struct, so the
	// zero value stays what it was.
	Kind string
}

// Registers struct and enum declarations from user modules globally so the main
// file's parser recognizes their literal and type-annotation syntax.
//
// Each registers under the module that declares it — `alpha_Response` — the way
// a function declared there becomes `alpha_make`. That is what lets two modules
// each declare a `Response`: they are two types, and the generated C names
// differ without any further work.
//
// The walk is transitive, and so is the set it returns: a file has always been
// able to name a type from a module it reaches only indirectly, and restricting
// that now would break code that reads perfectly well.
func PreRegisterUserStructs(imports []ImportRef, sourceDir string) []ModuleType {
	var found []ModuleType
	visited := map[string]bool{}
	mod, _ := FindMod(sourceDir)
	ids := moduleIdentities(mod, imports, sourceDir)
	for _, ref := range imports {
		if stdlib.Lookup(ref.Path) != nil {
			continue
		}
		if file, err := importFile(mod, sourceDir, ref.Path); err == nil {
			preRegisterStructsFromFile(mod, file, ids, visited, &found)
		}
	}
	return found
}

// localName is the name an import is known by: its alias, or its file's
// base name.
func localName(ref ImportRef) string {
	if ref.Alias != "" {
		return ref.Alias
	}
	return filepath.Base(ref.Path)
}

func moduleIdentities(mod *Mod, imports []ImportRef, sourceDir string) map[string]string {
	ids := map[string]string{}
	taken := map[string]bool{}
	var visit func(refs []ImportRef, dir string)
	visit = func(refs []ImportRef, dir string) {
		for _, ref := range refs {
			if stdlib.Lookup(ref.Path) != nil {
				continue
			}
			file, err := importFile(mod, dir, ref.Path)
			if err != nil {
				continue
			}
			abs, err := filepath.Abs(file)
			if err != nil {
				continue
			}
			if _, seen := ids[abs]; seen {
				continue
			}
			id := localName(ref)
			for n := 2; taken[id]; n++ {
				id = fmt.Sprintf("%s_%d", localName(ref), n)
			}
			ids[abs] = id
			taken[id] = true
			source, err := os.ReadFile(abs)
			if err != nil {
				continue
			}
			tokens, err := lexer.New(string(source)).Tokenize()
			if err != nil {
				continue
			}
			visit(ExtractImports(tokens), filepath.Dir(abs))
		}
	}
	visit(imports, sourceDir)
	return ids
}

func fileAliases(mod *Mod, refs []ImportRef, dir, file string, ids map[string]string) (map[string]string, error) {
	aliases := map[string]string{}
	byName := map[string]string{}
	for _, ref := range refs {
		if stdlib.Lookup(ref.Path) != nil {
			continue
		}
		target, err := importFile(mod, dir, ref.Path)
		if err != nil {
			continue
		}
		abs, err := filepath.Abs(target)
		if err != nil {
			continue
		}
		local := localName(ref)
		if prev, dup := byName[local]; dup && prev != abs {
			return nil, fmt.Errorf("%s:%d:%d: two imports are both named '%s' — give one another name: import \"%s\" as \"<name>\"",
				file, ref.Line, ref.Col, local, ref.Path)
		}
		byName[local] = abs
		if id, ok := ids[abs]; ok && id != local {
			aliases[local] = id
		}
	}
	return aliases, nil
}

func ModuleAliases(imports []ImportRef, sourceDir, file string) (map[string]string, error) {
	mod, err := FindMod(sourceDir)
	if err != nil {
		return nil, err
	}
	return fileAliases(mod, imports, sourceDir, file, moduleIdentities(mod, imports, sourceDir))
}

func preRegisterStructsFromFile(mod *Mod, filePath string, ids map[string]string, visited map[string]bool, found *[]ModuleType) {
	absPath, err := filepath.Abs(filePath)
	if err != nil || visited[absPath] {
		return
	}
	visited[absPath] = true
	module, ok := ids[absPath]
	if !ok {
		return
	}

	source, err := os.ReadFile(filePath)
	if err != nil {
		return
	}

	lex := lexer.New(string(source))
	tokens, err := lex.Tokenize()
	if err != nil {
		return
	}

	// Placeholders; the full definitions are filled in when the modules are
	// parsed, and registration is idempotent.
	for i := 0; i < len(tokens)-1; i++ {
		if tokens[i].Kind == token.TokenStruct && tokens[i+1].Kind == token.TokenIdent {
			name := tokens[i+1].Value
			*found = append(*found, ModuleType{Module: module, Name: name, Kind: "struct"})
			ast.RegisterStructType(ast.StructDef{Name: ast.QualifiedStructName(module, name), Module: module})
		}
		if tokens[i].Kind == token.TokenEnum && tokens[i+1].Kind == token.TokenIdent {
			name := tokens[i+1].Value
			*found = append(*found, ModuleType{Module: module, Name: name, Kind: "enum"})
			ast.RegisterEnumType(ast.EnumDef{Name: ast.QualifiedStructName(module, name), Module: module})
		}
		if tokens[i].Kind == token.TokenInterface && tokens[i+1].Kind == token.TokenIdent {
			name := tokens[i+1].Value
			*found = append(*found, ModuleType{Module: module, Name: name, Kind: "interface"})
			ast.RegisterInterfaceType(ast.InterfaceDef{Name: ast.QualifiedStructName(module, name), Module: module})
		}
	}

	modDir := filepath.Dir(absPath)
	for _, ref := range ExtractImports(tokens) {
		if stdlib.Lookup(ref.Path) != nil {
			continue
		}
		if file, err := importFile(mod, modDir, ref.Path); err == nil {
			preRegisterStructsFromFile(mod, file, ids, visited, found)
		}
	}
}

// SeedParserTypes tells a parser about every type registered so far, each under
// the module that declared it. A type with no module — the main program's own,
// and the struct types a stdlib module provides — is seeded by its bare name.
func SeedParserTypes(p *parser.Parser) {
	for _, name := range ast.AllStructNames() {
		seedOne(p, name, "struct")
	}
	for _, name := range ast.AllEnumNames() {
		seedOne(p, name, "enum")
	}
	for _, name := range ast.AllInterfaceNames() {
		seedOne(p, name, "interface")
	}
}

func seedOne(p *parser.Parser, flat, kind string) {
	module := ""
	switch kind {
	case "enum":
		if t, ok := ast.LookupEnumType(flat); ok {
			if def := ast.GetEnumDef(t); def != nil {
				module = def.Module
			}
		}
	case "interface":
		if t, ok := ast.LookupInterfaceType(flat); ok {
			if def := ast.GetInterfaceDef(t); def != nil {
				module = def.Module
			}
		}
	default:
		if t, ok := ast.LookupStructType(flat); ok {
			if def := ast.GetStructDef(t); def != nil {
				module = def.Module
			}
		}
	}
	if module == "" {
		switch kind {
		case "enum":
			p.AddEnumName(flat)
		case "interface":
			p.AddInterfaceName(flat)
		default:
			p.AddStructName(flat)
		}
		return
	}
	p.AddModuleType(module, strings.TrimPrefix(flat, module+"_"), kind)
}

// ResolveUserModules finds non-stdlib imports, parses their .dx files,
// prefixes their functions, and merges them into the main program.
func ResolveUserModules(program *ast.Program, sourceDir string) error {
	visited := map[string]bool{}
	processing := map[string]bool{}
	owners := map[string]string{}

	refs := make([]ImportRef, 0, len(program.Imports))
	mainFile := ""
	for _, imp := range program.Imports {
		refs = append(refs, ImportRef{Path: imp.Path, Alias: imp.Alias, Line: imp.Pos.Line, Col: imp.Pos.Col})
		if mainFile == "" {
			mainFile = imp.Pos.File
		}
	}
	mod, err := FindMod(sourceDir)
	if err != nil {
		return err
	}
	ids := moduleIdentities(mod, refs, sourceDir)
	aliases, err := fileAliases(mod, refs, sourceDir, mainFile, ids)
	if err != nil {
		return err
	}
	program.ModuleAliases = aliases
	for i := range program.Functions {
		renameModuleRefs(&program.Functions[i], aliases)
	}
	for i := range program.GlobalLets {
		renameModuleRefsInExpr(program.GlobalLets[i].Value, aliases)
	}

	mainExternals := map[string]bool{}
	for _, em := range program.Externals {
		mainExternals[em.Name] = true
	}

	mainGlobals := len(program.GlobalLets)
	r := &resolver{program: program, mod: mod, ids: ids, visited: visited, processing: processing, owners: owners, mainExternals: mainExternals}
	if err := r.resolveImports(sourceDir); err != nil {
		return err
	}
	ordered := make([]ast.LetStmt, 0, len(program.GlobalLets))
	ordered = append(ordered, program.GlobalLets[mainGlobals:]...)
	program.GlobalLets = append(ordered, program.GlobalLets[:mainGlobals]...)
	resolveQualifiedGlobals(program)
	return nil
}

type resolver struct {
	program       *ast.Program
	mod           *Mod
	ids           map[string]string
	visited       map[string]bool
	processing    map[string]bool
	owners        map[string]string
	mainExternals map[string]bool
}

func renameModuleRefs(fn *ast.Function, names map[string]string) {
	if len(names) == 0 {
		return
	}
	keys := make(map[string]bool, len(names))
	for k := range names {
		keys[k] = true
	}
	visible := withoutShadowed(keys, fn)
	if len(visible) == 0 {
		return
	}
	walkStmts(fn.Body, func(expr ast.Expr) ast.Expr {
		renameModuleRef(expr, names, visible)
		return nil
	})
}

func renameModuleRefsInExpr(expr ast.Expr, names map[string]string) {
	if expr == nil || len(names) == 0 {
		return
	}
	visible := make(map[string]bool, len(names))
	for k := range names {
		visible[k] = true
	}
	walkExpr(expr, func(e ast.Expr) ast.Expr {
		renameModuleRef(e, names, visible)
		return nil
	})
}

func renameModuleRef(expr ast.Expr, names map[string]string, visible map[string]bool) {
	switch e := expr.(type) {
	case *ast.CallExpr:
		head, rest := splitModuleHead(e.Module)
		if head != "" && visible[head] {
			e.Module = names[head] + rest
		}
	case *ast.FieldAccessExpr:
		if id, ok := e.Object.(*ast.Ident); ok && visible[id.Name] {
			id.Name = names[id.Name]
		}
	}
}

func splitModuleHead(module string) (string, string) {
	if dot := strings.Index(module, "."); dot >= 0 {
		return module[:dot], module[dot:]
	}
	return module, ""
}

// resolveQualifiedGlobals rewrites `someModule.NAME` into the flat name that
// module member was merged under — a module-level constant, or a function
// referenced as a value rather than called. Both are prefixed on merge, so
// without this a constant could only be read from inside its own module, and a
// handler could only be passed to http.route through a local wrapper.
func resolveQualifiedGlobals(program *ast.Program) {
	globals := make(map[string]bool, len(program.GlobalLets)+len(program.Functions))
	for _, gl := range program.GlobalLets {
		globals[gl.Name] = true
	}
	for _, fn := range program.Functions {
		globals[fn.Name] = true
	}
	modules := make(map[string]bool, len(program.UserModules))
	for _, m := range program.UserModules {
		modules[m] = true
	}
	if len(modules) == 0 {
		return
	}

	// rewrite returns the replacement for one expression, or nil to keep it.
	// visible is the set of module names not shadowed where the expression is.
	rewrite := func(visible map[string]bool, expr ast.Expr) ast.Expr {
		fa, ok := expr.(*ast.FieldAccessExpr)
		if !ok {
			return nil
		}
		ident, ok := fa.Object.(*ast.Ident)
		if !ok || !visible[ident.Name] {
			return nil
		}
		flat := ident.Name + "_" + fa.Field
		if !globals[flat] {
			// Not a module global — an ordinary field access on a variable that
			// happens to share a module's name.
			return nil
		}
		return &ast.Ident{Pos: fa.Pos, Name: flat}
	}

	// **A local takes the name.** Checking only whether the module has a
	// matching global got this backwards the moment it did: in a file importing
	// a module called `outcome`,
	//
	//	let outcome: gateway.Outcome = gateway.charge(...)
	//	if (!outcome.ok) { ... }
	//
	// rewrote `outcome.ok` to the module's `ok` function — so a field read
	// became a function value, and the condition stopped being a bool. The
	// module is unreachable by that name inside such a function, which is what
	// shadowing means everywhere else.
	for i := range program.Functions {
		fn := &program.Functions[i]
		visible := withoutShadowed(modules, fn)
		if len(visible) == 0 {
			continue
		}
		walkStmts(fn.Body, func(expr ast.Expr) ast.Expr {
			return rewrite(visible, expr)
		})
	}
	for i := range program.GlobalLets {
		program.GlobalLets[i].Value = walkExpr(program.GlobalLets[i].Value, func(expr ast.Expr) ast.Expr {
			return rewrite(modules, expr)
		})
	}
}

func (r *resolver) resolveImports(sourceDir string) error {
	program := r.program
	visited, processing := r.visited, r.processing
	var userImports []ast.Import
	var stdlibImports []ast.Import

	for _, imp := range program.Imports {
		if stdlib.Lookup(imp.Path) != nil {
			stdlibImports = append(stdlibImports, imp)
		} else {
			userImports = append(userImports, imp)
		}
	}

	program.Imports = stdlibImports

	for _, imp := range userImports {
		filePath, err := importFile(r.mod, sourceDir, imp.Path)
		if err != nil {
			return posError(imp.Pos, err)
		}
		absPath, err := filepath.Abs(filePath)
		if err != nil {
			return fmt.Errorf("cannot resolve path for import '%s': %v", imp.Path, err)
		}
		moduleName := r.identity(absPath, ImportRef{Path: imp.Path, Alias: imp.Alias})

		if visited[absPath] {
			if !containsString(program.UserModules, moduleName) {
				program.UserModules = append(program.UserModules, moduleName)
			}
			continue
		}

		if processing[absPath] {
			return fmt.Errorf("circular import detected: '%s'", imp.Path)
		}

		if err := r.resolveModuleFile(filePath, absPath, moduleName); err != nil {
			return err
		}
	}

	return nil
}

func (r *resolver) identity(absPath string, ref ImportRef) string {
	if id, ok := r.ids[absPath]; ok {
		return id
	}
	return localName(ref)
}

// Sub-imports are resolved before this module is parsed, so their struct types
// are registered by the time its type annotations are read.
func (r *resolver) resolveModuleFile(filePath, absPath, moduleName string) error {
	program := r.program
	visited, processing, owners := r.visited, r.processing, r.owners
	processing[absPath] = true

	source, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("cannot open user module '%s': %v", filePath, err)
	}

	lex := lexer.NewWithFile(string(source), filePath)
	tokens, err := lex.Tokenize()
	if err != nil {
		return err
	}

	subImports := ExtractImports(tokens)
	importPaths := make([]string, 0, len(subImports))
	for _, ref := range subImports {
		importPaths = append(importPaths, ref.Path)
	}

	modDir := filepath.Dir(absPath)
	aliases, err := fileAliases(r.mod, subImports, modDir, filePath, r.ids)
	if err != nil {
		return err
	}
	localImports := map[string]bool{}
	for _, ref := range subImports {
		localImports[localName(ref)] = true
	}
	for _, ref := range subImports {
		subPath := ref.Path
		if stdlib.Lookup(subPath) != nil {
			continue
		}
		subFilePath, err := importFile(r.mod, modDir, subPath)
		if err != nil {
			return posError(ast.Pos{File: filePath, Line: ref.Line, Col: ref.Col}, err)
		}
		subAbsPath, err := filepath.Abs(subFilePath)
		if err != nil {
			return fmt.Errorf("cannot resolve path for import '%s': %v", subPath, err)
		}
		subModuleName := r.identity(subAbsPath, ref)

		if visited[subAbsPath] {
			if !containsString(program.UserModules, subModuleName) {
				program.UserModules = append(program.UserModules, subModuleName)
			}
			continue
		}
		if processing[subAbsPath] {
			return fmt.Errorf("circular import detected: '%s'", subPath)
		}

		if err := r.resolveModuleFile(subFilePath, subAbsPath, subModuleName); err != nil {
			return err
		}
	}

	typeNames := stdlib.ModuleTypesForImports(importPaths)
	p := parser.New(tokens)
	// This file declares `moduleName`, so its own types register under it and a
	// bare reference inside it resolves to its own first.
	p.SetModule(moduleName)
	p.SetModuleAliases(aliases)
	for _, name := range typeNames {
		p.AddStructName(name)
	}
	SeedParserTypes(p)
	modProgram, errs := p.Parse()
	if len(errs) > 0 {
		return errs[0]
	}

	// A generic declared in a user module would be dropped here: its template
	// lives in this file's parser, and instantiating one needs that parser —
	// the main program's has no way to reach it. A generic struct is worse
	// still, because its instantiation happens while a file is parsed and this
	// module is read after the main program already was. Said plainly rather
	// than merged into nothing, which reported the call as an undefined
	// function and named no reason.
	if len(modProgram.GenericFuncs) > 0 || len(modProgram.GenericStructs) > 0 {
		name := ""
		if len(modProgram.GenericFuncs) > 0 {
			name = modProgram.GenericFuncs[0].Name
		} else {
			name = modProgram.GenericStructs[0].Name
		}
		return fmt.Errorf("%s: '%s' is generic, and a user module cannot declare a generic yet — move it into the main program", filePath, name)
	}

	FlattenStructMethods(modProgram)

	names := map[string]string{}
	for local, id := range aliases {
		names[local] = id
	}
	for _, em := range modProgram.Externals {
		if em.Local != "" && em.Local != em.Name {
			names[em.Local] = em.Name
		}
	}
	for i := range modProgram.Functions {
		renameModuleRefs(&modProgram.Functions[i], names)
	}
	for i := range modProgram.GlobalLets {
		renameModuleRefsInExpr(modProgram.GlobalLets[i].Value, names)
	}
	if err := r.refuseForeignExternals(modProgram, names, localImports); err != nil {
		return err
	}

	delete(processing, absPath)
	visited[absPath] = true

	// Globals are prefixed alongside functions: everything merges into one flat
	// namespace, so two modules declaring `let svc` would otherwise collide.
	ownNames := map[string]bool{}
	for _, fn := range modProgram.Functions {
		ownNames[fn.Name] = true
	}
	for _, gl := range modProgram.GlobalLets {
		ownNames[gl.Name] = true
	}

	for i := range modProgram.Functions {
		fn := &modProgram.Functions[i]
		if fn.Name == "main" || !ownNames[fn.Name] {
			continue
		}
		fn.Name = moduleName + "_" + fn.Name
		// A parameter or local may share a name with one of this module's
		// top-level names. Inside that function the local wins, so those names
		// are withheld from the rename rather than being captured by it.
		visible := withoutShadowed(ownNames, fn)
		for _, stmt := range fn.Body {
			prefixCallsInStmt(stmt, moduleName, visible)
		}
	}
	// Function bodies that were not renamed (main) still reference this module's
	// names, and every global initialiser may too.
	for i := range modProgram.GlobalLets {
		gl := &modProgram.GlobalLets[i]
		prefixCallsInExpr(gl.Value, moduleName, ownNames)
		gl.Name = moduleName + "_" + gl.Name
	}

	for _, fn := range modProgram.Functions {
		if fn.Name == "main" {
			continue
		}
		if err := own(owners, fn.Name, absPath, moduleName); err != nil {
			return err
		}
		program.Functions = append(program.Functions, fn)
	}

	program.GlobalLets = append(program.GlobalLets, modProgram.GlobalLets...)

	for _, sd := range modProgram.Structs {
		if program.StructModule == nil {
			program.StructModule = make(map[string]string)
		}
		program.StructModule[sd.Name] = moduleName
	}
	program.Structs = append(program.Structs, modProgram.Structs...)

	// Interfaces merge the same way structs and enums do. Without this a
	// library could not export one: the declaration parsed fine and was then
	// dropped, so the importing file reported "unknown type" for a name it
	// could see in the source.
	for _, id := range modProgram.Interfaces {
		if !containsInterface(program.Interfaces, id.Name) {
			program.Interfaces = append(program.Interfaces, id)
		}
	}

	// Enums merge the same way structs do. Without this an imported enum has no
	// typedef emitted and no variants validated, which is what made a
	// cross-module enum fail at the first use.
	for _, ed := range modProgram.Enums {
		if !containsEnum(program.Enums, ed.Name) {
			program.Enums = append(program.Enums, ed)
		}
	}

	// An external block declared in a module has to reach the program, or the
	// module's own calls into it report the module as not imported.
	for _, em := range modProgram.Externals {
		if !containsExternal(program.Externals, em.Name) {
			program.Externals = append(program.Externals, em)
		}
	}

	for _, modImp := range modProgram.Imports {
		if stdlib.Lookup(modImp.Path) != nil && !containsImport(program.Imports, modImp.Path) {
			program.Imports = append(program.Imports, modImp)
		}
	}

	for _, ref := range subImports {
		if stdlib.Lookup(ref.Path) != nil {
			continue
		}
		subFile, err := importFile(r.mod, modDir, ref.Path)
		if err != nil {
			continue
		}
		subAbs, err := filepath.Abs(subFile)
		if err != nil {
			continue
		}
		if subModName := r.identity(subAbs, ref); !containsString(program.UserModules, subModName) {
			program.UserModules = append(program.UserModules, subModName)
		}
	}

	if !containsString(program.UserModules, moduleName) {
		program.UserModules = append(program.UserModules, moduleName)
	}

	return nil
}

func posError(pos ast.Pos, err error) error {
	if pos.File == "" || pos.Line == 0 {
		return err
	}
	return fmt.Errorf("%s:%d:%d: %v", pos.File, pos.Line, pos.Col, err)
}

func (r *resolver) refuseForeignExternals(modProgram *ast.Program, own map[string]string, imports map[string]bool) error {
	if len(r.mainExternals) == 0 {
		return nil
	}
	var found error
	for i := range modProgram.Functions {
		fn := &modProgram.Functions[i]
		visible := withoutShadowed(r.mainExternals, fn)
		walkStmts(fn.Body, func(expr ast.Expr) ast.Expr {
			call, ok := expr.(*ast.CallExpr)
			if !ok || found != nil {
				return nil
			}
			head, _ := splitModuleHead(call.Module)
			if visible[head] && own[head] == "" && !imports[head] {
				found = fmt.Errorf("%s:%d:%d: external module '%s' is declared in the main program, and an external module is visible only in the file that declares it — declare it in this file too",
					call.Pos.File, call.Pos.Line, call.Pos.Col, head)
			}
			return nil
		})
	}
	return found
}

func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func containsEnum(enums []ast.EnumDef, name string) bool {
	for _, ed := range enums {
		if ed.Name == name {
			return true
		}
	}
	return false
}

func containsImport(imports []ast.Import, path string) bool {
	for _, imp := range imports {
		if imp.Path == path {
			return true
		}
	}
	return false
}

// Rewrites every reference to one of this module's own top-level names into the
// flat, prefixed name it is merged under.
func prefixCallsInStmt(stmt ast.Stmt, moduleName string, modNames map[string]bool) {
	renameInStmt(stmt, modNames, func(name string) string { return moduleName + "_" + name })
}

func renameInStmt(stmt ast.Stmt, modNames map[string]bool, prefix func(string) string) {

	stmtFn := func(st ast.Stmt) {
		// Assignment targets and increment/decrement targets are bare names
		// rather than expressions, so they are renamed here.
		switch t := st.(type) {
		case *ast.AssignStmt:
			if modNames[t.Name] {
				t.Name = prefix(t.Name)
			}
		case *ast.CompoundAssignStmt:
			if modNames[t.Name] {
				t.Name = prefix(t.Name)
			}
		case *ast.IncrementStmt:
			if modNames[t.Name] {
				t.Name = prefix(t.Name)
			}
		case *ast.DecrementStmt:
			if modNames[t.Name] {
				t.Name = prefix(t.Name)
			}
		}
	}

	exprFn := func(expr ast.Expr) ast.Expr {
		switch e := expr.(type) {
		case *ast.Ident:
			if modNames[e.Name] {
				e.Name = prefix(e.Name)
			}
		case *ast.CallExpr:
			if e.Module == "" && modNames[e.Name] {
				e.Name = prefix(e.Name)
			}
			// A method call on a module-level value carries its receiver as a
			// name rather than an expression. A dotted chain such as
			// self.field.method() has only its head renamed.
			if e.Module != "" {
				head := e.Module
				rest := ""
				if dot := strings.Index(head, "."); dot >= 0 {
					rest = head[dot:]
					head = head[:dot]
				}
				if modNames[head] {
					e.Module = prefix(head) + rest
				}
			}
		}
		// Never replace the node, only rename in place, so children are walked.
		return nil
	}

	walkStmtsWith([]ast.Stmt{stmt}, stmtFn, exprFn)
}

// prefixCallsInExpr is the expression-only form, for global initialisers.
func prefixCallsInExpr(expr ast.Expr, moduleName string, modNames map[string]bool) {
	if expr == nil {
		return
	}
	prefixCallsInStmt(&ast.ExprStmt{Expr: expr}, moduleName, modNames)
}

// Extracts methods into top-level functions taking a "self" receiver, rewriting
// bare field references in their bodies to read through it.
func FlattenStructMethods(program *ast.Program) {
	for _, sd := range program.Structs {
		if len(sd.Methods) == 0 {
			continue
		}

		// Sibling method names count too: named without being called, one is a
		// method value on the same receiver.
		fieldNames := map[string]bool{}
		methodNames := map[string]bool{}
		for _, m := range sd.Methods {
			methodNames[m.Name] = true
		}
		for _, f := range sd.Fields {
			fieldNames[f.Name] = true
		}

		structType, ok := ast.LookupStructType(sd.Name)
		if !ok {
			continue
		}

		for _, method := range sd.Methods {
			flatFn := ast.Function{
				Pos:         method.Pos,
				Name:        sd.Name + "_" + method.Name,
				ReturnType:  method.ReturnType,
				IsPrivate:   method.IsPrivate,
				Throws:      method.Throws,
				ThrowsTypes: method.ThrowsTypes,
			}

			// A pointer receiver, not a copy. A method that assigned to a field
			// of a by-value receiver compiled and then did nothing, which is a
			// worse outcome than either mutating or being rejected — and it is
			// what `return self` has to mean for a chain to thread one instance
			// through. It also stops every method call copying its struct.
			selfParam := ast.Param{Pos: method.Pos, Name: "self", Type: ast.RefTypeOf(structType)}
			flatFn.Params = append(flatFn.Params, selfParam)
			flatFn.Params = append(flatFn.Params, method.Params...)

			flatFn.Body = make([]ast.Stmt, len(method.Body))
			copy(flatFn.Body, method.Body)

			localNames := map[string]bool{}
			for _, p := range method.Params {
				localNames[p.Name] = true
			}

			for _, stmt := range flatFn.Body {
				rewriteFieldRefsInStmt(stmt, fieldNames, methodNames, localNames)
			}

			program.Functions = append(program.Functions, flatFn)
		}
	}
}

// Rewrites a method body so bare field names read through the receiver: `label`
// means `self.label`. A local of the same name shadows the field.
func rewriteFieldRefsInStmt(stmt ast.Stmt, fieldNames, methodNames, localNames map[string]bool) {
	stmtFn := func(st ast.Stmt) {
		if let, ok := st.(*ast.LetStmt); ok {
			// A local declared here shadows any field of the same name from this
			// point on.
			if let.Name != "" {
				localNames[let.Name] = true
			}
			for _, n := range let.Names {
				localNames[n] = true
			}
		}
	}

	exprFn := func(expr ast.Expr) ast.Expr {
		switch e := expr.(type) {
		case *ast.Ident:
			if (fieldNames[e.Name] || methodNames[e.Name]) && !localNames[e.Name] {
				return &ast.FieldAccessExpr{Pos: e.Pos, Object: &ast.Ident{Pos: e.Pos, Name: "self"}, Field: e.Name}
			}
		case *ast.CallExpr:
			// A call qualified by a field name is a method call on that field —
			// `hub.send(...)` where hub is a field, not a module. Only the first
			// segment can be a field: `cmd.action.isEmpty()` reads through the same
			// receiver as `cmd` does.
			if e.Module != "" {
				head := e.Module
				if dot := strings.Index(head, "."); dot >= 0 {
					head = head[:dot]
				}
				if fieldNames[head] && !localNames[head] {
					e.Module = "self." + e.Module
				}
			}
			// An unqualified call naming a sibling method is a call on the same
			// receiver, so `commandGuard(id)` inside a method means
			// `self.commandGuard(id)`.
			if e.Module == "" && methodNames[e.Name] && !localNames[e.Name] {
				e.Module = "self"
			}
		}
		return nil
	}

	walkStmtsWith([]ast.Stmt{stmt}, stmtFn, exprFn)
}

func walkProgramExprs(program *ast.Program, fn func(ast.Expr) ast.Expr) {
	for i := range program.GlobalLets {
		program.GlobalLets[i].Value = walkExpr(program.GlobalLets[i].Value, fn)
	}
	for i := range program.Functions {
		walkStmts(program.Functions[i].Body, fn)
	}
}

func walkStmts(stmts []ast.Stmt, fn func(ast.Expr) ast.Expr) {
	walkStmtsWith(stmts, nil, fn)
}

// A walker carries both callbacks through the whole traversal. They used to be
// passed as separate arguments, which lost stmtFn the moment an expression
// contained statements: a spawn block's body was visited with the statement
// callback dropped. Holding them together makes that impossible to reintroduce.
type walker struct {
	stmtFn func(ast.Stmt)
	exprFn func(ast.Expr) ast.Expr
	// inFrame stops the traversal at a nested function body — a lambda, or a
	// spawn. Those statements run in another frame, and on another thread for a
	// spawn, so an analysis about what happens in *this* call must not read
	// them as if they were inline.
	inFrame bool
}

// stmtFn, when non-nil, runs on each statement before its children — assignment
// targets are plain strings rather than expressions, so a rewrite needs it.
func walkStmtsWith(stmts []ast.Stmt, stmtFn func(ast.Stmt), exprFn func(ast.Expr) ast.Expr) {
	(&walker{stmtFn: stmtFn, exprFn: exprFn}).stmts(stmts)
}

func walkExpr(expr ast.Expr, fn func(ast.Expr) ast.Expr) ast.Expr {
	return (&walker{exprFn: fn}).expr(expr)
}

func (w *walker) stmts(stmts []ast.Stmt) {
	for _, stmt := range stmts {
		w.stmt(stmt)
	}
}

func (w *walker) stmt(stmt ast.Stmt) {
	if stmt == nil {
		return
	}
	if w.stmtFn != nil {
		w.stmtFn(stmt)
	}
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		s.Expr = w.expr(s.Expr)
	case *ast.LetStmt:
		s.Value = w.expr(s.Value)
	case *ast.AssignStmt:
		s.Value = w.expr(s.Value)
	case *ast.IndexAssignStmt:
		s.Array = w.expr(s.Array)
		s.Index = w.expr(s.Index)
		s.Value = w.expr(s.Value)
	case *ast.FieldAssignStmt:
		s.Object = w.expr(s.Object)
		s.Value = w.expr(s.Value)
	case *ast.CompoundAssignStmt:
		s.Value = w.expr(s.Value)
	case *ast.DestructureLetStmt:
		s.Value = w.expr(s.Value)
	case *ast.ReturnStmt:
		s.Value = w.expr(s.Value)
	case *ast.IfStmt:
		s.Cond = w.expr(s.Cond)
		w.stmts(s.Then)
		w.stmts(s.Else)
	case *ast.WhileStmt:
		s.Cond = w.expr(s.Cond)
		w.stmts(s.Body)
	case *ast.ForStmt:
		w.stmt(s.Init)
		s.Cond = w.expr(s.Cond)
		w.stmt(s.Post)
		w.stmts(s.Body)
	case *ast.ForeachStmt:
		s.Iterable = w.expr(s.Iterable)
		w.stmts(s.Body)
	case *ast.SwitchStmt:
		s.Tag = w.expr(s.Tag)
		for i := range s.Cases {
			for j := range s.Cases[i].Values {
				s.Cases[i].Values[j] = w.expr(s.Cases[i].Values[j])
			}
			w.stmts(s.Cases[i].Body)
		}
		w.stmts(s.Default)
	case *ast.SendStmt:
		s.Target = w.expr(s.Target)
		s.Value = w.expr(s.Value)
	case *ast.ThrowStmt:
		s.Value = w.expr(s.Value)
	case *ast.TryCatchStmt:
		w.stmts(s.Body)
		for i := range s.Catches {
			w.stmts(s.Catches[i].Body)
		}
		w.stmts(s.FinallyBody)
	case *ast.DeferStmt:
		s.Expr = w.expr(s.Expr)
	case *ast.BlockStmt:
		w.stmts(s.Stmts)
	}
}

func (w *walker) expr(expr ast.Expr) ast.Expr {
	if expr == nil {
		return nil
	}
	if w.exprFn != nil {
		if replacement := w.exprFn(expr); replacement != nil {
			return replacement
		}
	}
	switch e := expr.(type) {
	case *ast.CallExpr:
		e.Recv = w.expr(e.Recv)
		for i := range e.Args {
			e.Args[i] = w.expr(e.Args[i])
		}
	case *ast.BinaryExpr:
		e.Left = w.expr(e.Left)
		e.Right = w.expr(e.Right)
	case *ast.UnaryExpr:
		e.Operand = w.expr(e.Operand)
	case *ast.IndexExpr:
		e.Array = w.expr(e.Array)
		e.Index = w.expr(e.Index)
	case *ast.SliceExpr:
		e.Array = w.expr(e.Array)
		e.Start = w.expr(e.Start)
		e.End = w.expr(e.End)
	case *ast.ArrayLitExpr:
		for i := range e.Elems {
			e.Elems[i] = w.expr(e.Elems[i])
		}
	case *ast.ObjectLitExpr:
		for i := range e.Values {
			e.Values[i] = w.expr(e.Values[i])
		}
	case *ast.StructLitExpr:
		for i := range e.FieldValues {
			e.FieldValues[i] = w.expr(e.FieldValues[i])
		}
	case *ast.FieldAccessExpr:
		e.Object = w.expr(e.Object)
	case *ast.StringInterpExpr:
		for i := range e.Parts {
			e.Parts[i] = w.expr(e.Parts[i])
		}
	case *ast.MatchExpr:
		e.Tag = w.expr(e.Tag)
		for i := range e.Arms {
			for j := range e.Arms[i].Patterns {
				e.Arms[i].Patterns[j] = w.expr(e.Arms[i].Patterns[j])
			}
			e.Arms[i].Body = w.expr(e.Arms[i].Body)
		}
	case *ast.LambdaExpr:
		if !w.inFrame {
			w.stmts(e.Body)
		}
	case *ast.SpawnExpr:
		if !w.inFrame {
			e.Call = w.expr(e.Call)
			w.stmts(e.Body)
		}
	case *ast.ReceiveExpr:
		e.Source = w.expr(e.Source)
	}
	return expr
}

// Renaming a shadowed name would rebind a local to a module-level value.
func withoutShadowed(names map[string]bool, fn *ast.Function) map[string]bool {
	shadowed := map[string]bool{}
	for _, p := range fn.Params {
		shadowed[p.Name] = true
	}
	collectDeclaredNames(fn.Body, shadowed)
	if len(shadowed) == 0 {
		return names
	}
	visible := make(map[string]bool, len(names))
	for name := range names {
		if !shadowed[name] {
			visible[name] = true
		}
	}
	return visible
}

// Block scoping is not modelled: a name declared in an inner block withholds the
// rename for the whole function, erring toward an "undefined" error over a
// silent rebinding.
func collectDeclaredNames(stmts []ast.Stmt, out map[string]bool) {
	walkStmtsWith(stmts, func(st ast.Stmt) {
		switch s := st.(type) {
		case *ast.LetStmt:
			if s.Name != "" {
				out[s.Name] = true
			}
			for _, n := range s.Names {
				out[n] = true
			}
		case *ast.ForeachStmt:
			if s.ValueVar != "" {
				out[s.ValueVar] = true
			}
			if s.IndexVar != "" {
				out[s.IndexVar] = true
			}
		}
	}, func(expr ast.Expr) ast.Expr { return nil })
}

func containsExternal(list []ast.ExternalModule, name string) bool {
	for i := range list {
		if list[i].Name == name {
			return true
		}
	}
	return false
}

func containsInterface(list []ast.InterfaceDef, name string) bool {
	for i := range list {
		if list[i].Name == name {
			return true
		}
	}
	return false
}

type Instantiator struct {
	*parser.Parser
	Aliases map[string]string
}

func (in Instantiator) InstantiateFunc(name string, args []ast.Type) (*ast.Function, []ast.StructDef, error) {
	fn, structs, err := in.Parser.InstantiateFunc(name, args)
	if err == nil && fn != nil {
		renameModuleRefs(fn, in.Aliases)
	}
	return fn, structs, err
}

func RenameCCollisions(program *ast.Program) {
	taken := map[string]bool{}
	for _, fn := range program.Functions {
		taken[fn.Name] = true
	}
	for _, gl := range program.GlobalLets {
		taken[gl.Name] = true
	}
	renames := map[string]string{}
	pick := func(name string) {
		if name == "main" || !cLibraryNames[name] {
			return
		}
		to := "dx_" + name
		for taken[to] || cLibraryNames[to] {
			to += "_"
		}
		taken[to] = true
		renames[name] = to
	}
	for _, fn := range program.Functions {
		pick(fn.Name)
	}
	for _, gl := range program.GlobalLets {
		pick(gl.Name)
	}
	if len(renames) == 0 {
		return
	}
	names := make(map[string]bool, len(renames))
	for k := range renames {
		names[k] = true
	}
	rename := func(name string) string { return renames[name] }
	for i := range program.Functions {
		fn := &program.Functions[i]
		visible := withoutShadowed(names, fn)
		for _, stmt := range fn.Body {
			renameInStmt(stmt, visible, rename)
		}
		if to, ok := renames[fn.Name]; ok {
			fn.Name = to
		}
	}
	for i := range program.GlobalLets {
		gl := &program.GlobalLets[i]
		if gl.Value != nil {
			renameInStmt(&ast.ExprStmt{Expr: gl.Value}, names, rename)
		}
		if to, ok := renames[gl.Name]; ok {
			gl.Name = to
		}
	}
}
