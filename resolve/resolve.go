package resolve

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/lexer"
	"github.com/ensamuel7/dex/parser"
	"github.com/ensamuel7/dex/stdlib"
	"github.com/ensamuel7/dex/token"
)

// ExtractImportPaths scans raw tokens for import declarations and returns their paths.
func ExtractImportPaths(tokens []token.Token) []string {
	var paths []string
	for i := 0; i < len(tokens)-1; i++ {
		if tokens[i].Kind == token.TokenImport && tokens[i+1].Kind == token.TokenString {
			paths = append(paths, tokens[i+1].Value)
		}
	}
	return paths
}

// PreRegisterUserStructs scans user module files (non-stdlib imports) for struct
// and enum declarations and registers them globally so the main file parser
// recognizes struct literal syntax (e.g. User { field: value }).
func PreRegisterUserStructs(importPaths []string, sourceDir string) []string {
	var names []string
	visited := map[string]bool{}
	for _, path := range importPaths {
		if stdlib.Lookup(path) != nil {
			continue
		}
		preRegisterStructsFromFile(filepath.Join(sourceDir, path+".dx"), visited, &names)
	}
	return names
}

func preRegisterStructsFromFile(filePath string, visited map[string]bool, names *[]string) {
	absPath, err := filepath.Abs(filePath)
	if err != nil || visited[absPath] {
		return
	}
	visited[absPath] = true

	source, err := os.ReadFile(filePath)
	if err != nil {
		return
	}

	lex := lexer.New(string(source))
	tokens, err := lex.Tokenize()
	if err != nil {
		return
	}

	// Scan for struct/enum declarations and sub-imports.
	// Register placeholder types so ast.LookupStructType/LookupEnumType works
	// when the main file parser resolves type annotations (e.g. ": User").
	// The full definitions get filled in later when modules are fully parsed,
	// and RegisterStructType/RegisterEnumType are idempotent (update existing).
	for i := 0; i < len(tokens)-1; i++ {
		if tokens[i].Kind == token.TokenStruct && tokens[i+1].Kind == token.TokenIdent {
			name := tokens[i+1].Value
			*names = append(*names, name)
			ast.RegisterStructType(ast.StructDef{Name: name})
		}
		if tokens[i].Kind == token.TokenEnum && tokens[i+1].Kind == token.TokenIdent {
			name := tokens[i+1].Value
			*names = append(*names, name)
			ast.RegisterEnumType(ast.EnumDef{Name: name})
		}
	}

	// Recurse into sub-imports
	subPaths := ExtractImportPaths(tokens)
	modDir := filepath.Dir(absPath)
	for _, subPath := range subPaths {
		if stdlib.Lookup(subPath) != nil {
			continue
		}
		preRegisterStructsFromFile(filepath.Join(modDir, subPath+".dx"), visited, names)
	}
}

// ResolveUserModules finds non-stdlib imports, parses their .dx files,
// prefixes their functions, and merges them into the main program.
func ResolveUserModules(program *ast.Program, sourceDir string) error {
	visited := map[string]bool{}
	processing := map[string]bool{}
	return resolveImports(program, sourceDir, visited, processing)
}

func resolveImports(program *ast.Program, sourceDir string, visited, processing map[string]bool) error {
	var userImports []ast.Import
	var stdlibImports []ast.Import

	for _, imp := range program.Imports {
		if stdlib.Lookup(imp.Path) != nil {
			stdlibImports = append(stdlibImports, imp)
		} else {
			userImports = append(userImports, imp)
		}
	}

	// Replace imports with only stdlib imports (user imports are resolved)
	program.Imports = stdlibImports

	for _, imp := range userImports {
		moduleName := filepath.Base(imp.Path)
		if imp.Alias != "" {
			moduleName = imp.Alias
		}
		filePath := filepath.Join(sourceDir, imp.Path+".dx")
		absPath, err := filepath.Abs(filePath)
		if err != nil {
			return fmt.Errorf("cannot resolve path for import '%s': %v", imp.Path, err)
		}

		if visited[absPath] {
			if !containsString(program.UserModules, moduleName) {
				program.UserModules = append(program.UserModules, moduleName)
			}
			continue
		}

		if processing[absPath] {
			return fmt.Errorf("circular import detected: '%s'", imp.Path)
		}

		if err := resolveModuleFile(filePath, absPath, moduleName, program, visited, processing); err != nil {
			return err
		}
	}

	return nil
}

// resolveModuleFile resolves a single user module: reads, lexes, resolves sub-imports
// FIRST (so their struct types are registered), then parses, prefixes, and merges.
func resolveModuleFile(filePath, absPath, moduleName string, program *ast.Program, visited, processing map[string]bool) error {
	processing[absPath] = true

	source, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("cannot open user module '%s': %v", filePath, err)
	}

	// Lex
	lex := lexer.NewWithFile(string(source), filePath)
	tokens, err := lex.Tokenize()
	if err != nil {
		return err
	}

	// Extract import paths from tokens BEFORE parsing
	importPaths := ExtractImportPaths(tokens)

	// Resolve user sub-imports FIRST so their struct types are registered globally.
	// This ensures the parser for this module can reference struct types from dependencies.
	modDir := filepath.Dir(absPath)
	for _, subPath := range importPaths {
		if stdlib.Lookup(subPath) != nil {
			continue
		}
		subModuleName := filepath.Base(subPath)
		subFilePath := filepath.Join(modDir, subPath+".dx")
		subAbsPath, err := filepath.Abs(subFilePath)
		if err != nil {
			return fmt.Errorf("cannot resolve path for import '%s': %v", subPath, err)
		}

		if visited[subAbsPath] {
			if !containsString(program.UserModules, subModuleName) {
				program.UserModules = append(program.UserModules, subModuleName)
			}
			continue
		}
		if processing[subAbsPath] {
			return fmt.Errorf("circular import detected: '%s'", subPath)
		}

		if err := resolveModuleFile(subFilePath, subAbsPath, subModuleName, program, visited, processing); err != nil {
			return err
		}
	}

	// NOW parse — all dependency struct types are registered globally
	typeNames := stdlib.ModuleTypesForImports(importPaths)
	p := parser.New(tokens)
	for _, name := range typeNames {
		p.AddStructName(name)
	}
	// Seed parser with struct names from already-resolved dependencies
	for _, name := range ast.AllStructNames() {
		p.AddStructName(name)
	}
	modProgram, errs := p.Parse()
	if len(errs) > 0 {
		return errs[0]
	}

	// Flatten struct methods
	FlattenStructMethods(modProgram)

	delete(processing, absPath)
	visited[absPath] = true

	// Collect this module's own function names (before prefixing)
	ownFuncNames := map[string]bool{}
	for _, fn := range modProgram.Functions {
		ownFuncNames[fn.Name] = true
	}

	// Prefix only this module's own functions
	for i := range modProgram.Functions {
		fn := &modProgram.Functions[i]
		if fn.Name == "main" || !ownFuncNames[fn.Name] {
			continue
		}
		fn.Name = moduleName + "_" + fn.Name
		for _, stmt := range fn.Body {
			prefixCallsInStmt(stmt, moduleName, ownFuncNames)
		}
	}

	// Merge functions into program
	for _, fn := range modProgram.Functions {
		if fn.Name == "main" {
			continue
		}
		program.Functions = append(program.Functions, fn)
	}

	// Track which module each struct belongs to
	for _, sd := range modProgram.Structs {
		if program.StructModule == nil {
			program.StructModule = make(map[string]string)
		}
		program.StructModule[sd.Name] = moduleName
	}
	program.Structs = append(program.Structs, modProgram.Structs...)

	// Merge stdlib imports from this module
	for _, modImp := range modProgram.Imports {
		if stdlib.Lookup(modImp.Path) != nil && !containsImport(program.Imports, modImp.Path) {
			program.Imports = append(program.Imports, modImp)
		}
	}

	// Register user sub-module names
	for _, subPath := range importPaths {
		if stdlib.Lookup(subPath) == nil {
			subModName := filepath.Base(subPath)
			if !containsString(program.UserModules, subModName) {
				program.UserModules = append(program.UserModules, subModName)
			}
		}
	}

	if !containsString(program.UserModules, moduleName) {
		program.UserModules = append(program.UserModules, moduleName)
	}

	return nil
}

func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
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

// prefixCallsInStmt walks a statement tree and rewrites unqualified calls
// to module-internal functions with the module prefix.
func prefixCallsInStmt(stmt ast.Stmt, moduleName string, modFuncNames map[string]bool) {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		prefixCallsInExpr(s.Expr, moduleName, modFuncNames)
	case *ast.LetStmt:
		prefixCallsInExpr(s.Value, moduleName, modFuncNames)
	case *ast.AssignStmt:
		prefixCallsInExpr(s.Value, moduleName, modFuncNames)
	case *ast.ReturnStmt:
		if s.Value != nil {
			prefixCallsInExpr(s.Value, moduleName, modFuncNames)
		}
	case *ast.IfStmt:
		prefixCallsInExpr(s.Cond, moduleName, modFuncNames)
		for _, st := range s.Then {
			prefixCallsInStmt(st, moduleName, modFuncNames)
		}
		for _, st := range s.Else {
			prefixCallsInStmt(st, moduleName, modFuncNames)
		}
	case *ast.WhileStmt:
		prefixCallsInExpr(s.Cond, moduleName, modFuncNames)
		for _, st := range s.Body {
			prefixCallsInStmt(st, moduleName, modFuncNames)
		}
	case *ast.ForStmt:
		if s.Init != nil {
			prefixCallsInStmt(s.Init, moduleName, modFuncNames)
		}
		if s.Cond != nil {
			prefixCallsInExpr(s.Cond, moduleName, modFuncNames)
		}
		if s.Post != nil {
			prefixCallsInStmt(s.Post, moduleName, modFuncNames)
		}
		for _, st := range s.Body {
			prefixCallsInStmt(st, moduleName, modFuncNames)
		}
	case *ast.ForeachStmt:
		prefixCallsInExpr(s.Iterable, moduleName, modFuncNames)
		for _, st := range s.Body {
			prefixCallsInStmt(st, moduleName, modFuncNames)
		}
	case *ast.BlockStmt:
		for _, st := range s.Stmts {
			prefixCallsInStmt(st, moduleName, modFuncNames)
		}
	case *ast.IndexAssignStmt:
		prefixCallsInExpr(s.Array, moduleName, modFuncNames)
		prefixCallsInExpr(s.Index, moduleName, modFuncNames)
		prefixCallsInExpr(s.Value, moduleName, modFuncNames)
	case *ast.FieldAssignStmt:
		prefixCallsInExpr(s.Object, moduleName, modFuncNames)
		prefixCallsInExpr(s.Value, moduleName, modFuncNames)
	case *ast.CompoundAssignStmt:
		prefixCallsInExpr(s.Value, moduleName, modFuncNames)
	case *ast.SendStmt:
		if s.Target != nil {
			prefixCallsInExpr(s.Target, moduleName, modFuncNames)
		}
		prefixCallsInExpr(s.Value, moduleName, modFuncNames)
	}
}

// prefixCallsInExpr walks an expression tree and rewrites unqualified calls
// to module-internal functions with the module prefix.
func prefixCallsInExpr(expr ast.Expr, moduleName string, modFuncNames map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.CallExpr:
		if e.Module == "" && modFuncNames[e.Name] {
			e.Name = moduleName + "_" + e.Name
		}
		for _, arg := range e.Args {
			prefixCallsInExpr(arg, moduleName, modFuncNames)
		}
	case *ast.BinaryExpr:
		prefixCallsInExpr(e.Left, moduleName, modFuncNames)
		prefixCallsInExpr(e.Right, moduleName, modFuncNames)
	case *ast.UnaryExpr:
		prefixCallsInExpr(e.Operand, moduleName, modFuncNames)
	case *ast.IndexExpr:
		prefixCallsInExpr(e.Array, moduleName, modFuncNames)
		prefixCallsInExpr(e.Index, moduleName, modFuncNames)
	case *ast.ArrayLitExpr:
		for _, elem := range e.Elems {
			prefixCallsInExpr(elem, moduleName, modFuncNames)
		}
	case *ast.StructLitExpr:
		for _, val := range e.FieldValues {
			prefixCallsInExpr(val, moduleName, modFuncNames)
		}
	case *ast.FieldAccessExpr:
		prefixCallsInExpr(e.Object, moduleName, modFuncNames)
	case *ast.SpawnExpr:
		if e.Call != nil {
			prefixCallsInExpr(e.Call, moduleName, modFuncNames)
		}
		for _, stmt := range e.Body {
			prefixCallsInStmt(stmt, moduleName, modFuncNames)
		}
	case *ast.ReceiveExpr:
		prefixCallsInExpr(e.Source, moduleName, modFuncNames)
	}
}

// FlattenStructMethods extracts methods from struct definitions into top-level
// functions with a "self" parameter of the struct type. Method bodies have bare
// field-name references rewritten to self.fieldName.
func FlattenStructMethods(program *ast.Program) {
	for _, sd := range program.Structs {
		if len(sd.Methods) == 0 {
			continue
		}

		// Build set of field names for this struct
		fieldNames := map[string]bool{}
		for _, f := range sd.Fields {
			fieldNames[f.Name] = true
		}

		structType, ok := ast.LookupStructType(sd.Name)
		if !ok {
			continue
		}

		for _, method := range sd.Methods {
			flatFn := ast.Function{
				Name:       sd.Name + "_" + method.Name,
				ReturnType: method.ReturnType,
				IsPrivate:  method.IsPrivate,
			}

			selfParam := ast.Param{Name: "self", Type: structType}
			flatFn.Params = append(flatFn.Params, selfParam)
			flatFn.Params = append(flatFn.Params, method.Params...)

			flatFn.Body = make([]ast.Stmt, len(method.Body))
			copy(flatFn.Body, method.Body)

			localNames := map[string]bool{}
			for _, p := range method.Params {
				localNames[p.Name] = true
			}

			for _, stmt := range flatFn.Body {
				rewriteFieldRefsInStmt(stmt, fieldNames, localNames)
			}

			program.Functions = append(program.Functions, flatFn)
		}
	}
}

// rewriteFieldRefsInStmt walks a statement and rewrites bare identifiers that
// match struct field names into FieldAccessExpr{Object: Ident{"self"}, Field: name}.
func rewriteFieldRefsInStmt(stmt ast.Stmt, fieldNames, localNames map[string]bool) {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		s.Expr = rewriteFieldRefsInExpr(s.Expr, fieldNames, localNames)
	case *ast.LetStmt:
		s.Value = rewriteFieldRefsInExpr(s.Value, fieldNames, localNames)
		localNames[s.Name] = true
	case *ast.AssignStmt:
		if fieldNames[s.Name] && !localNames[s.Name] {
			// Rewrite to field assignment: self.field = value
		}
		s.Value = rewriteFieldRefsInExpr(s.Value, fieldNames, localNames)
	case *ast.ReturnStmt:
		if s.Value != nil {
			s.Value = rewriteFieldRefsInExpr(s.Value, fieldNames, localNames)
		}
	case *ast.IfStmt:
		s.Cond = rewriteFieldRefsInExpr(s.Cond, fieldNames, localNames)
		for _, st := range s.Then {
			rewriteFieldRefsInStmt(st, fieldNames, localNames)
		}
		for _, st := range s.Else {
			rewriteFieldRefsInStmt(st, fieldNames, localNames)
		}
	case *ast.WhileStmt:
		s.Cond = rewriteFieldRefsInExpr(s.Cond, fieldNames, localNames)
		for _, st := range s.Body {
			rewriteFieldRefsInStmt(st, fieldNames, localNames)
		}
	case *ast.ForStmt:
		if s.Init != nil {
			rewriteFieldRefsInStmt(s.Init, fieldNames, localNames)
		}
		if s.Cond != nil {
			s.Cond = rewriteFieldRefsInExpr(s.Cond, fieldNames, localNames)
		}
		if s.Post != nil {
			rewriteFieldRefsInStmt(s.Post, fieldNames, localNames)
		}
		for _, st := range s.Body {
			rewriteFieldRefsInStmt(st, fieldNames, localNames)
		}
	case *ast.ForeachStmt:
		s.Iterable = rewriteFieldRefsInExpr(s.Iterable, fieldNames, localNames)
		for _, st := range s.Body {
			rewriteFieldRefsInStmt(st, fieldNames, localNames)
		}
	case *ast.BlockStmt:
		for _, st := range s.Stmts {
			rewriteFieldRefsInStmt(st, fieldNames, localNames)
		}
	case *ast.IndexAssignStmt:
		s.Array = rewriteFieldRefsInExpr(s.Array, fieldNames, localNames)
		s.Index = rewriteFieldRefsInExpr(s.Index, fieldNames, localNames)
		s.Value = rewriteFieldRefsInExpr(s.Value, fieldNames, localNames)
	case *ast.FieldAssignStmt:
		s.Object = rewriteFieldRefsInExpr(s.Object, fieldNames, localNames)
		s.Value = rewriteFieldRefsInExpr(s.Value, fieldNames, localNames)
	case *ast.CompoundAssignStmt:
		s.Value = rewriteFieldRefsInExpr(s.Value, fieldNames, localNames)
	case *ast.SendStmt:
		if s.Target != nil {
			s.Target = rewriteFieldRefsInExpr(s.Target, fieldNames, localNames)
		}
		s.Value = rewriteFieldRefsInExpr(s.Value, fieldNames, localNames)
	}
}

// rewriteFieldRefsInExpr rewrites bare identifiers matching struct fields to self.field.
func rewriteFieldRefsInExpr(expr ast.Expr, fieldNames, localNames map[string]bool) ast.Expr {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *ast.Ident:
		if fieldNames[e.Name] && !localNames[e.Name] {
			return &ast.FieldAccessExpr{Object: &ast.Ident{Name: "self"}, Field: e.Name}
		}
		return e
	case *ast.CallExpr:
		// Rewrite field-qualified calls: database.query() -> self.database.query()
		// When Module matches a struct field name, it's a method call on that field,
		// not a module call. Prefix with "self." so checker/codegen resolve it correctly.
		if e.Module != "" && fieldNames[e.Module] && !localNames[e.Module] {
			e.Module = "self." + e.Module
		}
		for i, arg := range e.Args {
			e.Args[i] = rewriteFieldRefsInExpr(arg, fieldNames, localNames)
		}
		return e
	case *ast.BinaryExpr:
		e.Left = rewriteFieldRefsInExpr(e.Left, fieldNames, localNames)
		e.Right = rewriteFieldRefsInExpr(e.Right, fieldNames, localNames)
		return e
	case *ast.UnaryExpr:
		e.Operand = rewriteFieldRefsInExpr(e.Operand, fieldNames, localNames)
		return e
	case *ast.IndexExpr:
		e.Array = rewriteFieldRefsInExpr(e.Array, fieldNames, localNames)
		e.Index = rewriteFieldRefsInExpr(e.Index, fieldNames, localNames)
		return e
	case *ast.ArrayLitExpr:
		for i, elem := range e.Elems {
			e.Elems[i] = rewriteFieldRefsInExpr(elem, fieldNames, localNames)
		}
		return e
	case *ast.StructLitExpr:
		for i, val := range e.FieldValues {
			e.FieldValues[i] = rewriteFieldRefsInExpr(val, fieldNames, localNames)
		}
		return e
	case *ast.FieldAccessExpr:
		e.Object = rewriteFieldRefsInExpr(e.Object, fieldNames, localNames)
		return e
	case *ast.SpawnExpr:
		if e.Call != nil {
			e.Call = rewriteFieldRefsInExpr(e.Call, fieldNames, localNames)
		}
		for _, stmt := range e.Body {
			rewriteFieldRefsInStmt(stmt, fieldNames, localNames)
		}
		return e
	case *ast.ReceiveExpr:
		e.Source = rewriteFieldRefsInExpr(e.Source, fieldNames, localNames)
		return e
	default:
		return e
	}
}
