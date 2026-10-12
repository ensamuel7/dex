package lint

import (
	"fmt"
	"path/filepath"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/resolve"
	"github.com/ensamuel7/dex/stdlib"
)

// Whole-program questions: which names are referenced anywhere, which imports
// earn their place, which private functions nothing calls, and which statements
// nothing can reach.

func fmtPos(p ast.Pos) string {
	if p.File != "" {
		return fmt.Sprintf("%s:%d:%d", filepath.Base(p.File), p.Line, p.Col)
	}
	return fmt.Sprintf("line %d", p.Line)
}

// idents returns every bare name mentioned anywhere in the program. A function
// named without being called is a function value — passed to http.route, stored
// in a variable — so this is what separates "nothing calls it" from "nothing
// uses it".
func (l *linter) idents() map[string]bool {
	if l.identCache != nil {
		return l.identCache
	}
	names := map[string]bool{}
	collect := func(e ast.Expr) {
		switch n := e.(type) {
		case *ast.Ident:
			names[n.Name] = true
		case *ast.FieldAccessExpr:
			// A method value reads as a field access: `h.handle` passed along.
			names[n.Field] = true
		}
	}
	for i := range l.program.Functions {
		resolve.VisitStmts(l.program.Functions[i].Body, nil, collect)
	}
	for i := range l.program.GlobalLets {
		resolve.VisitExpr(l.program.GlobalLets[i].Value, collect)
	}
	l.identCache = names
	return names
}

// usedAsValue reports whether a function is referred to without being called.
// Such a function's signature is fixed by whatever receives it, so an unused
// parameter there is a contract rather than an oversight.
func (l *linter) usedAsValue(name string) bool {
	return l.idents()[name]
}

// valueUse reports whether a function is handed around rather than only
// called. A struct method is passed by its own name — ws.handleConnect(onOpen)
// — and merged under a flat one, so both spellings have to be tried.
func (l *linter) valueUse(fn *ast.Function) bool {
	if l.usedAsValue(fn.Name) {
		return true
	}
	if _, method, ok := splitMethodName(fn.Name); ok {
		return l.usedAsValue(method)
	}
	return false
}

// calls returns every user function the program calls, by the flat name the
// function is registered under.
func (l *linter) calls() map[string]bool {
	if l.callCache != nil {
		return l.callCache
	}
	called := map[string]bool{}
	collect := func(e ast.Expr) {
		if call, ok := e.(*ast.CallExpr); ok {
			if name := l.calleeName(call); name != "" {
				called[name] = true
			}
		}
	}
	for i := range l.program.Functions {
		resolve.VisitStmts(l.program.Functions[i].Body, nil, collect)
	}
	for i := range l.program.GlobalLets {
		resolve.VisitExpr(l.program.GlobalLets[i].Value, collect)
	}
	l.callCache = called
	return called
}

// calleeName resolves a call to the flat name of the user function it reaches,
// or "" when it reaches none — a stdlib call, a constructor, or a call through a
// function value, whose target is not known statically.
func (l *linter) calleeName(e *ast.CallExpr) string {
	if e == nil || e.IsConstructor {
		return ""
	}
	// A method call carries the struct the checker resolved it against, which is
	// what makes the flattened name recoverable.
	if e.IsMethodCall && ast.IsStructType(e.StructType) {
		return ast.StructName(e.StructType) + "_" + e.Name
	}
	if e.Module == "" {
		return e.Name
	}
	// A user module's functions were merged under module_name; a stdlib
	// module's were not merged at all.
	if l.userModules()[headName(e.Module)] {
		return headName(e.Module) + "_" + e.Name
	}
	return ""
}

func (l *linter) userModules() map[string]bool {
	if l.moduleCache != nil {
		return l.moduleCache
	}
	mods := make(map[string]bool, len(l.program.UserModules))
	for _, m := range l.program.UserModules {
		mods[m] = true
	}
	l.moduleCache = mods
	return mods
}

// methodNames returns the flat names of every struct method, so the unused
// check can leave them alone: a method is reached through a receiver, and a
// method value through a field access, neither of which reads as a plain call.
func (l *linter) methodNames() map[string]bool {
	names := map[string]bool{}
	for _, sd := range l.program.Structs {
		for _, m := range sd.Methods {
			names[sd.Name+"_"+m.Name] = true
		}
	}
	return names
}

func (l *linter) checkPrivateFunctions() {
	called := l.calls()
	idents := l.idents()
	methods := l.methodNames()

	for i := range l.program.Functions {
		fn := &l.program.Functions[i]
		switch {
		case !fn.IsPrivate, fn.Name == "main":
			continue
		case methods[fn.Name]:
			// A private method: reached through a receiver, not by name.
			continue
		case called[fn.Name] || idents[fn.Name]:
			continue
		case allows(fn.Annotations, RuleUnusedFunc):
			continue
		}
		l.report(RuleUnusedFunc, fn.Pos, "private function '%s' is never called", fn.Name)
	}
}

// checkImports reports an imported module that nothing references. A module
// that declares types is left alone: `import "json"` earns its keep through a
// `json.Value` annotation, and a type annotation is resolved during parsing —
// by the time lint runs there is no mention of the module left to find.
func (l *linter) checkImports() {
	if l.opts.Disable[RuleUnusedImport] {
		return
	}
	used := map[string]bool{}
	collect := func(e ast.Expr) {
		switch n := e.(type) {
		case *ast.CallExpr:
			if n.Module != "" {
				used[headName(n.Module)] = true
			}
		case *ast.Ident:
			used[n.Name] = true
		case *ast.FieldAccessExpr:
			if obj, ok := n.Object.(*ast.Ident); ok {
				used[obj.Name] = true
			}
		}
	}
	for i := range l.program.Functions {
		resolve.VisitStmts(l.program.Functions[i].Body, nil, collect)
	}
	for i := range l.program.GlobalLets {
		resolve.VisitExpr(l.program.GlobalLets[i].Value, collect)
	}

	for _, imp := range l.program.Imports {
		name := imp.Alias
		if name == "" {
			name = filepath.Base(imp.Path)
		}
		if used[name] {
			continue
		}
		if mod := stdlib.Lookup(imp.Path); mod == nil || len(mod.Types) > 0 {
			continue
		}
		l.report(RuleUnusedImport, imp.Pos, "import '%s' is never used", imp.Path)
	}
}

// checkUnreachable reports the first statement after one that always leaves the
// block.
func (l *linter) checkUnreachable() {
	check := func(stmts []ast.Stmt) {
		for i, stmt := range stmts {
			kw := terminator(stmt)
			if kw == "" || i == len(stmts)-1 {
				continue
			}
			next := stmts[i+1]
			l.report(RuleUnreachable, ast.StmtPos(next), "unreachable: the '%s' above always leaves this block", kw)
			return
		}
	}
	for i := range l.program.Functions {
		body := l.program.Functions[i].Body
		check(body)
		resolve.VisitStmts(body, func(st ast.Stmt) {
			for _, block := range childBlocks(st) {
				check(block)
			}
		}, func(e ast.Expr) {
			switch n := e.(type) {
			case *ast.LambdaExpr:
				check(n.Body)
			case *ast.SpawnExpr:
				check(n.Body)
			}
		})
	}
}

func terminator(stmt ast.Stmt) string {
	switch stmt.(type) {
	case *ast.ReturnStmt:
		return "return"
	case *ast.ThrowStmt:
		return "throw"
	case *ast.BreakStmt:
		return "break"
	case *ast.ContinueStmt:
		return "continue"
	}
	return ""
}

// checkAllowAnnotations reports an #[allow(...)] that names nothing. A
// suppression that silences no rule is worse than no suppression at all: it
// reads as handled and behaves as forgotten.
func (l *linter) checkAllowAnnotations() {
	seen := func(annotations []string, pos ast.Pos, what string) {
		for _, a := range annotations {
			name, ok := ast.ParseAllowAnnotation(a)
			if ok && !knownAllowTarget(name) {
				l.report(RuleUnknownAllow, pos,
					"#[allow(%s)] on %s names no rule (dex lint --rules lists them)", name, what)
			}
		}
	}
	for i := range l.program.Functions {
		fn := &l.program.Functions[i]
		seen(fn.Annotations, fn.Pos, "'"+displayName(fn.Name)+"'")
		for j := range fn.Params {
			seen(fn.Params[j].Annotations, fn.Params[j].Pos, "parameter '"+fn.Params[j].Name+"'")
		}
		resolve.VisitStmts(fn.Body, func(st ast.Stmt) {
			if let, ok := st.(*ast.LetStmt); ok {
				seen(let.Annotations, let.Pos, "'"+let.Name+"'")
			}
		}, nil)
	}
	for i := range l.program.GlobalLets {
		gl := &l.program.GlobalLets[i]
		seen(gl.Annotations, gl.Pos, "'"+gl.Name+"'")
	}
}
