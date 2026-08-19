package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/stdlib"
)

// genExprNoParen generates an expression without wrapping outer parens,
// used for if/while conditions which already provide parens.
func (g *Generator) genExprNoParen(out *strings.Builder, expr ast.Expr) {
	switch e := expr.(type) {
	case *ast.BinaryExpr:
		// Check null comparison in no-paren context
		if g.isNullComparison(e) {
			g.genNullComparison(out, e, false)
			return
		}
		// Check string comparison in no-paren context too
		if g.isStringExpr(e.Left) || g.isStringExpr(e.Right) {
			switch e.Op {
			case ast.BinEq, ast.BinStrictEq:
				out.WriteString("strcmp(")
				g.genStringData(out, e.Left)
				out.WriteString(", ")
				g.genStringData(out, e.Right)
				out.WriteString(") == 0")
				return
			case ast.BinNeq, ast.BinStrictNeq:
				out.WriteString("strcmp(")
				g.genStringData(out, e.Left)
				out.WriteString(", ")
				g.genStringData(out, e.Right)
				out.WriteString(") != 0")
				return
			}
		}
		// Cross-numeric operations in no-paren context
		if e.HasMixedTypes {
			widerType := g.widerNumericType(e.LeftType, e.RightType)
			castType := g.cType(widerType)
			if e.LeftType != widerType {
				out.WriteString(fmt.Sprintf("(%s)", castType))
			}
			g.genExpr(out, e.Left)
			out.WriteString(fmt.Sprintf(" %s ", g.cBinOp(e.Op)))
			if e.Op == ast.BinDiv || e.Op == ast.BinMod {
				out.WriteString(g.nonzeroCheckFunc(widerType) + "(")
			}
			if e.RightType != widerType {
				out.WriteString(fmt.Sprintf("(%s)", castType))
			}
			g.genExpr(out, e.Right)
			if e.Op == ast.BinDiv || e.Op == ast.BinMod {
				out.WriteString(")")
			}
			return
		}
		g.genExpr(out, e.Left)
		out.WriteString(fmt.Sprintf(" %s ", g.cBinOp(e.Op)))
		if e.Op == ast.BinDiv || e.Op == ast.BinMod {
			rightType := g.typeOfExpr(e.Right)
			out.WriteString(g.nonzeroCheckFunc(rightType) + "(")
			g.genExpr(out, e.Right)
			out.WriteString(")")
		} else {
			g.genExpr(out, e.Right)
		}
	default:
		g.genExpr(out, expr)
	}
}

// isNullComparison checks if a binary expression is a null comparison.
func (g *Generator) isNullComparison(e *ast.BinaryExpr) bool {
	if e.Op != ast.BinEq && e.Op != ast.BinNeq {
		return false
	}
	_, leftNull := e.Left.(*ast.NullLit)
	_, rightNull := e.Right.(*ast.NullLit)
	return leftNull || rightNull
}

// genNullComparison emits a null comparison for optional types.
func (g *Generator) genNullComparison(out *strings.Builder, e *ast.BinaryExpr, withParen bool) {
	var nonNullExpr ast.Expr
	if _, ok := e.Left.(*ast.NullLit); ok {
		nonNullExpr = e.Right
	} else {
		nonNullExpr = e.Left
	}

	// Determine the type of the non-null side
	nonNullType := g.typeOfExpr(nonNullExpr)
	isEq := e.Op == ast.BinEq

	if ast.IsOptionalType(nonNullType) {
		inner := ast.OptionalInnerType(nonNullType)
		if ast.IsValueType(inner) {
			// Value-type optional: check .has_value
			if withParen {
				out.WriteString("(")
			}
			if isEq {
				out.WriteString("!")
			}
			g.genExpr(out, nonNullExpr)
			out.WriteString(".has_value")
			if withParen {
				out.WriteString(")")
			}
			return
		}
	}
	// Heap/struct optional: check == NULL or != NULL
	if withParen {
		out.WriteString("(")
	}
	g.genExpr(out, nonNullExpr)
	if isEq {
		out.WriteString(" == NULL")
	} else {
		out.WriteString(" != NULL")
	}
	if withParen {
		out.WriteString(")")
	}
}

// flattenStringConcat flattens a chain of BinaryExpr{BinAdd} nodes on strings
// into a list of leaf operands (left-to-right).
func (g *Generator) flattenStringConcat(expr ast.Expr) []ast.Expr {
	bin, ok := expr.(*ast.BinaryExpr)
	if !ok || bin.Op != ast.BinAdd || !(g.isStringExpr(bin.Left) || g.isStringExpr(bin.Right)) {
		return []ast.Expr{expr}
	}
	var result []ast.Expr
	result = append(result, g.flattenStringConcat(bin.Left)...)
	result = append(result, g.flattenStringConcat(bin.Right)...)
	return result
}

// genStringConcat generates a linearized string concatenation chain.
// It flattens nested BinAdd nodes, emits each operand that produces a +1 allocation
// into a temp variable (registered in pendingReleases), and chains dex_str_concat calls
// while releasing intermediates. Non-string operands are auto-coerced via StringBuilder.
func (g *Generator) genStringConcat(out *strings.Builder, expr *ast.BinaryExpr) {
	operands := g.flattenStringConcat(expr)

	if len(operands) < 2 {
		// Shouldn't happen, but handle defensively
		g.genExpr(out, operands[0])
		return
	}

	// Check if any operand is non-string (needs coercion)
	hasNonString := false
	for _, op := range operands {
		if !g.isStringExpr(op) {
			hasNonString = true
			break
		}
	}

	// For a simple 2-operand concat of two strings, release +1 operand temps inline
	if len(operands) == 2 && !hasNonString {
		leftNew := g.isNewAlloc(operands[0])
		rightNew := g.isNewAlloc(operands[1])
		if !leftNew && !rightNew {
			// Both borrowed — simple emit
			out.WriteString("dex_str_concat(")
			g.genExpr(out, operands[0])
			out.WriteString(", ")
			g.genExpr(out, operands[1])
			out.WriteString(")")
			return
		}
		// At least one operand is +1 — use statement-expression to release it
		out.WriteString("({ ")
		lTmp := g.nextTemp()
		out.WriteString(fmt.Sprintf("DexString* %s = ", lTmp))
		g.genExpr(out, operands[0])
		out.WriteString("; ")
		rTmp := g.nextTemp()
		out.WriteString(fmt.Sprintf("DexString* %s = ", rTmp))
		g.genExpr(out, operands[1])
		out.WriteString("; ")
		resTmp := g.nextTemp()
		out.WriteString(fmt.Sprintf("DexString* %s = dex_str_concat(%s, %s); ", resTmp, lTmp, rTmp))
		if leftNew {
			out.WriteString(fmt.Sprintf("dex_release(%s); ", lTmp))
		}
		if rightNew {
			out.WriteString(fmt.Sprintf("dex_release(%s); ", rTmp))
		}
		out.WriteString(fmt.Sprintf("%s; })", resTmp))
		return
	}

	// For 3+ operands or coercion, use StringBuilder for efficient concatenation.
	g.usesStringBuilder = true
	g.usesString = true
	sbTmp := g.nextTemp()
	resTmp := g.nextTemp()
	out.WriteString(fmt.Sprintf("({ DexStringBuilder* %s = dex_sb_new(); ", sbTmp))

	// Append each operand, handling both string and non-string types
	for _, op := range operands {
		if g.isStringExpr(op) {
			tmpName := g.nextTemp()
			out.WriteString(fmt.Sprintf("DexString* %s = ", tmpName))
			g.genExpr(out, op)
			out.WriteString(fmt.Sprintf("; dex_sb_append_str(%s, %s); ", sbTmp, tmpName))
			if g.isNewAlloc(op) {
				out.WriteString(fmt.Sprintf("dex_release(%s); ", tmpName))
			}
		} else {
			typ := g.typeOfExpr(op)
			tmpName := g.nextTemp()
			out.WriteString(fmt.Sprintf("%s %s = ", g.cType(typ), tmpName))
			g.genExpr(out, op)
			out.WriteString("; ")
			g.genAppendToSB(out, sbTmp, tmpName, typ)
		}
	}

	out.WriteString(fmt.Sprintf("DexString* %s = dex_sb_toString(%s); dex_release(%s); %s; })", resTmp, sbTmp, sbTmp, resTmp))
}

// genAppendToSB appends a C value of the given type to a StringBuilder variable.
// Handles primitives, strings, structs, arrays, and enums.
func (g *Generator) genAppendToSB(out *strings.Builder, sbVar string, cExpr string, typ ast.Type) {
	switch typ {
	case ast.TypeInt:
		out.WriteString(fmt.Sprintf("dex_sb_append_int(%s, %s); ", sbVar, cExpr))
	case ast.TypeLong:
		out.WriteString(fmt.Sprintf("dex_sb_append_long(%s, %s); ", sbVar, cExpr))
	case ast.TypeDouble:
		out.WriteString(fmt.Sprintf("dex_sb_append_double(%s, %s); ", sbVar, cExpr))
	case ast.TypeBool:
		out.WriteString(fmt.Sprintf("dex_sb_append_bool(%s, %s); ", sbVar, cExpr))
	case ast.TypeChar:
		out.WriteString(fmt.Sprintf("dex_sb_append_char(%s, %s); ", sbVar, cExpr))
	case ast.TypeString:
		out.WriteString(fmt.Sprintf("dex_sb_append_str(%s, %s); ", sbVar, cExpr))
	default:
		// Auto-unwrap ref types: dereference and delegate to inner type
		if ast.IsRefType(typ) {
			innerType := ast.RefInnerType(typ)
			derefExpr := fmt.Sprintf("(*%s)", cExpr)
			g.genAppendToSB(out, sbVar, derefExpr, innerType)
			return
		}
		if ast.IsStructType(typ) {
			def := ast.GetStructDef(typ)
			if def == nil {
				out.WriteString(fmt.Sprintf("dex_sb_append_cstr(%s, \"<struct>\"); ", sbVar))
				return
			}
			cnt := g.printCounter
			g.printCounter++
			structVar := fmt.Sprintf("_ps%d", cnt)
			cType := g.cType(typ)
			out.WriteString(fmt.Sprintf("{ %s %s = %s; ", cType, structVar, cExpr))
			out.WriteString(fmt.Sprintf("dex_sb_append_cstr(%s, \"%s{\"); ", sbVar, def.Name))
			for i, f := range def.Fields {
				if i > 0 {
					out.WriteString(fmt.Sprintf("dex_sb_append_cstr(%s, \", \"); ", sbVar))
				}
				fieldExpr := fmt.Sprintf("%s.%s", structVar, f.Name)
				out.WriteString(fmt.Sprintf("dex_sb_append_cstr(%s, \"%s: \"); ", sbVar, f.Name))
				g.genAppendToSB(out, sbVar, fieldExpr, f.Type)
			}
			out.WriteString(fmt.Sprintf("dex_sb_append_cstr(%s, \"}\"); ", sbVar))
			out.WriteString("} ")
		} else if ast.IsArrayType(typ) {
			elemType := ast.ElementType(typ)
			cnt := g.printCounter
			g.printCounter++
			iterVar := fmt.Sprintf("_pi%d", cnt)
			arrVar := fmt.Sprintf("_pa%d", cnt)
			if ast.IsStructArrayType(typ) {
				out.WriteString(fmt.Sprintf("{ DexArrayStruct* %s = (DexArrayStruct*)%s; ", arrVar, cExpr))
			} else {
				out.WriteString(fmt.Sprintf("{ %s %s = %s; ", g.cType(typ), arrVar, cExpr))
			}
			out.WriteString(fmt.Sprintf("dex_sb_append_cstr(%s, \"[\"); ", sbVar))
			out.WriteString(fmt.Sprintf("for (int %s = 0; %s < %s->len; %s++) { ", iterVar, iterVar, arrVar, iterVar))
			out.WriteString(fmt.Sprintf("if (%s > 0) dex_sb_append_cstr(%s, \", \"); ", iterVar, sbVar))
			elemExpr := fmt.Sprintf("%s->data[%s]", arrVar, iterVar)
			if ast.IsStructArrayType(typ) {
				elemCType := g.cType(elemType)
				elemExpr = fmt.Sprintf("*(%s*)dex_array_struct_get(%s, %s)", elemCType, arrVar, iterVar)
			}
			g.genAppendToSB(out, sbVar, elemExpr, elemType)
			out.WriteString("} ")
			out.WriteString(fmt.Sprintf("dex_sb_append_cstr(%s, \"]\"); ", sbVar))
			out.WriteString("} ")
		} else if ast.IsEnumType(typ) {
			out.WriteString(fmt.Sprintf("dex_sb_append_int(%s, %s); ", sbVar, cExpr))
		} else {
			out.WriteString(fmt.Sprintf("dex_sb_append_cstr(%s, \"<unknown>\"); ", sbVar))
		}
	}
}

// genExprAsString emits code producing a DexString* from any expression,
// using a temporary StringBuilder for non-string types.
func (g *Generator) genExprAsString(out *strings.Builder, expr ast.Expr) {
	typ := g.typeOfExpr(expr)
	if typ == ast.TypeString {
		g.genExpr(out, expr)
		return
	}
	g.usesStringBuilder = true
	g.usesString = true
	sbTmp := g.nextTemp()
	valTmp := g.nextTemp()
	resTmp := g.nextTemp()
	out.WriteString(fmt.Sprintf("({ DexStringBuilder* %s = dex_sb_new(); ", sbTmp))
	out.WriteString(fmt.Sprintf("%s %s = ", g.cType(typ), valTmp))
	g.genExpr(out, expr)
	out.WriteString("; ")
	g.genAppendToSB(out, sbTmp, valTmp, typ)
	out.WriteString(fmt.Sprintf("DexString* %s = dex_sb_toString(%s); dex_release(%s); %s; })", resTmp, sbTmp, sbTmp, resTmp))
}

// isStringExpr checks if an expression is known to produce a string type.
func (g *Generator) isStringExpr(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.StringLit:
		return true
	case *ast.CallExpr:
		// Polymorphic return type: uses ResolvedType if set
		if e.ResolvedType != 0 {
			return e.ResolvedType == ast.TypeString
		}
		// Map method calls: get() on a map with string values
		if e.Module != "" {
			if mapType, ok := g.mapVars[e.Module]; ok {
				if e.Name == "get" {
					return ast.MapValueType(mapType) == ast.TypeString
				}
				return false
			}
			// Field chain map access (e.g., self.myMap.get("key"))
			if strings.Contains(e.Module, ".") {
				chainType := g.resolveFieldChainType(e.Module)
				if ast.IsMapType(chainType) {
					if e.Name == "get" {
						return ast.MapValueType(chainType) == ast.TypeString
					}
					return false
				}
			}
		}
		// StringBuilder methods that return strings
		if e.Module != "" && g.sbVars[e.Module] {
			return e.Name == "toString"
		}
		// String methods that return strings
		if e.Module != "" && g.strVars[e.Module] {
			switch e.Name {
			case "toLower", "toUpper", "trim", "substring", "replace":
				return true
			}
			return false
		}
		// Check stdlib functions
		if e.Module != "" {
			funcDef, ok := stdlib.LookupFunc(e.Module, e.Name)
			if ok {
				return funcDef.ReturnType == ast.TypeString
			}
			return false
		}
		// User-defined functions
		if fn, ok := g.funcs[e.Name]; ok {
			return fn.ReturnType == ast.TypeString
		}
	case *ast.Ident:
		return g.strVars[e.Name]
	case *ast.IndexExpr:
		// Check if indexing a map with string values
		if ident, ok := e.Array.(*ast.Ident); ok {
			if mapType, ok := g.mapVars[ident.Name]; ok {
				return ast.MapValueType(mapType) == ast.TypeString
			}
			if arrType, ok := g.arrVars[ident.Name]; ok {
				return arrType == ast.TypeArrayString
			}
		}
		return false
	case *ast.BinaryExpr:
		if e.Op == ast.BinAdd {
			return g.isStringExpr(e.Left) || g.isStringExpr(e.Right)
		}
	case *ast.FieldAccessExpr:
		return g.typeOfExpr(e) == ast.TypeString
	}
	return false
}

// genPrintArray generates C code to print an array as [elem, elem, ...].
func (g *Generator) genPrintArray(out *strings.Builder, expr ast.Expr, arrType ast.Type, newline bool) {
	elemType := ast.ElementType(arrType)
	cnt := g.printCounter
	g.printCounter++
	iterVar := fmt.Sprintf("_pi%d", cnt)

	out.WriteString("do { ")

	// Store the array pointer in a temp variable
	arrVar := fmt.Sprintf("_pa%d", cnt)
	if ast.IsStructArrayType(arrType) {
		out.WriteString(fmt.Sprintf("DexArrayStruct* %s = (DexArrayStruct*)", arrVar))
	} else {
		out.WriteString(fmt.Sprintf("%s %s = ", g.cType(arrType), arrVar))
	}
	g.genExpr(out, expr)
	out.WriteString("; ")

	out.WriteString("printf(\"[\"); ")
	out.WriteString(fmt.Sprintf("for (int %s = 0; %s < %s->len; %s++) { ", iterVar, iterVar, arrVar, iterVar))
	out.WriteString(fmt.Sprintf("if (%s > 0) printf(\", \"); ", iterVar))

	// Generate print for each element
	elemExpr := fmt.Sprintf("%s->data[%s]", arrVar, iterVar)
	if ast.IsStructArrayType(arrType) {
		elemCType := g.cType(elemType)
		elemExpr = fmt.Sprintf("*(%s*)dex_array_struct_get(%s, %s)", elemCType, arrVar, iterVar)
	}
	g.genPrintElem(out, elemExpr, elemType)
	out.WriteString("; ")

	out.WriteString("} ")
	if newline {
		out.WriteString("printf(\"]\\n\"); ")
	} else {
		out.WriteString("printf(\"]\"); ")
	}
	out.WriteString("} while(0)")
}

// genPrintElem generates C code to print a single element given as a C expression string.
func (g *Generator) genPrintElem(out *strings.Builder, cExpr string, typ ast.Type) {
	if ast.IsArrayType(typ) {
		// Nested array: recurse
		elemType := ast.ElementType(typ)
		cnt := g.printCounter
		g.printCounter++
		iterVar := fmt.Sprintf("_pi%d", cnt)
		arrVar := fmt.Sprintf("_pa%d", cnt)

		if ast.IsStructArrayType(typ) {
			out.WriteString(fmt.Sprintf("{ DexArrayStruct* %s = (DexArrayStruct*)%s; ", arrVar, cExpr))
		} else {
			out.WriteString(fmt.Sprintf("{ %s %s = %s; ", g.cType(typ), arrVar, cExpr))
		}
		out.WriteString("printf(\"[\"); ")
		out.WriteString(fmt.Sprintf("for (int %s = 0; %s < %s->len; %s++) { ", iterVar, iterVar, arrVar, iterVar))
		out.WriteString(fmt.Sprintf("if (%s > 0) printf(\", \"); ", iterVar))

		innerExpr := fmt.Sprintf("%s->data[%s]", arrVar, iterVar)
		if ast.IsStructArrayType(typ) {
			innerCType := g.cType(elemType)
			innerExpr = fmt.Sprintf("*(%s*)dex_array_struct_get(%s, %s)", innerCType, arrVar, iterVar)
		}
		g.genPrintElem(out, innerExpr, elemType)
		out.WriteString("; ")

		out.WriteString("} printf(\"]\"); }")
		return
	}
	if ast.IsStructType(typ) {
		// Nested struct: recurse
		def := ast.GetStructDef(typ)
		if def == nil {
			out.WriteString(fmt.Sprintf("printf(\"%%d\", %s)", cExpr))
			return
		}
		cnt := g.printCounter
		g.printCounter++
		structVar := fmt.Sprintf("_ps%d", cnt)
		cType := g.cType(typ)
		out.WriteString(fmt.Sprintf("{ %s %s = %s; ", cType, structVar, cExpr))
		out.WriteString(fmt.Sprintf("printf(\"%s{\"); ", def.Name))
		for i, f := range def.Fields {
			if i > 0 {
				out.WriteString("printf(\", \"); ")
			}
			fieldExpr := fmt.Sprintf("%s.%s", structVar, f.Name)
			out.WriteString(fmt.Sprintf("printf(\"%s: \"); ", f.Name))
			g.genPrintElem(out, fieldExpr, f.Type)
			out.WriteString("; ")
		}
		out.WriteString("printf(\"}\"); }")
		return
	}
	// Primitive types
	switch typ {
	case ast.TypeChar:
		out.WriteString(fmt.Sprintf("printf(\"%%c\", %s)", cExpr))
	case ast.TypeInt:
		out.WriteString(fmt.Sprintf("printf(\"%%d\", %s)", cExpr))
	case ast.TypeLong:
		out.WriteString(fmt.Sprintf("printf(\"%%ld\", %s)", cExpr))
	case ast.TypeDouble:
		out.WriteString(fmt.Sprintf("printf(\"%%f\", %s)", cExpr))
	case ast.TypeString:
		out.WriteString(fmt.Sprintf("printf(\"%%s\", (%s) ? (%s)->data : \"\")", cExpr, cExpr))
	case ast.TypeBool:
		out.WriteString(fmt.Sprintf("printf(\"%%s\", (%s) ? \"true\" : \"false\")", cExpr))
	default:
		// enums and other integer-like types
		out.WriteString(fmt.Sprintf("printf(\"%%d\", %s)", cExpr))
	}
}

// genPrintStruct generates C code to print a struct as Name{field: value, ...}.
func (g *Generator) genPrintStruct(out *strings.Builder, expr ast.Expr, structType ast.Type, newline bool) {
	def := ast.GetStructDef(structType)
	if def == nil {
		out.WriteString("printf(\"<unknown struct>\")")
		return
	}
	cnt := g.printCounter
	g.printCounter++
	structVar := fmt.Sprintf("_ps%d", cnt)
	cType := g.cType(structType)

	out.WriteString("do { ")
	// If expr is a ref type (&Struct), dereference the pointer
	exprType := g.typeOfExpr(expr)
	if ast.IsRefType(exprType) {
		out.WriteString(fmt.Sprintf("%s %s = *(", cType, structVar))
		g.genExpr(out, expr)
		out.WriteString("); ")
	} else {
		out.WriteString(fmt.Sprintf("%s %s = ", cType, structVar))
		g.genExpr(out, expr)
		out.WriteString("; ")
	}

	out.WriteString(fmt.Sprintf("printf(\"%s{\"); ", def.Name))
	for i, f := range def.Fields {
		if i > 0 {
			out.WriteString("printf(\", \"); ")
		}
		fieldExpr := fmt.Sprintf("%s.%s", structVar, f.Name)
		out.WriteString(fmt.Sprintf("printf(\"%s: \"); ", f.Name))
		g.genPrintElem(out, fieldExpr, f.Type)
		out.WriteString("; ")
	}
	if newline {
		out.WriteString("printf(\"}\\n\"); ")
	} else {
		out.WriteString("printf(\"}\"); ")
	}
	out.WriteString("} while(0)")
}

// genZeroValue emits the C zero value for a DexLang type.
// int/long/char → 0, double → 0.0, bool → 0, string → dex_string_from_lit(""),
// structs → recursive zero-init, arrays/other pointers → NULL.
func (g *Generator) genZeroValue(out *strings.Builder, typ ast.Type) {
	switch typ {
	case ast.TypeInt, ast.TypeChar:
		out.WriteString("0")
	case ast.TypeLong:
		out.WriteString("0L")
	case ast.TypeDouble:
		out.WriteString("0.0")
	case ast.TypeBool:
		out.WriteString("0")
	case ast.TypeString:
		out.WriteString("dex_string_from_lit(\"\")")
	default:
		if ast.IsStructType(typ) {
			def := ast.GetStructDef(typ)
			if def == nil {
				out.WriteString("((" + g.cType(typ) + "){ 0 })")
				return
			}
			cName := "Dex_" + def.Name
			out.WriteString(fmt.Sprintf("(%s){ ", cName))
			for i, f := range def.Fields {
				if i > 0 {
					out.WriteString(", ")
				}
				out.WriteString(fmt.Sprintf(".%s = ", f.Name))
				g.genZeroValue(out, f.Type)
			}
			out.WriteString(" }")
			return
		}
		// Arrays, optionals, and other pointer types: NULL
		out.WriteString("NULL")
	}
}
