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

// Registers struct and enum declarations from user modules globally so the main
// file's parser recognizes their literal and type-annotation syntax.
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

	// Placeholders; the full definitions are filled in when the modules are
	// parsed, and registration is idempotent.
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

// Sub-imports are resolved before this module is parsed, so their struct types
// are registered by the time its type annotations are read.
func resolveModuleFile(filePath, absPath, moduleName string, program *ast.Program, visited, processing map[string]bool) error {
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

	importPaths := ExtractImportPaths(tokens)

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

	typeNames := stdlib.ModuleTypesForImports(importPaths)
	p := parser.New(tokens)
	for _, name := range typeNames {
		p.AddStructName(name)
	}
	for _, name := range ast.AllStructNames() {
		p.AddStructName(name)
	}
	modProgram, errs := p.Parse()
	if len(errs) > 0 {
		return errs[0]
	}

	FlattenStructMethods(modProgram)

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

	for _, modImp := range modProgram.Imports {
		if stdlib.Lookup(modImp.Path) != nil && !containsImport(program.Imports, modImp.Path) {
			program.Imports = append(program.Imports, modImp)
		}
	}

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

// Rewrites every reference to one of this module's own top-level names into the
// flat, prefixed name it is merged under.
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

// stmtFn, when non-nil, runs on each statement before its children — assignment
// targets are plain strings rather than expressions, so a rewrite needs it.
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
