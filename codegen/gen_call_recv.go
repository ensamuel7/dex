package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

// Method calls whose receiver is an expression rather than a named variable —
// sb.toString().len(), reflect.fields(unit).len(), a fluent builder chain.
//
// Rather than duplicating the method tables for a second kind of receiver, the
// receiver is evaluated into a statement-scoped temporary and the call is
// forwarded as if that temporary had been the variable all along. The existing
// named-receiver emission then handles it unchanged, which is the point: there
// is one place that knows what dex_str_trim is called, not two that have to
// agree.
//
// The temporary is what makes this correct rather than merely shorter.
//
// The receiver is evaluated once: some methods interpolate the receiver into
// their C more than once, and a receiver that is a call — toString(), a reflect
// accessor — must not run twice because the method mentions it twice.
//
// Ownership is settled in one place: a receiver expression that minted a
// reference is registered as a statement temp and released when the statement
// ends, exactly as genBorrowed does for a borrowed argument, while a receiver
// that only borrowed is not released. Without this, sb.toString().len() leaks
// the string the chain was built from, because nothing else holds it.
func (g *Generator) genExprReceiverCall(out *strings.Builder, e *ast.CallExpr) bool {
	if e.Recv == nil {
		return false
	}
	declType := g.typeOfExpr(e.Recv)
	// json.Value has its own method emission, which runs before this and knows
	// its own reference rules.
	if declType == ast.TypeJsonValue {
		return false
	}
	// declType is what the temporary is declared as and registered under, so a
	// link that returned &Query is held as a pointer and handed to the next
	// method without another address-of. kindType drives only the choice of
	// which method table applies.
	kindType := declType
	if ast.IsRefType(kindType) {
		kindType = ast.RefInnerType(kindType)
	}
	tmp, ok := g.hoistReceiver(e.Recv, declType)
	if !ok {
		// Nowhere to hoist a declaration to — an if or while condition, which
		// is generated outside any statement prelude. Without this the call
		// fell through to the named-receiver path with an empty receiver and
		// emitted `isEmpty()`, a call to a function that does not exist, so
		// `if (tags[i].isEmpty())` did not compile at all while the same
		// expression in a `let` did.
		//
		// A statement expression needs no prelude, and unlike hoisting it is
		// also correct in a while condition, which re-evaluates every iteration.
		return g.genExprReceiverCallInline(out, e, declType, kindType)
	}
	g.registerReceiverTemp(tmp, declType, kindType)
	g.genCallExpr(out, g.forwardedReceiverCall(e, tmp))
	return true
}

// registerReceiverTemp puts the temporary into the maps the named-receiver path
// consults, so it recognises it as a string, array, map, builder or struct.
func (g *Generator) registerReceiverTemp(tmp string, declType, kindType ast.Type) {
	g.varTypes[tmp] = declType
	switch {
	case kindType == ast.TypeString:
		g.strVars[tmp] = true
	case kindType == ast.TypeStringBuilder:
		g.sbVars[tmp] = true
	case ast.IsArrayType(kindType):
		g.arrVars[tmp] = kindType
	case ast.IsMapType(kindType):
		g.mapVars[tmp] = kindType
	}
}

func (g *Generator) forwardedReceiverCall(e *ast.CallExpr, tmp string) *ast.CallExpr {
	return &ast.CallExpr{
		Pos:      e.Pos,
		Module:   tmp,
		Name:     e.Name,
		Args:     e.Args,
		ArgNames: e.ArgNames,
		// Carried over, or a struct method would be emitted as a bare call to a
		// function named after the method with no receiver at all.
		IsMethodCall: e.IsMethodCall,
		StructType:   e.StructType,
		ResolvedType: e.ResolvedType,
	}
}

// genExprReceiverCallInline is genExprReceiverCall where no statement prelude
// exists to hold the receiver's declaration, so the declaration, the call and
// the release live in a statement expression instead.
func (g *Generator) genExprReceiverCallInline(out *strings.Builder, e *ast.CallExpr, declType, kindType ast.Type) bool {
	tmp := g.nextTemp()
	g.registerReceiverTemp(tmp, declType, kindType)

	// Asked of the forwarded call, not of e: typeOfExpr cannot resolve a method
	// through Recv, and reading void there discarded the result of every
	// value-returning method in a condition.
	fwd := g.forwardedReceiverCall(e, tmp)
	var call strings.Builder
	g.genCallExpr(&call, fwd)
	retType := g.typeOfExpr(fwd)

	var owned bool
	switch {
	case ast.IsHeapType(declType):
		owned = g.isNewAlloc(e.Recv)
	case ast.IsStructType(declType) && ast.NeedsRelease(declType):
		owned = !g.borrowsHeapValue(e.Recv)
	}

	out.WriteString(fmt.Sprintf("({ %s %s = ", g.cType(declType), tmp))
	g.genExpr(out, e.Recv)
	out.WriteString("; ")
	// A void method has no value to bind, and __auto_type cannot name void.
	if retType == ast.TypeVoid {
		out.WriteString(call.String())
		out.WriteString("; ")
		if owned {
			out.WriteString(fmt.Sprintf("dex_release(%s); ", tmp))
		}
		out.WriteString("})")
		return true
	}
	res := g.nextTemp()
	out.WriteString(fmt.Sprintf("__auto_type %s = %s; ", res, call.String()))
	if owned {
		out.WriteString(fmt.Sprintf("dex_release(%s); ", tmp))
	}
	out.WriteString(fmt.Sprintf("%s; })", res))
	return true
}

// hoistReceiver evaluates expr into a statement-scoped temporary and returns its
// name. Unlike genBorrowed it always creates the temporary, because a method may
// mention its receiver more than once and the receiver must not be re-evaluated;
// it is only *released* if the expression minted the reference.
func (g *Generator) hoistReceiver(expr ast.Expr, recvType ast.Type) (string, bool) {
	if !g.hoistingEnabled() {
		return "", false
	}
	// Generated against a fresh prelude so anything it hoists in turn is declared
	// before this declaration rather than spliced into the middle of it.
	savedPrelude := g.stmtPrelude
	g.stmtPrelude = &strings.Builder{}
	var exprBuf strings.Builder
	g.genExpr(&exprBuf, expr)
	nested := g.stmtPrelude.String()
	g.stmtPrelude = savedPrelude

	tmp := g.nextTemp()
	g.stmtPrelude.WriteString(nested)
	g.stmtPrelude.WriteString(fmt.Sprintf("%s %s = %s; ", g.cType(recvType), tmp, exprBuf.String()))

	var owned bool
	switch {
	case ast.IsHeapType(recvType):
		owned = g.isNewAlloc(expr)
	case ast.IsStructType(recvType) && ast.NeedsRelease(recvType):
		owned = !g.borrowsHeapValue(expr)
	}
	if owned {
		g.stmtTemps = append(g.stmtTemps, scopeVar{name: tmp, typ: recvType})
	}
	return tmp, true
}
