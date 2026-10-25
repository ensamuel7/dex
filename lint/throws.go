package lint

import (
	"sort"
	"strings"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/resolve"
)

// Throw-effect inference: which exception types each function can let escape,
// and which call sites are left to discover one at runtime.
//
// The whole analysis rests on one fact about the language: nothing in the C
// runtime raises. Every exception starts at a `throw` statement in .dx source,
// so the set of throwing functions is the transitive closure of the throw
// statements over the call graph — no stdlib annotations, no escape hatch for
// something the compiler cannot see.
//
// Two things deliberately stop the traversal. A `throw` inside a lambda runs
// when the lambda is invoked, which may be anywhere, and a `throw` inside a
// spawn unwinds on another thread entirely; neither escapes the function that
// merely wrote it down. A call through a function value stops it too, in the
// other direction: the target is not known statically, so it is assumed not to
// throw rather than guessed at.

// catchAll is the type that handles every exception. It is also the built-in
// type, so a bare `throws` — one naming nothing — reads as this: something
// escapes, and only a catch-all can be sure of stopping it.
const catchAll = "Exception"

// A guard is the set of exception types a position is protected against: the
// catch clauses of every enclosing try. Values are treated as immutable, so
// extending one copies.
type guard struct {
	all   bool
	types map[string]bool
}

func (g guard) covers(typ string) bool {
	return g.all || g.types[typ]
}

// with returns the guard in effect inside a try body.
func (g guard) with(s *ast.TryCatchStmt) guard {
	next := guard{all: g.all, types: make(map[string]bool, len(g.types)+len(s.Catches))}
	for t := range g.types {
		next.types[t] = true
	}
	for i := range s.Catches {
		if s.Catches[i].IsCatchAll() {
			next.all = true
			continue
		}
		next.types[s.Catches[i].TypeName] = true
	}
	return next
}

// declaredGuard is what a signature promises to handle on its caller's behalf.
func declaredGuard(fn *ast.Function) guard {
	g := guard{types: make(map[string]bool, len(fn.ThrowsTypes))}
	if fn.Throws && len(fn.ThrowsTypes) == 0 {
		// A bare `throws` declares an escape without naming it, so anything is
		// covered: there is nothing more specific to hold it to.
		g.all = true
		return g
	}
	for _, t := range fn.ThrowsTypes {
		if t == catchAll {
			g.all = true
		}
		g.types[t] = true
	}
	return g
}

// origin is where an exception type ultimately comes from, so a message can
// point at the throw rather than only at the call.
type origin struct {
	pos ast.Pos
	// isThrow separates a real throw from a function that only declares one: a
	// declared type has a position worth naming, but calling it "raised at"
	// would be a lie.
	isThrow bool
}

// throwSite is an unguarded `throw` in a body.
type throwSite struct {
	typ string
	pos ast.Pos
}

// escapeSite is an unguarded call, with the guard that was in effect: whether
// it needs reporting depends on which types the callee turns out to throw.
type escapeSite struct {
	pos    ast.Pos
	callee string
	guard  guard
}

// effects is what the analysis knows about one function.
type effects struct {
	fn *ast.Function
	// throws are the unguarded throw statements, one per type: the advice they
	// produce is about the signature, so repeating it for every throw of the
	// same type would say the same thing many times.
	throws  []throwSite
	escapes []escapeSite
	// body is the fixpoint result — what escapes on the evidence of the body
	// alone, which is what makes an unnecessary declaration detectable.
	body map[string]origin
}

// escaping visits every type that can leave this function, body and
// declaration together. A declaration counts even with nothing in the body
// that throws: it is a contract with callers, which is what lets a function
// start throwing later without surprising them.
func (e *effects) escaping(visit func(typ string, o origin)) {
	for typ, o := range e.body {
		visit(typ, o)
	}
	declared := e.fn.ThrowsTypes
	if e.fn.Throws && len(declared) == 0 {
		declared = []string{catchAll}
	}
	for _, typ := range declared {
		if _, fromBody := e.body[typ]; !fromBody {
			visit(typ, origin{pos: e.fn.Pos})
		}
	}
}

func (l *linter) checkThrows() {
	all := map[string]*effects{}
	order := make([]*effects, 0, len(l.program.Functions))
	for i := range l.program.Functions {
		fn := &l.program.Functions[i]
		e := &effects{fn: fn, body: map[string]origin{}}
		l.scanGuarded(fn.Body, guard{}, e)
		// A struct's methods are flattened into one function list, and two
		// structs may declare the same method name; the flat name is unique.
		all[fn.Name] = e
		order = append(order, e)
	}

	resolveThrowSets(all, order)

	for _, e := range order {
		l.reportThrowFindings(e, all)
	}
	l.checkCatchClauses()
}

// scanGuarded records every point an exception could leave a body that the
// enclosing try clauses do not already handle. A try with only a finally
// guards nothing: the finally runs and the exception carries on.
func (l *linter) scanGuarded(stmts []ast.Stmt, g guard, e *effects) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.ThrowStmt:
			l.recordThrow(s, g, e)
			l.scanCalls(s.Value, g, e)
			continue

		case *ast.TryCatchStmt:
			// The try body is covered by this try's clauses. The catch and
			// finally bodies are not: an exception raised in either one
			// propagates out past the try that holds them.
			l.scanGuarded(s.Body, g.with(s), e)
			for i := range s.Catches {
				l.scanGuarded(s.Catches[i].Body, g, e)
			}
			l.scanGuarded(s.FinallyBody, g, e)
			continue

		case *ast.DeferStmt:
			// A deferred call runs on the way out of the frame, where no try in
			// this body is still in scope.
			l.scanCalls(s.Expr, guard{}, e)
			continue
		}

		l.scanStmtCalls(stmt, g, e)
		for _, block := range childBlocks(stmt) {
			l.scanGuarded(block, g, e)
		}
	}
}

// recordThrow notes a throw whose type nothing above it catches.
func (l *linter) recordThrow(s *ast.ThrowStmt, g guard, e *effects) {
	typ := thrownTypeName(s.Value)
	// The checker's type is authoritative. Reading the syntax alone named
	// `throw e` an Exception and `throw makeErr()` a "makeErr".
	if ast.IsStructType(s.ValueType) {
		typ = ast.StructName(s.ValueType)
	}
	if g.covers(typ) {
		return
	}
	for _, existing := range e.throws {
		if existing.typ == typ {
			return
		}
	}
	e.throws = append(e.throws, throwSite{typ: typ, pos: s.Pos})
}

// thrownTypeName reads the type off a throw. `throw ParseError(...)` is a
// constructor call; a struct literal carries its type directly. Anything the
// checker resolved to something else falls back to the catch-all name, which
// only happens on a program the checker already rejected.
func thrownTypeName(value ast.Expr) string {
	switch v := value.(type) {
	case *ast.CallExpr:
		if ast.IsStructType(v.StructType) {
			return ast.StructName(v.StructType)
		}
		if v.Module == "" && v.Name != "" {
			return v.Name
		}
	case *ast.StructLitExpr:
		if v.Name != "" {
			return v.Name
		}
	}
	return catchAll
}

// scanStmtCalls records the calls a statement makes in its own expressions,
// without descending into the statement's child blocks — scanGuarded does that,
// because only it knows what a try covers.
func (l *linter) scanStmtCalls(stmt ast.Stmt, g guard, e *effects) {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		l.scanCalls(s.Expr, g, e)
	case *ast.LetStmt:
		l.scanCalls(s.Value, g, e)
	case *ast.DestructureLetStmt:
		l.scanCalls(s.Value, g, e)
	case *ast.AssignStmt:
		l.scanCalls(s.Value, g, e)
	case *ast.CompoundAssignStmt:
		l.scanCalls(s.Value, g, e)
	case *ast.IndexAssignStmt:
		l.scanCalls(s.Array, g, e)
		l.scanCalls(s.Index, g, e)
		l.scanCalls(s.Value, g, e)
	case *ast.FieldAssignStmt:
		l.scanCalls(s.Object, g, e)
		l.scanCalls(s.Value, g, e)
	case *ast.ReturnStmt:
		l.scanCalls(s.Value, g, e)
	case *ast.SendStmt:
		l.scanCalls(s.Target, g, e)
		l.scanCalls(s.Value, g, e)
	case *ast.IfStmt:
		l.scanCalls(s.Cond, g, e)
	case *ast.WhileStmt:
		l.scanCalls(s.Cond, g, e)
	case *ast.ForStmt:
		l.scanStmtCalls(s.Init, g, e)
		l.scanCalls(s.Cond, g, e)
		l.scanStmtCalls(s.Post, g, e)
	case *ast.ForeachStmt:
		l.scanCalls(s.Iterable, g, e)
	case *ast.SwitchStmt:
		l.scanCalls(s.Tag, g, e)
		for i := range s.Cases {
			for _, v := range s.Cases[i].Values {
				l.scanCalls(v, g, e)
			}
		}
	}
}

func (l *linter) scanCalls(expr ast.Expr, g guard, e *effects) {
	if expr == nil {
		return
	}
	resolve.VisitExprInFrame(expr, func(n ast.Expr) {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return
		}
		if name := l.calleeName(call); name != "" {
			e.escapes = append(e.escapes, escapeSite{pos: call.Pos, callee: name, guard: g})
		}
	})
}

// resolveThrowSets runs the propagation to a fixpoint: a type a callee can
// throw escapes its caller too, unless that call site is guarded against it.
func resolveThrowSets(all map[string]*effects, order []*effects) {
	for _, e := range order {
		for _, t := range e.throws {
			if _, seen := e.body[t.typ]; !seen {
				e.body[t.typ] = origin{pos: t.pos, isThrow: true}
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, e := range order {
			for _, esc := range e.escapes {
				callee, ok := all[esc.callee]
				if !ok {
					continue
				}
				callee.escaping(func(typ string, o origin) {
					if esc.guard.covers(typ) {
						return
					}
					if _, seen := e.body[typ]; seen {
						return
					}
					e.body[typ] = o
					changed = true
				})
			}
		}
	}
}

func (l *linter) reportThrowFindings(e *effects, all map[string]*effects) {
	fn := e.fn
	declared := declaredGuard(fn)

	l.reportUnnecessaryThrows(e)

	if allows(fn.Annotations, RuleUncaughtThrow) {
		return
	}

	// A throw whose type the signature does not declare.
	for _, t := range e.throws {
		if declared.covers(t.typ) {
			continue
		}
		l.reportEscape(fn, t.pos, "this throws "+ast.DisplayStructName(t.typ), []string{t.typ})
	}

	// A call that can raise something neither caught here nor declared — and
	// only when the callee *declared* that it throws.
	//
	// Inference is exact, so it is tempting to report every call that can reach
	// a throw. On a real codebase that is unusable: one guard in a shared query
	// builder reached 500 unguarded call sites, and reporting all of them said
	// nothing a reader could act on, because the decision about where to catch
	// is made once, not 500 times.
	//
	// So a finding follows a declaration. An undeclared throw is reported where
	// it is raised, at the one place that can fix it; writing `throws` there is
	// what tells its callers, and theirs in turn. That is the gradient Java
	// gets from making the declaration mandatory, reached instead by making the
	// obligation follow the annotation — which also means a throw used as an
	// assertion, declared nowhere, stays one finding rather than becoming
	// hundreds.
	for _, esc := range e.escapes {
		callee, ok := all[esc.callee]
		if !ok || !callee.fn.Throws {
			continue
		}
		var uncovered []string
		var raisedAt string
		callee.escaping(func(typ string, o origin) {
			if esc.guard.covers(typ) || declared.covers(typ) {
				return
			}
			uncovered = append(uncovered, typ)
			if raisedAt == "" && o.isThrow && o.pos.Line > 0 && o.pos != esc.pos {
				raisedAt = fmtPos(o.pos)
			}
		})
		if len(uncovered) == 0 {
			continue
		}
		sort.Strings(uncovered)
		what := "'" + l.displayName(esc.callee) + "' may throw " + typeList(uncovered)
		if raisedAt != "" {
			what += " (raised at " + raisedAt + ")"
		}
		l.reportEscape(fn, esc.pos, what, uncovered)
	}
}

// reportEscape says what to do about an exception leaving a function. Which
// advice applies depends on what the signature already says: a function naming
// some types is told to name this one too, a function naming none is told to
// start, and main has no caller to pass anything to — there, catching it is
// the only thing left.
func (l *linter) reportEscape(fn *ast.Function, pos ast.Pos, what string, types []string) {
	switch {
	case fn.Name == "main":
		l.report(RuleUncaughtThrow, pos, "%s, and nothing above main can catch it — wrap it in try/catch", what)
	case len(fn.ThrowsTypes) > 0:
		l.report(RuleUndeclaredThrow, pos, "%s, which '%s' does not declare — add %s to its throws, or catch it here",
			what, l.displayName(fn.Name), typeList(types))
	default:
		l.report(RuleUncaughtThrow, pos, "%s, and '%s' does not declare throws — wrap it in try/catch, or declare the signature throws %s",
			what, l.displayName(fn.Name), typeList(types))
	}
}

func (l *linter) reportUnnecessaryThrows(e *effects) {
	fn := e.fn
	if !fn.Throws || allows(fn.Annotations, RuleUnneededThrows) {
		return
	}
	if len(fn.ThrowsTypes) == 0 {
		if len(e.body) == 0 {
			l.report(RuleUnneededThrows, fn.Pos,
				"'%s' is declared throws but nothing in it can throw", l.displayName(fn.Name))
		}
		return
	}
	for _, typ := range fn.ThrowsTypes {
		if _, thrown := e.body[typ]; thrown {
			continue
		}
		// A catch-all declaration is satisfied by anything escaping at all.
		if typ == catchAll && len(e.body) > 0 {
			continue
		}
		l.report(RuleUnneededThrows, fn.Pos,
			"'%s' declares throws %s but nothing in it throws %s", l.displayName(fn.Name), ast.DisplayStructName(typ), ast.DisplayStructName(typ))
	}
}

// checkCatchClauses reports a handler that can never run: one after a
// catch-all, or a second clause for a type already handled above it.
func (l *linter) checkCatchClauses() {
	for i := range l.program.Functions {
		resolve.VisitStmts(l.program.Functions[i].Body, func(st ast.Stmt) {
			try, ok := st.(*ast.TryCatchStmt)
			if !ok {
				return
			}
			seen := map[string]bool{}
			catchAllAt := ""
			for j := range try.Catches {
				clause := &try.Catches[j]
				switch {
				case catchAllAt != "":
					l.report(RuleUnreachableCatch, clause.Pos,
						"catch (%s: %s) can never run: the clause at %s catches everything",
						clause.VarName, ast.DisplayStructName(clause.TypeName), catchAllAt)
				case seen[clause.TypeName]:
					l.report(RuleUnreachableCatch, clause.Pos,
						"catch (%s: %s) can never run: %s is already caught above",
						clause.VarName, ast.DisplayStructName(clause.TypeName), ast.DisplayStructName(clause.TypeName))
				}
				seen[clause.TypeName] = true
				if clause.IsCatchAll() && catchAllAt == "" {
					catchAllAt = fmtPos(clause.Pos)
				}
			}
		}, nil)
	}
}

// displayName turns the flat name of a method back into something a reader
// recognises: Box_risky is written Box.risky.
func (l *linter) displayName(flat string) string {
	if structName, method, ok := splitMethodName(flat); ok {
		return ast.DisplayStructName(structName) + "." + method
	}
	for _, module := range l.program.UserModules {
		rest, ok := strings.CutPrefix(flat, module+"_")
		if !ok {
			continue
		}
		if structName, method, ok := splitMethodName(rest); ok {
			return ast.DisplayStructName(structName) + "." + method
		}
		return module + "." + rest
	}
	return flat
}

func typeList(types []string) string {
	names := make([]string, len(types))
	for i, t := range types {
		names[i] = ast.DisplayStructName(t)
	}
	return strings.Join(names, ", ")
}
