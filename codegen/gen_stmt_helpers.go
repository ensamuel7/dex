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
	switch expr.(type) {
	case *ast.Ident:
		return false // borrowed reference
	case *ast.FieldAccessExpr:
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
	default:
		return false // conservative: assume borrowed to avoid premature free
	}
}
