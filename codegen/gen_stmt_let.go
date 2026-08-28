package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

// genLetStmt emits a variable declaration. Most of the length is the special
// cases: literals that take their type from the declaration, and the several
// container kinds that are built rather than assigned.
func (g *Generator) genLetStmt(out *strings.Builder, s *ast.LetStmt, prefix string, indent int) {
	// Expand multi-declaration into individual let statements
	if len(s.Names) > 0 {
		for _, name := range s.Names {
			individual := &ast.LetStmt{
				Pos: s.Pos, Name: name, Type: s.Type,
				Value: s.Value, IsConst: s.IsConst, Annotations: s.Annotations,
			}
			g.genStmt(out, individual, indent)
		}
		return
	}
	g.varTypes[s.Name] = s.Type
	if len(s.Annotations) > 0 {
		g.varAnnotations[s.Name] = s.Annotations
	}
	if s.Type == ast.TypeString {
		g.strVars[s.Name] = true
	}
	if ast.IsArrayType(s.Type) {
		g.arrVars[s.Name] = s.Type
	}
	if ast.IsStructType(s.Type) {
		g.structVars[s.Name] = s.Type
	}
	// An optional container resolves to its inner type once narrowed, so
	// register it under the inner type for method dispatch.
	if ast.IsOptionalType(s.Type) {
		inner := ast.OptionalInnerType(s.Type)
		if ast.IsArrayType(inner) {
			g.arrVars[s.Name] = inner
		}
		if ast.IsStructType(inner) {
			g.structVars[s.Name] = inner
		}
	}
	if ast.IsMapType(s.Type) {
		g.mapVars[s.Name] = s.Type
	}
	if s.Type == ast.TypeStringBuilder {
		g.sbVars[s.Name] = true
	}
	// json.Value target: every literal form on the right builds a JSON
	// document, so it is handled before the map/array literal cases below.
	if s.Type == ast.TypeJsonValue {
		out.WriteString(fmt.Sprintf("%sDexJsonValue* %s = ", prefix, s.Name))
		g.genJsonValue(out, s.Value)
		out.WriteString(";\n")
		g.registerScopeVar(s.Name, s.Type)
		return
	}
	// Special case for map literal: let m: map[K,V] = {}
	if _, ok := s.Value.(*ast.MapLitExpr); ok {
		suffix := g.mapSuffix(s.Type)
		out.WriteString(fmt.Sprintf("%s%s %s = dex_map_%s_new();\n", prefix, g.cType(s.Type), s.Name, suffix))
		g.registerScopeVar(s.Name, s.Type)
		return
	}
	// Special case for channel/task types: let ch = channel(int), let t = spawn { ... }
	if ast.IsChanType(s.Type) || ast.IsTaskType(s.Type) {
		// Already handled by varTypes above; ensure cType works
	}
	// Special case for receive expression: let val = receive(task)
	if recvExpr, ok := s.Value.(*ast.ReceiveExpr); ok {
		ctyp := g.cType(s.Type)
		out.WriteString(fmt.Sprintf("%s%s %s; dex_chan_recv(", prefix, ctyp, s.Name))
		g.genExpr(out, recvExpr.Source)
		out.WriteString(fmt.Sprintf(", &%s);\n", s.Name))
		g.registerScopeVar(s.Name, s.Type)
		return
	}
	// Special case for array literal value
	if arrLit, ok := s.Value.(*ast.ArrayLitExpr); ok {
		if ast.IsStructArrayType(s.Type) {
			// Struct array: use generic DexArrayStruct
			elemType := ast.ElementType(s.Type)
			elemCType := g.cType(elemType)
			cleanupFn := g.structArrayCleanupFunc(elemType)
			out.WriteString(fmt.Sprintf("%sDexArrayStruct* %s = dex_array_struct_new(sizeof(%s), %s);\n", prefix, s.Name, elemCType, cleanupFn))
			if len(arrLit.Elems) > 0 {
				for _, elem := range arrLit.Elems {
					out.WriteString(fmt.Sprintf("%s{ %s _tmp_elem = ", prefix, elemCType))
					g.genExpr(out, elem)
					out.WriteString(fmt.Sprintf("; dex_array_struct_push(%s, &_tmp_elem); }\n", s.Name))
				}
			}
			g.registerScopeVar(s.Name, s.Type)
			return
		}
		cNewFn := g.arrayNewFunc(s.Type)
		out.WriteString(fmt.Sprintf("%s%s %s = %s();\n", prefix, g.cType(s.Type), s.Name, cNewFn))
		if len(arrLit.Elems) > 0 {
			// Inline initialize data
			for i, elem := range arrLit.Elems {
				if s.Type == ast.TypeArrayString {
					// For string arrays, we need to retain the string element
					out.WriteString(fmt.Sprintf("%s%s->data[%d] = ", prefix, s.Name, i))
					g.genExpr(out, elem)
					out.WriteString(";\n")
					// String literals from genExpr produce +1 refs via dex_string_from_lit
					// so no extra retain needed
				} else {
					out.WriteString(fmt.Sprintf("%s%s->data[%d] = ", prefix, s.Name, i))
					g.genExpr(out, elem)
					out.WriteString(";\n")
				}
			}
			out.WriteString(fmt.Sprintf("%s%s->len = %d;\n", prefix, s.Name, len(arrLit.Elems)))
		}
		g.registerScopeVar(s.Name, s.Type)
		return
	}
	// Special case for string declarations
	if s.Type == ast.TypeString {
		if ast.HasAnnotation(s.Annotations, ast.AnnotRegion) {
			// #[region] string — allocate from arena
			if strLit, ok := s.Value.(*ast.StringLit); ok {
				out.WriteString(fmt.Sprintf("%sDexString* %s = dex_arena_string(_arena, %q, %d);\n", prefix, s.Name, strLit.Value, len(strLit.Value)))
			} else {
				out.WriteString(fmt.Sprintf("%sDexString* %s = ", prefix, s.Name))
				g.genExpr(out, s.Value)
				out.WriteString(";\n")
			}
			g.registerScopeVar(s.Name, s.Type)
			return
		}
		if strLit, ok := s.Value.(*ast.StringLit); ok {
			out.WriteString(fmt.Sprintf("%sDexString* %s = dex_string_from_lit(%q);\n", prefix, s.Name, strLit.Value))
		} else {
			out.WriteString(fmt.Sprintf("%sDexString* %s = ", prefix, s.Name))
			g.genExpr(out, s.Value)
			out.WriteString(";\n")
			// If RHS is a borrowed reference (variable, array index, or field access), retain it
			// But not for #[owned] — ownership transfer, no retain
			isBorrowed := false
			if _, ok := s.Value.(*ast.Ident); ok {
				isBorrowed = true
			}
			if _, ok := s.Value.(*ast.IndexExpr); ok {
				isBorrowed = true
			}
			if _, ok := s.Value.(*ast.FieldAccessExpr); ok {
				isBorrowed = true
			}
			if isBorrowed && !ast.HasAnnotation(s.Annotations, ast.AnnotOwned) {
				out.WriteString(fmt.Sprintf("%sdex_retain(%s);\n", prefix, s.Name))
			}
		}
		g.registerScopeVar(s.Name, s.Type)
		// Emit debug cycle tracking if annotated
		if ast.HasAnnotation(s.Annotations, ast.AnnotDebugCycles) {
			out.WriteString(fmt.Sprintf("%sdex_cycle_track(%s, %q);\n", prefix, s.Name, s.Name))
		}
		return
	}
	// Special case for optional type declarations
	if ast.IsOptionalType(s.Type) {
		g.usesOptional = true
		inner := ast.OptionalInnerType(s.Type)
		_, isNull := s.Value.(*ast.NullLit)
		// Check if the RHS already produces the optional type (e.g., calling a function that returns T?)
		valType := g.typeOfExpr(s.Value)
		valIsOptional := valType == s.Type
		if ast.IsValueType(inner) {
			ctyp := g.cType(s.Type)
			if isNull {
				out.WriteString(fmt.Sprintf("%s%s %s = {0};\n", prefix, ctyp, s.Name))
			} else if valIsOptional {
				out.WriteString(fmt.Sprintf("%s%s %s = ", prefix, ctyp, s.Name))
				g.genExpr(out, s.Value)
				out.WriteString(";\n")
			} else {
				out.WriteString(fmt.Sprintf("%s%s %s = {1, ", prefix, ctyp, s.Name))
				g.genExpr(out, s.Value)
				out.WriteString("};\n")
			}
		} else if ast.IsStructType(inner) {
			ctyp := g.cType(s.Type) // Dex_Foo*
			if isNull {
				out.WriteString(fmt.Sprintf("%s%s %s = NULL;\n", prefix, ctyp, s.Name))
			} else if valIsOptional {
				// RHS already produces the optional pointer — take it as-is
				out.WriteString(fmt.Sprintf("%s%s %s = ", prefix, ctyp, s.Name))
				g.genExpr(out, s.Value)
				out.WriteString(";\n")
			} else {
				innerCType := "Dex_" + ast.StructName(inner)
				out.WriteString(fmt.Sprintf("%s%s %s = (%s*)malloc(sizeof(%s));\n", prefix, ctyp, s.Name, innerCType, innerCType))
				out.WriteString(fmt.Sprintf("%s*%s = ", prefix, s.Name))
				g.genExpr(out, s.Value)
				out.WriteString(";\n")
			}
		} else {
			// Heap type (string, array, etc.) — same pointer, NULL = absent
			ctyp := g.cType(s.Type)
			if isNull {
				out.WriteString(fmt.Sprintf("%s%s %s = NULL;\n", prefix, ctyp, s.Name))
			} else if inner == ast.TypeString {
				// Handle string optional like regular string init
				if strLit, ok := s.Value.(*ast.StringLit); ok {
					out.WriteString(fmt.Sprintf("%s%s %s = dex_string_from_lit(%q);\n", prefix, ctyp, s.Name, strLit.Value))
				} else {
					out.WriteString(fmt.Sprintf("%s%s %s = ", prefix, ctyp, s.Name))
					g.genExpr(out, s.Value)
					out.WriteString(";\n")
					if _, ok := s.Value.(*ast.Ident); ok {
						out.WriteString(fmt.Sprintf("%sdex_retain(%s);\n", prefix, s.Name))
					}
				}
			} else {
				out.WriteString(fmt.Sprintf("%s%s %s = ", prefix, ctyp, s.Name))
				g.genExpr(out, s.Value)
				out.WriteString(";\n")
			}
		}
		g.registerScopeVar(s.Name, s.Type)
		return
	}
	// Special case for ref type declarations
	if ast.IsRefType(s.Type) {
		ctyp := g.cType(s.Type)
		out.WriteString(fmt.Sprintf("%s%s %s = &", prefix, ctyp, s.Name))
		g.genExpr(out, s.Value)
		out.WriteString(";\n")
		g.varTypes[s.Name] = s.Type
		return
	}
	// Special case for weak reference declarations
	if ast.IsWeakType(s.Type) {
		out.WriteString(fmt.Sprintf("%sDexWeakRef* %s = dex_weak_new(", prefix, s.Name))
		g.genExpr(out, s.Value)
		out.WriteString(");\n")
		g.registerScopeVar(s.Name, s.Type)
		return
	}
	constPrefix := ""
	if s.IsConst {
		constPrefix = "const "
	}
	out.WriteString(fmt.Sprintf("%s%s%s %s = ", prefix, constPrefix, g.cType(s.Type), s.Name))
	g.genExpr(out, s.Value)
	out.WriteString(";\n")
	// A struct literal copies borrowed heap fields without owning them.
	if lit, ok := s.Value.(*ast.StructLitExpr); ok && ast.IsStructType(s.Type) {
		g.emitRetainStructLitFields(out, prefix, s.Name, s.Type, lit)
	}
	g.registerScopeVar(s.Name, s.Type)
	// Emit debug cycle tracking if annotated
	if ast.HasAnnotation(s.Annotations, ast.AnnotDebugCycles) && ast.IsHeapType(s.Type) {
		out.WriteString(fmt.Sprintf("%sdex_cycle_track(%s, %q);\n", prefix, s.Name, s.Name))
	}

}
