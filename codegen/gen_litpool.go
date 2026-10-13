package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/resolve"
)

// A string literal evaluates to the same bytes every time, so it is emitted
// once as an immortal static (DEX_LIT_DEF in strings.c) instead of being
// malloc'd and copied at each evaluation. In a loop that turned one malloc and
// one free per iteration per literal into a pointer to .data.
//
// Ownership is deliberately left alone. isNewAlloc still answers true for a
// StringLit, so every retain and release codegen already emitted is still
// emitted — they are simply no-ops on an immortal object. That is what makes
// this safe to add without revisiting a single ownership decision: a literal
// that is over-released leaks nothing and a literal that is under-released
// frees nothing.

// collectStringLiterals assigns a pool symbol to every string literal in the
// program. It runs before any output is written, because the definitions are
// emitted with the string runtime and the use sites come later.
//
// The traversal goes through resolve.VisitFunctionBodies and resolve.VisitStmts
// rather than codegen's own scan pass: scan is a hand-written walker, and a
// literal it failed to reach would be referenced as a symbol that was never
// defined. litRef falls back for that case anyway, so a miss costs the
// optimisation rather than the build.
func (g *Generator) collectStringLiterals(program *ast.Program) {
	record := func(e ast.Expr) {
		lit, ok := e.(*ast.StringLit)
		if !ok {
			return
		}
		g.poolLiteral(lit.Value)
	}
	for i := range program.GlobalLets {
		resolve.VisitStmt(&program.GlobalLets[i], nil, record)
	}
	resolve.VisitFunctionBodies(program, func(fn *ast.Function) {
		resolve.VisitStmts(fn.Body, nil, record)
	})
}

// poolLiteral registers one value, if it can be pooled at all.
func (g *Generator) poolLiteral(value string) {
	if _, seen := g.litPool[value]; seen {
		return
	}
	// dex_string_from_lit measures with strlen, so a literal holding a NUL is
	// truncated there. The pool takes its length from sizeof and would keep the
	// whole thing — a different string. Pooling is the more faithful of the two
	// but it is not the behaviour this program has today, so the NUL case keeps
	// the allocating path and changes nothing.
	if strings.ContainsRune(value, 0) {
		return
	}
	g.litPool[value] = fmt.Sprintf("_dex_lit_%d", len(g.litOrder))
	g.litOrder = append(g.litOrder, value)
}

// litRef is the C expression for a string literal's value: the pooled static
// where there is one, and otherwise the allocating call that was emitted before
// this pass existed.
func (g *Generator) litRef(value string) string {
	if sym, ok := g.litPool[value]; ok {
		return fmt.Sprintf("DEX_LIT(%s)", sym)
	}
	return fmt.Sprintf("dex_string_from_lit(%q)", value)
}

// emitStringLitPool writes the definitions. Called with the string runtime,
// which defines DEX_LIT_DEF and the layout assertions behind it.
func (g *Generator) emitStringLitPool(out *strings.Builder) {
	if len(g.litOrder) == 0 {
		return
	}
	for _, value := range g.litOrder {
		out.WriteString(fmt.Sprintf("DEX_LIT_DEF(%s, %q);\n", g.litPool[value], value))
	}
	out.WriteString("\n")
}
