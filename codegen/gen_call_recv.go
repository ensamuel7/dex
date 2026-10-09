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
		return false
	}
	// Register the temporary under the maps the named-receiver path consults, so
	// it recognises the temp as a string, array, map, builder or struct.
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
	g.genCallExpr(out, &ast.CallExpr{
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
	})
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
