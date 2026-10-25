package lsp

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/lexer"
	"github.com/ensamuel7/dex/parser"
	"github.com/ensamuel7/dex/resolve"
	"github.com/ensamuel7/dex/stdlib"
	"github.com/ensamuel7/dex/token"
)

func (s *Server) completionsAt(text string, pos Position, uri string) []CompletionItem {
	lines := strings.Split(text, "\n")
	if pos.Line >= len(lines) {
		return nil
	}

	line := lines[pos.Line]
	col := pos.Character
	if col > len(line) {
		col = len(line)
	}
	prefix := line[:col]

	// Check if we're completing after a dot (module.func)
	if idx := strings.LastIndex(prefix, "."); idx >= 0 {
		// Extract module name before the dot
		before := strings.TrimSpace(prefix[:idx])
		words := strings.Fields(before)
		if len(words) > 0 {
			moduleName := words[len(words)-1]
			// `self.` reaches the enclosing struct's fields and methods, which is
			// neither a module nor an enum and so would otherwise complete to
			// nothing at all.
			if moduleName == "self" {
				if items := s.selfMemberCompletions(text, pos.Line, uri); items != nil {
					return items
				}
			}
			return s.moduleCompletions(moduleName, text, uri)
		}
	}

	// `self` is offered only where it means something: inside a struct body.
	items := selfItem(text, pos.Line)

	// Keywords
	for _, kw := range []struct {
		label  string
		detail string
	}{
		{"fn", "Function declaration (short)"},
		{"function", "Function declaration"},
		{"let", "Variable declaration"},
		{"const", "Immutable variable declaration"},
		{"return", "Return from function"},
		{"if", "Conditional branch"},
		{"else", "Else branch"},
		{"while", "While loop"},
		{"import", "Import module"},
		{"public", "Public access modifier"},
		{"private", "Private access modifier"},
		{"true", "Boolean true"},
		{"false", "Boolean false"},
		{"try", "Try-catch block"},
		{"catch", "Catch exception"},
		{"finally", "Finally block"},
		{"throw", "Throw exception"},
		{"throws", "Declare that a function may let an exception escape"},
		{"switch", "Switch statement"},
		{"case", "Case branch"},
		{"default", "Default branch"},
		{"enum", "Enum type declaration"},
		{"map", "Map type"},
		{"for", "C-style for loop"},
		{"foreach", "Iterate over array elements"},
		{"as", "Type alias in foreach"},
		{"break", "Break out of a loop"},
		{"continue", "Skip to next loop iteration"},
		{"struct", "Struct type declaration"},
		{"match", "Pattern matching expression"},
		{"defer", "Defer statement execution to scope exit"},
		{"interface", "Interface type declaration"},
		{"spawn", "Spawn a concurrent task"},
		{"chan", "Channel type for concurrency"},
		{"null", "Null value"},
		{"mutex", "Mutual exclusion lock"},
		{"weak", "Weak reference, not counted for ownership"},
		{"external", "Declare a module of C-ABI functions"},
	} {
		items = append(items, CompletionItem{
			Label:  kw.label,
			Kind:   CompletionKindKeyword,
			Detail: kw.detail,
		})
	}

	// Types
	for _, t := range []string{"int", "bool", "string", "long", "double", "char", "void", "int[]", "bool[]", "string[]", "long[]", "double[]", "char[]"} {
		items = append(items, CompletionItem{
			Label:  t,
			Kind:   CompletionKindType,
			Detail: "Built-in type",
		})
	}

	// User-defined functions from current file
	lex := lexer.New(text)
	tokens, err := lex.Tokenize()
	if err == nil {
		p := parser.New(tokens)
		seedParserModuleTypes(p, tokens, filepath.Dir(uriToPath(uri)))
		program, errs := p.Parse()
		if len(errs) == 0 {
			for _, fn := range program.Functions {
				items = append(items, CompletionItem{
					Label:  fn.Name,
					Kind:   CompletionKindFunction,
					Detail: formatFuncSignature(&fn),
				})
			}

			// Enum names and variants
			for _, ed := range program.Enums {
				items = append(items, CompletionItem{
					Label:  ed.Name,
					Kind:   CompletionKindType,
					Detail: "Enum type",
				})
			}

			// Imported module names
			for _, imp := range program.Imports {
				items = append(items, CompletionItem{
					Label:  importModuleName(imp),
					Kind:   CompletionKindModule,
					Detail: "Module",
				})
			}
		}
	}

	// A name typed bare may live in a module the file never imported, so offer
	// the qualified call with the import line attached.
	items = append(items, s.autoImportCompletions(identifierPrefix(prefix), text, uri)...)

	return items
}

// identifierPrefix returns the identifier being typed at the end of a line.
func identifierPrefix(linePrefix string) string {
	end := len(linePrefix)
	start := end
	for start > 0 && isIdentContinue(linePrefix[start-1]) {
		start--
	}
	if start > 0 && linePrefix[start-1] == '.' {
		return "" // a module member, already handled by moduleCompletions
	}
	word := linePrefix[start:end]
	if word != "" && !isIdentStart(word[0]) {
		return ""
	}
	return word
}

// maxAutoImportItems keeps a two-letter prefix from flooding the popup with
// every stdlib function that happens to match.
const maxAutoImportItems = 50

// autoImportCompletions suggests `module.fn` for every unimported module that
// exports a function matching the identifier being typed, carrying the import
// line as an additional edit so accepting one item writes both.
func (s *Server) autoImportCompletions(word string, text string, uri string) []CompletionItem {
	if len(word) < 2 {
		return nil
	}
	filePath := uriToPath(uri)
	sourceDir := filepath.Dir(filePath)
	lower := strings.ToLower(word)

	var items []CompletionItem
	add := func(modPath, modName, fnName, detail, doc string) {
		if len(items) >= maxAutoImportItems {
			return
		}
		label := modName + "." + fnName
		items = append(items, CompletionItem{
			Label:               label,
			Kind:                CompletionKindFunction,
			Detail:              detail + " — auto-import \"" + modPath + "\"",
			Documentation:       doc,
			InsertText:          label,
			FilterText:          fnName,
			SortText:            "zz" + label,
			AdditionalTextEdits: buildImportEdit(modPath, text),
		})
	}

	for _, modName := range sortedModuleNames() {
		if isModuleImported(modName, text) {
			continue
		}
		mod := stdlib.Lookup(modName)
		for _, fnName := range sortedFuncNames(mod) {
			if !strings.HasPrefix(strings.ToLower(fnName), lower) {
				continue
			}
			fdef := mod.Funcs[fnName]
			add(modName, modName, fnName, stdlibSignature(modName, fnName, &fdef), fdef.Doc)
		}
	}

	files := userModuleFiles(sourceDir)
	for _, impPath := range sortedKeys(files) {
		if sameFile(files[impPath], filePath) || isModuleImported(impPath, text) {
			continue
		}
		program := parseModuleFile(files[impPath])
		if program == nil {
			continue
		}
		for i := range program.Functions {
			fn := &program.Functions[i]
			if fn.Name == "main" || fn.IsPrivate {
				continue
			}
			if !strings.HasPrefix(strings.ToLower(fn.Name), lower) {
				continue
			}
			add(impPath, filepath.Base(impPath), fn.Name, formatFuncSignature(fn), "")
		}
	}

	return items
}

// sortedModuleNames lists the stdlib modules in a stable order.
func sortedModuleNames() []string {
	names := make([]string, 0, len(stdlib.AllModules()))
	for name := range stdlib.AllModules() {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// sortedFuncNames lists one module's functions in a stable order.
func sortedFuncNames(mod *stdlib.Module) []string {
	names := make([]string, 0, len(mod.Funcs))
	for name := range mod.Funcs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (s *Server) moduleCompletions(moduleName string, text string, uri string) []CompletionItem {
	// Check if moduleName is an enum type — offer variant completions
	if enumType, ok := ast.LookupEnumType(moduleName); ok {
		def := ast.GetEnumDef(enumType)
		if def != nil {
			var items []CompletionItem
			for _, v := range def.Variants {
				items = append(items, CompletionItem{
					Label:  v,
					Kind:   CompletionKindValue,
					Detail: moduleName + "." + v,
				})
			}
			return items
		}
	}

	mod := stdlib.Lookup(moduleName)
	lookupName := moduleName
	// If direct lookup fails, check if moduleName is an alias
	if mod == nil {
		if resolved := resolveAliasToPath(moduleName, text); resolved != "" {
			mod = stdlib.Lookup(resolved)
			lookupName = resolved
		}
	}
	if mod != nil {
		// Build auto-import edit if this module isn't imported yet
		var autoImportEdits []TextEdit
		if !isModuleImported(lookupName, text) {
			autoImportEdits = buildImportEdit(lookupName, text)
		}

		var items []CompletionItem
		for name, fdef := range mod.Funcs {
			var paramStr string
			retStr := typeName(fdef.ReturnType)
			if sp, sr, ok := stdlib.SpecialSignature(moduleName, name, &fdef); ok {
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
			detail := fmt.Sprintf("(%s): %s", paramStr, retStr)
			item := CompletionItem{
				Label:               name,
				Kind:                CompletionKindFunction,
				Detail:              detail,
				Documentation:       fdef.Doc,
				AdditionalTextEdits: autoImportEdits,
			}
			if len(autoImportEdits) > 0 {
				item.Detail = detail + " (auto-import)"
			}
			items = append(items, item)
		}
		return items
	}

	// Check if moduleName is a user module import
	if items := s.userModuleCompletions(moduleName, text, uri); items != nil {
		return items
	}

	// Not imported yet, but a .dx file next to this one defines it — offer its
	// members with the import attached.
	if items := s.unimportedUserModuleCompletions(moduleName, text, uri); items != nil {
		return items
	}

	// Not a known module, so a value whose type is not known here: offer every
	// built-in receiver's methods. The list is the one hover reads.
	return builtinMethodCompletions()
}

// isModuleImported checks whether a module is already imported in the source text.
func isModuleImported(moduleName string, text string) bool {
	lex := lexer.New(text)
	tokens, err := lex.Tokenize()
	if err != nil {
		return false
	}
	for i := 0; i < len(tokens)-1; i++ {
		if tokens[i].Kind == token.TokenImport && tokens[i+1].Kind == token.TokenString {
			if tokens[i+1].Value == moduleName {
				return true
			}
		}
	}
	return false
}

// buildImportEdit creates a TextEdit to insert an import statement at the correct position.
func buildImportEdit(moduleName string, text string) []TextEdit {
	lines := strings.Split(text, "\n")
	insertLine := 0
	lastImportLine := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "import ") {
			lastImportLine = i
		}
	}

	if lastImportLine >= 0 {
		// Insert after the last import
		insertLine = lastImportLine + 1
	} else {
		// No imports yet — insert at top of file
		insertLine = 0
	}

	newText := fmt.Sprintf("import \"%s\"\n", moduleName)

	return []TextEdit{
		{
			Range: Range{
				Start: Position{Line: insertLine, Character: 0},
				End:   Position{Line: insertLine, Character: 0},
			},
			NewText: newText,
		},
	}
}

// userModuleCompletions returns completion items for a user module import.
// It resolves the module file path, parses it, and extracts exported functions
// and struct names. Returns nil if moduleName is not a user module import.
func (s *Server) userModuleCompletions(moduleName string, text string, uri string) []CompletionItem {
	// Find the import path for this module name
	importPath := resolveModuleNameToPath(moduleName, text)
	if importPath == "" || stdlib.Lookup(importPath) != nil {
		return nil
	}

	// Resolve to absolute .dx file path
	filePath := uriToPath(uri)
	sourceDir := filepath.Dir(filePath)
	modFile, err := resolve.ImportFile(sourceDir, sourceDir, importPath)
	if err != nil {
		return nil
	}
	if _, err := os.Stat(modFile); err != nil {
		return nil
	}
	return moduleMemberCompletions(modFile, nil)
}

// unimportedUserModuleCompletions answers `someModule.` for a module the file
// has not imported, found by its .dx file next to the open document. Every item
// carries the import line, so picking one both qualifies and imports.
func (s *Server) unimportedUserModuleCompletions(moduleName string, text string, uri string) []CompletionItem {
	filePath := uriToPath(uri)
	impPath, modFile, ok := userModuleNamed(moduleName, filepath.Dir(filePath), filePath)
	if !ok {
		return nil
	}
	return moduleMemberCompletions(modFile, buildImportEdit(impPath, text))
}

// moduleMemberCompletions lists the functions and struct types a .dx module
// offers its importers. autoImport, when set, is attached to every item.
func moduleMemberCompletions(modFile string, autoImport []TextEdit) []CompletionItem {
	program := parseModuleFile(modFile)
	if program == nil {
		return nil
	}

	var items []CompletionItem

	for i := range program.Functions {
		fn := &program.Functions[i]
		if fn.Name == "main" || fn.IsPrivate {
			continue
		}
		items = append(items, CompletionItem{
			Label:               fn.Name,
			Kind:                CompletionKindFunction,
			Detail:              withAutoImportNote(formatFuncSignature(fn), autoImport),
			AdditionalTextEdits: autoImport,
		})
	}

	for _, sd := range program.Structs {
		items = append(items, CompletionItem{
			Label:               sd.Name,
			Kind:                CompletionKindType,
			Detail:              withAutoImportNote("Struct type", autoImport),
			AdditionalTextEdits: autoImport,
		})
	}

	return items
}

// withAutoImportNote marks a detail line whose item also writes an import.
func withAutoImportNote(detail string, autoImport []TextEdit) string {
	if len(autoImport) == 0 {
		return detail
	}
	return detail + " (auto-import)"
}

// resolveModuleNameToPath scans the text for import declarations and returns
// the import path for a given module name (either the path itself if it matches
// filepath.Base, or the path if an alias matches).
func resolveModuleNameToPath(moduleName string, text string) string {
	lex := lexer.New(text)
	tokens, err := lex.Tokenize()
	if err != nil {
		return ""
	}
	for i := 0; i < len(tokens)-1; i++ {
		if tokens[i].Kind == token.TokenImport && tokens[i+1].Kind == token.TokenString {
			path := tokens[i+1].Value
			// Check for alias: import "path" as "alias"
			if i+3 < len(tokens) && tokens[i+2].Kind == token.TokenAs && tokens[i+3].Kind == token.TokenString {
				if tokens[i+3].Value == moduleName {
					return path
				}
				continue
			}
			// No alias — check if the base name matches
			if filepath.Base(path) == moduleName {
				return path
			}
		}
	}
	return ""
}
