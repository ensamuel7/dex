package resolve

import "github.com/ensamuel7/dex/ast"

// Read-only traversal for consumers that inspect the AST rather than rewrite it
// — the lint pass, mainly. The mutating walker treats a non-nil callback result
// as a replacement node and stops descending into the original, so a visitor
// written against it truncates its own traversal the first time a callback
// returns something. These wrappers make that mistake unexpressible.

// VisitStmts calls stmtFn on each statement before its children, and exprFn on
// every expression beneath them. Either callback may be nil. Statements nested
// inside expressions — a lambda or spawn body — are visited too.
func VisitStmts(stmts []ast.Stmt, stmtFn func(ast.Stmt), exprFn func(ast.Expr)) {
	(&walker{stmtFn: stmtFn, exprFn: readOnly(exprFn)}).stmts(stmts)
}

// VisitStmt is VisitStmts for a single statement.
func VisitStmt(stmt ast.Stmt, stmtFn func(ast.Stmt), exprFn func(ast.Expr)) {
	(&walker{stmtFn: stmtFn, exprFn: readOnly(exprFn)}).stmt(stmt)
}

// VisitExpr calls fn on expr and every expression beneath it. Statements inside
// a lambda or spawn body are descended into, but only their expressions are
// reported; use VisitStmts when the statements themselves matter.
func VisitExpr(expr ast.Expr, fn func(ast.Expr)) {
	(&walker{exprFn: readOnly(fn)}).expr(expr)
}

// VisitExprInFrame is VisitExpr restricted to the current call frame: it stops
// at a lambda or spawn body rather than descending into it. An analysis of what
// one call does — which exceptions escape it, say — has to stop there, because
// those statements run when the closure is invoked or on another thread, not
// where they are written.
func VisitExprInFrame(expr ast.Expr, fn func(ast.Expr)) {
	(&walker{exprFn: readOnly(fn), inFrame: true}).expr(expr)
}

// VisitStmtInFrame is VisitStmt restricted to the current call frame, with the
// same stopping rule as VisitExprInFrame. Statement bodies belonging to the
// statement itself — an if, a loop, a try — are still visited.
func VisitStmtInFrame(stmt ast.Stmt, stmtFn func(ast.Stmt), exprFn func(ast.Expr)) {
	(&walker{stmtFn: stmtFn, exprFn: readOnly(exprFn), inFrame: true}).stmt(stmt)
}

// VisitFunctionBodies visits every function in the program, including the
// flattened struct methods appended by FlattenStructMethods.
func VisitFunctionBodies(program *ast.Program, fn func(*ast.Function)) {
	for i := range program.Functions {
		fn(&program.Functions[i])
	}
}

func readOnly(fn func(ast.Expr)) func(ast.Expr) ast.Expr {
	if fn == nil {
		return nil
	}
	return func(e ast.Expr) ast.Expr {
		fn(e)
		return nil
	}
}
