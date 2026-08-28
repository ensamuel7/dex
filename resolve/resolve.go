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
	if err := resolveImports(program, sourceDir, visited, processing); err != nil {
		return err
	}
	resolveQualifiedGlobals(program)
	return nil
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
	var rewrite func(expr ast.Expr) ast.Expr
	rewrite = func(expr ast.Expr) ast.Expr {
		fa, ok := expr.(*ast.FieldAccessExpr)
		if !ok {
			return nil
		}
		ident, ok := fa.Object.(*ast.Ident)
		if !ok || !modules[ident.Name] {
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

	walkProgramExprs(program, func(expr ast.Expr) ast.Expr {
		return rewrite(expr)
	})
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

	// Collect this module's own function and global names (before prefixing).
	// Globals are prefixed alongside functions: they are merged into one flat
	// namespace, so two modules each declaring `let svc` would otherwise become
	// the same variable without anything reporting it.
	ownNames := map[string]bool{}
	for _, fn := range modProgram.Functions {
		ownNames[fn.Name] = true
	}
	for _, gl := range modProgram.GlobalLets {
		ownNames[gl.Name] = true
	}

	// Prefix only this module's own functions
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

	// Merge functions into program
	for _, fn := range modProgram.Functions {
		if fn.Name == "main" {
			continue
		}
		program.Functions = append(program.Functions, fn)
	}

	// Merge global let declarations from module
	program.GlobalLets = append(program.GlobalLets, modProgram.GlobalLets...)

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
// prefixCallsInStmt rewrites every reference to one of this module's own
// top-level names — functions and module-level values alike — into the flat,
// module-prefixed name they are merged under. Both walkers below are the shared
// generic ones, so a statement kind added to the language is covered here
// automatically rather than being silently skipped.
func prefixCallsInStmt(stmt ast.Stmt, moduleName string, modNames map[string]bool) {
	prefix := func(name string) string { return moduleName + "_" + name }

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
	case *ast.SliceExpr:
		e.Array = rewriteFieldRefsInExpr(e.Array, fieldNames, localNames)
		if e.Start != nil {
			e.Start = rewriteFieldRefsInExpr(e.Start, fieldNames, localNames)
		}
		if e.End != nil {
			e.End = rewriteFieldRefsInExpr(e.End, fieldNames, localNames)
		}
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

// walkProgramExprs visits every expression in the program, replacing any for
// which fn returns a non-nil node. It exists so a rewrite can be expressed once
// rather than repeated across every statement and expression kind.
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

// walkStmtsWith visits every statement and expression. stmtFn, when non-nil, is
// called on each statement before its children are walked — statement targets
// such as an assignment's variable name are plain strings, not expressions, so
// a rewrite that touches them needs this hook.
func walkStmtsWith(stmts []ast.Stmt, stmtFn func(ast.Stmt), exprFn func(ast.Expr) ast.Expr) {
	for _, stmt := range stmts {
		walkStmtWith(stmt, stmtFn, exprFn)
	}
}

func walkStmt(stmt ast.Stmt, fn func(ast.Expr) ast.Expr) {
	walkStmtWith(stmt, nil, fn)
}

func walkStmtWith(stmt ast.Stmt, stmtFn func(ast.Stmt), exprFn func(ast.Expr) ast.Expr) {
	if stmt == nil {
		return
	}
	if stmtFn != nil {
		stmtFn(stmt)
	}
	fn := exprFn
	walkStmts := func(body []ast.Stmt) { walkStmtsWith(body, stmtFn, exprFn) }
	walkStmt := func(st ast.Stmt) { walkStmtWith(st, stmtFn, exprFn) }
	_ = walkStmt
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		s.Expr = walkExpr(s.Expr, fn)
	case *ast.LetStmt:
		s.Value = walkExpr(s.Value, fn)
	case *ast.AssignStmt:
		s.Value = walkExpr(s.Value, fn)
	case *ast.IndexAssignStmt:
		s.Array = walkExpr(s.Array, fn)
		s.Index = walkExpr(s.Index, fn)
		s.Value = walkExpr(s.Value, fn)
	case *ast.FieldAssignStmt:
		s.Value = walkExpr(s.Value, fn)
	case *ast.ReturnStmt:
		s.Value = walkExpr(s.Value, fn)
	case *ast.IfStmt:
		s.Cond = walkExpr(s.Cond, fn)
		walkStmts(s.Then)
		walkStmts(s.Else)
	case *ast.WhileStmt:
		s.Cond = walkExpr(s.Cond, fn)
		walkStmts(s.Body)
	case *ast.ForStmt:
		if s.Init != nil {
			walkStmt(s.Init)
		}
		s.Cond = walkExpr(s.Cond, fn)
		if s.Post != nil {
			walkStmt(s.Post)
		}
		walkStmts(s.Body)
	case *ast.ForeachStmt:
		s.Iterable = walkExpr(s.Iterable, fn)
		walkStmts(s.Body)
	case *ast.SwitchStmt:
		s.Tag = walkExpr(s.Tag, fn)
		for i := range s.Cases {
			for j := range s.Cases[i].Values {
				s.Cases[i].Values[j] = walkExpr(s.Cases[i].Values[j], fn)
			}
			walkStmts(s.Cases[i].Body)
		}
		walkStmts(s.Default)
	case *ast.SendStmt:
		s.Target = walkExpr(s.Target, fn)
		s.Value = walkExpr(s.Value, fn)
	case *ast.ThrowStmt:
		s.Value = walkExpr(s.Value, fn)
	case *ast.TryCatchStmt:
		walkStmts(s.Body)
		walkStmts(s.CatchBody)
		walkStmts(s.FinallyBody)
	case *ast.DeferStmt:
		s.Expr = walkExpr(s.Expr, fn)
	case *ast.BlockStmt:
		walkStmts(s.Stmts)
	}
}

func walkExpr(expr ast.Expr, fn func(ast.Expr) ast.Expr) ast.Expr {
	if expr == nil {
		return nil
	}
	if replacement := fn(expr); replacement != nil {
		return replacement
	}
	switch e := expr.(type) {
	case *ast.CallExpr:
		e.Recv = walkExpr(e.Recv, fn)
		for i := range e.Args {
			e.Args[i] = walkExpr(e.Args[i], fn)
		}
	case *ast.BinaryExpr:
		e.Left = walkExpr(e.Left, fn)
		e.Right = walkExpr(e.Right, fn)
	case *ast.UnaryExpr:
		e.Operand = walkExpr(e.Operand, fn)
	case *ast.IndexExpr:
		e.Array = walkExpr(e.Array, fn)
		e.Index = walkExpr(e.Index, fn)
	case *ast.SliceExpr:
		e.Array = walkExpr(e.Array, fn)
		e.Start = walkExpr(e.Start, fn)
		e.End = walkExpr(e.End, fn)
	case *ast.ArrayLitExpr:
		for i := range e.Elems {
			e.Elems[i] = walkExpr(e.Elems[i], fn)
		}
	case *ast.ObjectLitExpr:
		for i := range e.Values {
			e.Values[i] = walkExpr(e.Values[i], fn)
		}
	case *ast.StructLitExpr:
		for i := range e.FieldValues {
			e.FieldValues[i] = walkExpr(e.FieldValues[i], fn)
		}
	case *ast.FieldAccessExpr:
		e.Object = walkExpr(e.Object, fn)
	case *ast.SpawnExpr:
		e.Call = walkExpr(e.Call, fn)
		walkStmts(e.Body, fn)
	case *ast.ReceiveExpr:
		e.Source = walkExpr(e.Source, fn)
	}
	return expr
}


// withoutShadowed returns names minus any that fn declares as a parameter or
// local. Renaming a shadowed name would rebind a local to a module-level value,
// which is both wrong and hard to see in the resulting error.
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

// collectDeclaredNames gathers every name a statement list introduces, at any
// nesting depth. Block scoping is not modelled: a name declared in an inner
// block withholds the rename for the whole function, which errs toward leaving
// a reference alone. That surfaces as an ordinary "undefined" error rather than
// as a silent rebinding.
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
