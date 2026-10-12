package lint

import (
	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/resolve"
)

// Scope analysis: which declarations are read, and which hide a name that an
// enclosing scope already uses. Expression traversal is delegated to the shared
// walker in resolve; what this file adds is the block structure the walker does
// not model, because scoping is the whole question here.

type binding struct {
	name        string
	pos         ast.Pos
	what        string // how the diagnostic names it: "variable", "parameter", ...
	rule        string
	annotations []string
	read        bool
	// quiet marks a binding that exists only so names resolve against it. The
	// receiver of a flattened method and a catch clause's exception are both
	// introduced by the language rather than asked for, so going unread is not
	// a finding.
	quiet bool
}

type scope struct {
	parent *scope
	names  map[string]*binding
	order  []*binding // declaration order, so findings come out in source order
}

func newScope(parent *scope) *scope {
	return &scope{parent: parent, names: map[string]*binding{}}
}

func (s *scope) lookup(name string) *binding {
	for sc := s; sc != nil; sc = sc.parent {
		if b, ok := sc.names[name]; ok {
			return b
		}
	}
	return nil
}

func (s *scope) markRead(name string) {
	if b := s.lookup(name); b != nil {
		b.read = true
	}
}

func (l *linter) checkScopes() {
	for i := range l.program.Functions {
		fn := &l.program.Functions[i]
		sc := newScope(nil)

		reportParams := !allows(fn.Annotations, RuleUnusedParam) && !l.valueUse(fn)
		for j := range fn.Params {
			p := &fn.Params[j]
			l.declare(sc, &binding{
				name:        p.Name,
				pos:         p.Pos,
				what:        "parameter",
				rule:        RuleUnusedParam,
				annotations: p.Annotations,
				// `self` is the receiver the method flattening inserts, not
				// something the author wrote, and a method that reads no field
				// does not want to be told about it.
				quiet: p.Name == "self" || !reportParams,
			})
		}

		l.walkBlock(fn.Body, sc)
		l.closeScope(sc)
	}
}

// declare adds a binding, reporting the shadowing of any enclosing name first —
// the check has to run before the new binding is in place.
func (l *linter) declare(sc *scope, b *binding) {
	if b.name == "" {
		return
	}
	if outer := sc.lookup(b.name); outer != nil && b.name != "self" {
		l.report(RuleShadow, b.pos, "%s '%s' hides the %s declared at %s",
			b.what, b.name, outer.what, posString(outer.pos))
	}
	sc.names[b.name] = b
	sc.order = append(sc.order, b)
}

// closeScope reports the bindings in a scope that were never read. It runs when
// the scope ends, so an inner block that reads a name has already been seen.
func (l *linter) closeScope(sc *scope) {
	for _, b := range sc.order {
		if b.read || b.quiet || allows(b.annotations, b.rule) {
			continue
		}
		l.report(b.rule, b.pos, "%s '%s' is declared but never read", b.what, b.name)
	}
}

// walkBlock analyses a statement list in a scope of its own.
func (l *linter) walkBlock(stmts []ast.Stmt, parent *scope) {
	if stmts == nil {
		return
	}
	sc := newScope(parent)
	l.walkStmts(stmts, sc)
	l.closeScope(sc)
}

func (l *linter) walkStmts(stmts []ast.Stmt, sc *scope) {
	for _, stmt := range stmts {
		l.walkStmt(stmt, sc)
	}
}

func (l *linter) walkStmt(stmt ast.Stmt, sc *scope) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		// The initialiser is read in the enclosing scope: `let x = x` reads the
		// outer x, and declaring first would make it read itself.
		l.visitExprs(s.Value, sc)
		what := "variable"
		if s.IsConst {
			what = "constant"
		}
		for _, name := range append([]string{s.Name}, s.Names...) {
			l.declare(sc, &binding{name: name, pos: s.Pos, what: what, rule: RuleUnusedVar, annotations: s.Annotations})
		}

	case *ast.DestructureLetStmt:
		l.visitExprs(s.Value, sc)
		for _, name := range s.Names {
			l.declare(sc, &binding{name: name, pos: s.Pos, what: "variable", rule: RuleUnusedVar})
		}

	case *ast.AssignStmt:
		// Assigning is not reading. A variable only ever written to is exactly
		// what unused-var is for, so the target is deliberately not marked.
		l.visitExprs(s.Value, sc)
	case *ast.CompoundAssignStmt:
		l.visitExprs(s.Value, sc)
	case *ast.IncrementStmt, *ast.DecrementStmt:
		// Nothing read, nothing nested.

	case *ast.IndexAssignStmt:
		l.visitExprs(s.Array, sc)
		l.visitExprs(s.Index, sc)
		l.visitExprs(s.Value, sc)
	case *ast.FieldAssignStmt:
		l.visitExprs(s.Object, sc)
		l.visitExprs(s.Value, sc)

	case *ast.ExprStmt:
		l.visitExprs(s.Expr, sc)
	case *ast.ReturnStmt:
		l.visitExprs(s.Value, sc)
	case *ast.ThrowStmt:
		l.visitExprs(s.Value, sc)
	case *ast.DeferStmt:
		l.visitExprs(s.Expr, sc)
	case *ast.SendStmt:
		l.visitExprs(s.Target, sc)
		l.visitExprs(s.Value, sc)

	case *ast.IfStmt:
		l.visitExprs(s.Cond, sc)
		l.walkBlock(s.Then, sc)
		l.walkBlock(s.Else, sc)

	case *ast.WhileStmt:
		l.visitExprs(s.Cond, sc)
		l.walkBlock(s.Body, sc)

	case *ast.ForStmt:
		// The init declaration belongs to the loop, not to the body: it is
		// visible to the condition and the post statement too.
		loop := newScope(sc)
		l.walkStmt(s.Init, loop)
		l.visitExprs(s.Cond, loop)
		l.walkStmt(s.Post, loop)
		l.walkBlock(s.Body, loop)
		l.closeScope(loop)

	case *ast.ForeachStmt:
		l.visitExprs(s.Iterable, sc)
		loop := newScope(sc)
		for _, name := range []string{s.IndexVar, s.ValueVar} {
			l.declare(loop, &binding{name: name, pos: s.Pos, what: "loop variable", rule: RuleUnusedVar})
		}
		l.walkBlock(s.Body, loop)
		l.closeScope(loop)

	case *ast.SwitchStmt:
		l.visitExprs(s.Tag, sc)
		for i := range s.Cases {
			for _, v := range s.Cases[i].Values {
				l.visitExprs(v, sc)
			}
			l.walkBlock(s.Cases[i].Body, sc)
		}
		l.walkBlock(s.Default, sc)

	case *ast.TryCatchStmt:
		l.walkBlock(s.Body, sc)
		for i := range s.Catches {
			clause := &s.Catches[i]
			catch := newScope(sc)
			l.declare(catch, &binding{
				name: clause.VarName, pos: clause.Pos, what: "caught exception",
				rule: RuleUnusedVar, quiet: true,
			})
			l.walkBlock(clause.Body, catch)
			l.closeScope(catch)
		}
		l.walkBlock(s.FinallyBody, sc)

	case *ast.BlockStmt:
		l.walkBlock(s.Stmts, sc)
	}
}

// visitExprs marks every name an expression reads, and analyses any lambda or
// spawn body it contains as a scope nested in this one. The shared walker
// descends into those bodies already — which is what makes a captured variable
// count as read — so the nested analysis is only about declarations made
// inside them.
func (l *linter) visitExprs(expr ast.Expr, sc *scope) {
	if expr == nil {
		return
	}
	var lambdas []*ast.LambdaExpr
	var spawns []*ast.SpawnExpr
	resolve.VisitExpr(expr, func(e ast.Expr) {
		switch n := e.(type) {
		case *ast.Ident:
			sc.markRead(n.Name)
		case *ast.CallExpr:
			// A method call names its receiver in Module rather than as an
			// expression: hub.send(x) reads hub.
			if n.Module != "" {
				sc.markRead(headName(n.Module))
			}
			// Calling a function value reads the binding that holds it. The
			// call is spelled exactly like a call to a top-level function, so
			// this is a no-op unless a local of that name is in scope — which
			// is the whole point: `let add7 = makeAdder(7)` followed by
			// `add7(1)` does use add7.
			sc.markRead(n.Name)
		case *ast.FieldAccessExpr:
			// Covered by the Ident case for the object, nothing extra.
		case *ast.LambdaExpr:
			if !l.seen[n] {
				l.seen[n] = true
				lambdas = append(lambdas, n)
			}
		case *ast.SpawnExpr:
			if !l.seen[n] {
				l.seen[n] = true
				spawns = append(spawns, n)
			}
		}
	})
	for _, lam := range lambdas {
		inner := newScope(sc)
		for i := range lam.Params {
			p := &lam.Params[i]
			l.declare(inner, &binding{
				name: p.Name, pos: p.Pos, what: "parameter",
				rule: RuleUnusedParam, annotations: p.Annotations,
			})
		}
		l.walkBlock(lam.Body, inner)
		l.closeScope(inner)
	}
	for _, sp := range spawns {
		l.walkBlock(sp.Body, sc)
	}
}

// headName returns the first segment of a dotted receiver: the variable in
// cmd.action.isEmpty() is cmd.
func headName(module string) string {
	for i := 0; i < len(module); i++ {
		if module[i] == '.' {
			return module[:i]
		}
	}
	return module
}

func posString(p ast.Pos) string {
	if p.Line == 0 {
		return "an enclosing scope"
	}
	return fmtPos(p)
}
