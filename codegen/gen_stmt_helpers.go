package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

// extractNullCheckCodegen detects null comparison patterns in conditions for codegen narrowing.
func extractNullCheckCodegen(cond ast.Expr) (string, bool) {
	binExpr, ok := cond.(*ast.BinaryExpr)
	if !ok {
		return "", false
	}
	if binExpr.Op != ast.BinEq && binExpr.Op != ast.BinNeq {
		return "", false
	}
	if ident, ok := binExpr.Left.(*ast.Ident); ok {
		if _, ok := binExpr.Right.(*ast.NullLit); ok {
			return ident.Name, binExpr.Op == ast.BinNeq
		}
	}
	if _, ok := binExpr.Left.(*ast.NullLit); ok {
		if ident, ok := binExpr.Right.(*ast.Ident); ok {
			return ident.Name, binExpr.Op == ast.BinNeq
		}
	}
	return "", false
}

// emitNarrowing emits a narrowed variable declaration for an optional variable
// and registers it in the narrowedVars map so genExpr uses the narrowed name.
func (g *Generator) emitNarrowing(out *strings.Builder, prefix, varName string) {
	varType, ok := g.varTypes[varName]
	if !ok || !ast.IsOptionalType(varType) {
		return
	}
	inner := ast.OptionalInnerType(varType)
	if ast.IsValueType(inner) {
		narrowedName := "_narrow_" + varName
		out.WriteString(fmt.Sprintf("%s%s %s = %s.value;\n", prefix, g.cType(inner), narrowedName, varName))
		g.narrowedVars[varName] = narrowedName
		g.narrowedTypes[varName] = inner
		return
	}
	if ast.IsStructType(inner) {
		// Optional structs are Dex_Foo* — bind a value copy so everything
		// downstream (field access, encode, calls) sees a plain struct.
		narrowedName := "_narrow_" + varName
		out.WriteString(fmt.Sprintf("%s%s %s = *%s;\n", prefix, g.cType(inner), narrowedName, varName))
		g.narrowedVars[varName] = narrowedName
		g.narrowedTypes[varName] = inner
		return
	}
	// Heap types (string, array, struct array) keep the same C variable — the
	// pointer is already the value — but the narrowed *type* still matters so
	// method calls and field access resolve against the inner type.
	g.narrowedTypes[varName] = inner
}

// clearNarrowing removes a variable from the narrowedVars map.
func (g *Generator) clearNarrowing(varName string) {
	delete(g.narrowedVars, varName)
	delete(g.narrowedTypes, varName)
}

// genForInit generates the init part of a for loop (no trailing semicolon).
func (g *Generator) genForInit(out *strings.Builder, stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		g.varTypes[s.Name] = s.Type
		if s.Type == ast.TypeString {
			g.strVars[s.Name] = true
		}
		if ast.IsArrayType(s.Type) {
			g.arrVars[s.Name] = s.Type
		}
		out.WriteString(fmt.Sprintf("%s %s = ", g.cType(s.Type), s.Name))
		g.genExpr(out, s.Value)
	case *ast.AssignStmt:
		out.WriteString(fmt.Sprintf("%s = ", s.Name))
		g.genExpr(out, s.Value)
	}
}

// genForPost generates the post part of a for loop (no trailing semicolon).
func (g *Generator) genForPost(out *strings.Builder, stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.IncrementStmt:
		if t, ok := g.varTypes[s.Name]; ok && isPrimitiveRef(t) {
			out.WriteString(fmt.Sprintf("(*%s)++", s.Name))
		} else {
			out.WriteString(fmt.Sprintf("%s++", s.Name))
		}
	case *ast.DecrementStmt:
		if t, ok := g.varTypes[s.Name]; ok && isPrimitiveRef(t) {
			out.WriteString(fmt.Sprintf("(*%s)--", s.Name))
		} else {
			out.WriteString(fmt.Sprintf("%s--", s.Name))
		}
	case *ast.CompoundAssignStmt:
		if t, ok := g.varTypes[s.Name]; ok && isPrimitiveRef(t) {
			out.WriteString(fmt.Sprintf("(*%s) %s= ", s.Name, g.cBinOp(s.Op)))
		} else {
			out.WriteString(fmt.Sprintf("%s %s= ", s.Name, g.cBinOp(s.Op)))
		}
		g.genExpr(out, s.Value)
	case *ast.AssignStmt:
		if isPrimitiveRef(g.varTypes[s.Name]) {
			out.WriteString(fmt.Sprintf("(*%s) = ", s.Name))
		} else {
			out.WriteString(fmt.Sprintf("%s = ", s.Name))
		}
		g.genExpr(out, s.Value)
	}
}

// exprToString renders an expression to a string (for use in foreach).
func (g *Generator) exprToString(expr ast.Expr) string {
	var buf strings.Builder
	g.genExpr(&buf, expr)
	return buf.String()
}

// isNewAlloc returns true if the expression produces a +1 ref (new allocation).
// Variable references are borrowed (not +1).
func (g *Generator) isNewAlloc(expr ast.Expr) bool {
	// json.Value has its own ownership rules: indexing a document mints a
	// reference where indexing an array only borrows one, and its literals build
	// a fresh document.
	if g.typeOfExpr(expr) == ast.TypeJsonValue {
		return g.jsonValueOwned(expr)
	}
	switch expr.(type) {
	case *ast.Ident:
		return false // borrowed reference
	case *ast.FieldAccessExpr:
		// Reading a field borrows it, but naming a method builds a fresh closure
		// over the receiver.
		if fa, isField := expr.(*ast.FieldAccessExpr); isField && fa.IsMethodValue {
			return true
		}
		return false // borrowed from struct field
	case *ast.IndexExpr:
		return false // borrowed from array/map element
	case *ast.SliceExpr:
		return true // slice produces a new array (+1 ref)
	case *ast.StringLit:
		return true // dex_string_from_lit produces +1
	case *ast.CallExpr:
		return true // function calls produce +1
	case *ast.BinaryExpr:
		return true // concat produces +1
	case *ast.ReceiveExpr:
		return true // channel receive produces +1
	case *ast.StringInterpExpr:
		return true // interpolation produces +1
	case *ast.MatchExpr:
		return true // match produces a new value
	case *ast.ArrayLitExpr, *ast.MapLitExpr, *ast.ObjectLitExpr:
		return true // a literal container is freshly allocated (+1)
	default:
		return false // conservative: assume borrowed to avoid premature free
	}
}

// alwaysExitsCodegen mirrors the checker's alwaysExits: does this statement list
// unconditionally leave the enclosing block?
func alwaysExitsCodegen(stmts []ast.Stmt) bool {
	if len(stmts) == 0 {
		return false
	}
	switch last := stmts[len(stmts)-1].(type) {
	case *ast.ReturnStmt, *ast.ThrowStmt, *ast.BreakStmt, *ast.ContinueStmt:
		return true
	case *ast.IfStmt:
		return last.Else != nil && alwaysExitsCodegen(last.Then) && alwaysExitsCodegen(last.Else)
	}
	return false
}

// --- Statement-scoped owned temporaries ---
//
// An expression like s.toUpper() returns a +1 reference. When the value is
// stored, the storer takes it over; when it is only read — printed, or passed to
// something that borrows — nobody owns it and it leaks. These hoist it into a
// temporary released at the end of the statement.

func (g *Generator) beginStmtHoist() {
	g.stmtPrelude = &strings.Builder{}
	g.stmtTemps = nil
}

// Disabled where a hoisted declaration has nowhere to go.
func (g *Generator) hoistingEnabled() bool {
	return g.stmtPrelude != nil && g.stmtHoistOff == 0
}

func (g *Generator) suspendHoisting() func() {
	g.stmtHoistOff++
	return func() { g.stmtHoistOff-- }
}

// For a consumer that only borrows: a minted reference is bound to a
// statement-scoped temporary instead.
func (g *Generator) genBorrowed(out *strings.Builder, expr ast.Expr) {
	t := g.typeOfExpr(expr)
	if !g.hoistingEnabled() {
		g.genExpr(out, expr)
		return
	}
	// Produced here rather than borrowed from somewhere that still owns it. A
	// struct built on the spot owns its heap fields, so it is hoisted too.
	var owned bool
	switch {
	case ast.IsHeapType(t):
		owned = g.isNewAlloc(expr)
	case ast.IsStructType(t) && ast.NeedsRelease(t):
		owned = !g.borrowsHeapValue(expr)
	}
	if !owned {
		g.genExpr(out, expr)
		return
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
	g.stmtPrelude.WriteString(fmt.Sprintf("%s %s = %s; ", g.cType(t), tmp, exprBuf.String()))
	// A struct literal copies borrowed heap fields without retaining them, and
	// the temporary's release would drop a reference it never owned.
	if lit, ok := expr.(*ast.StructLitExpr); ok && ast.IsStructType(t) {
		var retains strings.Builder
		g.emitRetainStructLitFields(&retains, "", tmp, t, lit)
		g.stmtPrelude.WriteString(strings.ReplaceAll(strings.TrimRight(retains.String(), "\n"), "\n", " "))
		if retains.Len() > 0 {
			g.stmtPrelude.WriteString(" ")
		}
	}
	g.stmtTemps = append(g.stmtTemps, scopeVar{name: tmp, typ: t})
	out.WriteString(tmp)
}

// Wraps a generated statement in a block declaring and releasing its hoists.
func (g *Generator) emitWithHoists(out *strings.Builder, prefix, stmt string) {
	if g.stmtPrelude == nil || len(g.stmtTemps) == 0 {
		out.WriteString(prefix + stmt)
		g.stmtPrelude = nil
		g.stmtTemps = nil
		return
	}
	out.WriteString(prefix + "{ " + g.stmtPrelude.String() + strings.TrimRight(stmt, "\n") + " ")
	for i := len(g.stmtTemps) - 1; i >= 0; i-- {
		var rel strings.Builder
		g.emitReleaseVar(&rel, "", g.stmtTemps[i].name, g.stmtTemps[i].typ)
		out.WriteString(strings.ReplaceAll(strings.TrimRight(rel.String(), "\n"), "\n", " ") + " ")
	}
	out.WriteString("}\n")
	g.stmtPrelude = nil
	g.stmtTemps = nil
}

// For a statement that must not be wrapped in a block — a `let`, whose variable
// has to stay in scope. Pair with emitHoistReleases.
func (g *Generator) emitHoistPrelude(out *strings.Builder, prefix string) {
	if g.stmtPrelude == nil || g.stmtPrelude.Len() == 0 {
		return
	}
	out.WriteString(prefix + g.stmtPrelude.String() + "\n")
}

// The prelude is left in place: a return emits its releases while generating its
// body, but its declarations are written ahead of that body.
func (g *Generator) emitHoistReleases(out *strings.Builder, prefix string) {
	for i := len(g.stmtTemps) - 1; i >= 0; i-- {
		// emitReleaseVar rather than a bare release: a hoisted struct is freed by
		// releasing each of its heap fields, not the struct itself.
		g.emitReleaseVar(out, prefix, g.stmtTemps[i].name, g.stmtTemps[i].typ)
	}
	g.stmtTemps = nil
}

// Such a literal needs a retain even in a function with nothing to clean up.
func (g *Generator) structLitBorrowsHeapField(structType ast.Type, lit *ast.StructLitExpr) bool {
	def := ast.GetStructDef(structType)
	if def == nil {
		return false
	}
	fieldTypes := make(map[string]ast.Type, len(def.Fields))
	for _, f := range def.Fields {
		fieldTypes[f.Name] = f.Type
	}
	for i, name := range lit.FieldNames {
		if i >= len(lit.FieldValues) {
			break
		}
		ft, ok := fieldTypes[name]
		if !ok || !ast.IsHeapType(ft) {
			continue
		}
		if g.borrowsHeapValue(lit.FieldValues[i]) {
			return true
		}
	}
	return false
}
