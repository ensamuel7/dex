package checker

import (
	"fmt"

	"github.com/ensamuel7/dex/ast"
)

func (c *Checker) checkStmt(stmt ast.Stmt, returnType ast.Type) error {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		// Expand multi-declaration into individual let statements
		if len(s.Names) > 0 {
			for _, name := range s.Names {
				individual := &ast.LetStmt{
					Pos: s.Pos, Name: name, Type: s.Type,
					Value: s.Value, IsConst: s.IsConst, Annotations: s.Annotations,
				}
				if err := c.checkStmt(individual, returnType); err != nil {
					return err
				}
			}
			return nil
		}
		// Check if RHS is an #[owned] variable — aliasing is not allowed
		if ident, ok := s.Value.(*ast.Ident); ok {
			rhsAnnots := c.resolveAnnotations(ident.Name)
			if ast.HasAnnotation(rhsAnnots, ast.AnnotOwned) {
				return c.errAt(s.Pos, "cannot alias #[owned] variable '%s'", ident.Name)
			}
		}
		// Pre-annotate db.col() with the expected return type
		if call, ok := s.Value.(*ast.CallExpr); ok && call.Module == "db" && call.Name == "col" {
			if s.Type == ast.TypeInferred {
				return c.errAt(s.Pos, "db.col() requires an explicit type annotation (e.g., let x: int = db.col(...))")
			}
			call.ResolvedType = s.Type
		}
		// Pre-annotate json.decode() with the expected struct type
		if call, ok := s.Value.(*ast.CallExpr); ok && call.Module == "json" && call.Name == "decode" {
			if s.Type == ast.TypeInferred {
				return c.errAt(s.Pos, "json.decode() requires an explicit type annotation (e.g., let x: MyStruct = json.decode(...))")
			}
			call.ResolvedType = s.Type
		}

		// Handle type inference
		if s.Type == ast.TypeInferred {
			// Cannot infer type from null literal
			if _, ok := s.Value.(*ast.NullLit); ok {
				return c.errAt(s.Pos, "cannot infer type of null; use explicit type annotation (e.g., let x: int? = null)")
			}
			// Cannot infer type from empty array literal
			if arrLit, ok := s.Value.(*ast.ArrayLitExpr); ok && len(arrLit.Elems) == 0 {
				return c.errAt(s.Pos, "cannot infer type of empty array literal; use explicit type annotation")
			}
			exprType, err := c.checkExpr(s.Value)
			if err != nil {
				return err
			}
			s.Type = exprType
			if err := validateAnnotations(s.Annotations, s.Type, s.Name); err != nil {
				return c.errAt(s.Pos, "%s", err)
			}
			c.define(s.Name, s.Type)
			c.defineAnnotations(s.Name, s.Annotations)
			if s.IsConst {
				c.defineConst(s.Name)
			}
			return nil
		}

		// Validate annotations on the declared type
		if err := validateAnnotations(s.Annotations, s.Type, s.Name); err != nil {
			return c.errAt(s.Pos, "%s", err)
		}

		// Handle empty map literal: infer key/value types from declared type
		if mapLit, ok := s.Value.(*ast.MapLitExpr); ok {
			if !ast.IsMapType(s.Type) {
				return c.errAt(s.Pos, "cannot assign empty map literal to non-map type %s", typeName(s.Type))
			}
			mapLit.MapType = s.Type
			c.define(s.Name, s.Type)
			c.defineAnnotations(s.Name, s.Annotations)
			if s.IsConst {
				c.defineConst(s.Name)
			}
			return nil
		}

		// Handle empty array literal: infer element type from declared type
		if arrLit, ok := s.Value.(*ast.ArrayLitExpr); ok && len(arrLit.Elems) == 0 {
			if !ast.IsArrayType(s.Type) {
				return c.errAt(s.Pos, "cannot assign empty array literal to non-array type %s", typeName(s.Type))
			}
			arrLit.ElemType = ast.ElementType(s.Type)
			c.define(s.Name, s.Type)
			c.defineAnnotations(s.Name, s.Annotations)
			if s.IsConst {
				c.defineConst(s.Name)
			}
			return nil
		}
		// Handle array literal with implicit element widening (e.g. [1,2,3] -> long[])
		if arrLit, ok := s.Value.(*ast.ArrayLitExpr); ok && len(arrLit.Elems) > 0 && ast.IsArrayType(s.Type) {
			targetElem := ast.ElementType(s.Type)
			allCompatible := true
			for _, elem := range arrLit.Elems {
				elemType, err := c.checkExpr(elem)
				if err != nil {
					return err
				}
				if !canAssign(targetElem, elemType, elem) {
					allCompatible = false
					break
				}
			}
			if allCompatible {
				arrLit.ElemType = targetElem
				c.define(s.Name, s.Type)
				c.defineAnnotations(s.Name, s.Annotations)
				if s.IsConst {
					c.defineConst(s.Name)
				}
				return nil
			}
		}
		exprType, err := c.checkExpr(s.Value)
		if err != nil {
			return err
		}
		if !canAssign(s.Type, exprType, s.Value) {
			return c.errAt(s.Pos, "type mismatch in let: expected %s, got %s", typeName(s.Type), typeName(exprType))
		}
		c.define(s.Name, s.Type)
		c.defineAnnotations(s.Name, s.Annotations)
		if s.IsConst {
			c.defineConst(s.Name)
		}
		return nil

	case *ast.ReturnStmt:
		// Bare return (no value) — only valid in void functions
		if s.Value == nil {
			if returnType != ast.TypeVoid {
				return c.errAt(s.Pos, "return statement must have a value in non-void function")
			}
			return nil
		}
		// Check if returning a #[noEscape] or #[region] variable
		if ident, ok := s.Value.(*ast.Ident); ok {
			annots := c.resolveAnnotations(ident.Name)
			if ast.HasAnnotation(annots, ast.AnnotNoEscape) {
				return c.errAt(s.Pos, "cannot return #[noEscape] variable '%s'", ident.Name)
			}
			if ast.HasAnnotation(annots, ast.AnnotRegion) {
				return c.errAt(s.Pos, "cannot return #[region] variable '%s'", ident.Name)
			}
		}
		// Pre-annotate db.col() with the expected return type
		if call, ok := s.Value.(*ast.CallExpr); ok && call.Module == "db" && call.Name == "col" {
			call.ResolvedType = returnType
		}
		// Pre-annotate json.decode() with the expected return type
		if call, ok := s.Value.(*ast.CallExpr); ok && call.Module == "json" && call.Name == "decode" {
			call.ResolvedType = returnType
		}
		exprType, err := c.checkExpr(s.Value)
		if err != nil {
			return err
		}
		if exprType != returnType && !canAssign(returnType, exprType, s.Value) {
			return c.errAt(s.Pos, "return type mismatch: expected %s, got %s", typeName(returnType), typeName(exprType))
		}
		return nil

	case *ast.ExprStmt:
		_, err := c.checkExpr(s.Expr)
		return err

	case *ast.IfStmt:
		condType, err := c.checkExpr(s.Cond)
		if err != nil {
			return err
		}
		if condType != ast.TypeBool {
			return c.errAt(s.Pos, "if condition must be bool, got %s", typeName(condType))
		}
		// Detect null check pattern for type narrowing
		varName, isNotNull := extractNullCheck(s.Cond)
		c.pushScope()
		if varName != "" && isNotNull {
			// x != null in then-branch: narrow to inner type
			if varType, ok := c.resolve(varName); ok && ast.IsOptionalType(varType) {
				c.define(varName, ast.OptionalInnerType(varType))
			}
		}
		for _, stmt := range s.Then {
			if err := c.checkStmt(stmt, returnType); err != nil {
				return err
			}
		}
		c.popScope()
		if s.Else != nil {
			c.pushScope()
			if varName != "" && !isNotNull {
				// x == null in else-branch: narrow to inner type in else
				if varType, ok := c.resolve(varName); ok && ast.IsOptionalType(varType) {
					c.define(varName, ast.OptionalInnerType(varType))
				}
			}
			for _, stmt := range s.Else {
				if err := c.checkStmt(stmt, returnType); err != nil {
					return err
				}
			}
			c.popScope()
		}
		return nil

	case *ast.WhileStmt:
		condType, err := c.checkExpr(s.Cond)
		if err != nil {
			return err
		}
		if condType != ast.TypeBool {
			return c.errAt(s.Pos, "while condition must be bool, got %s", typeName(condType))
		}
		c.pushScope()
		c.loopDepth++
		for _, stmt := range s.Body {
			if err := c.checkStmt(stmt, returnType); err != nil {
				return err
			}
		}
		c.loopDepth--
		c.popScope()
		return nil

	case *ast.ForStmt:
		c.pushScope()
		if err := c.checkStmt(s.Init, returnType); err != nil {
			return err
		}
		condType, err := c.checkExpr(s.Cond)
		if err != nil {
			return err
		}
		if condType != ast.TypeBool {
			return c.errAt(s.Pos, "for condition must be bool, got %s", typeName(condType))
		}
		if err := c.checkStmt(s.Post, returnType); err != nil {
			return err
		}
		c.loopDepth++
		for _, stmt := range s.Body {
			if err := c.checkStmt(stmt, returnType); err != nil {
				return err
			}
		}
		c.loopDepth--
		c.popScope()
		return nil

	case *ast.ForeachStmt:
		iterType, err := c.checkExpr(s.Iterable)
		if err != nil {
			return err
		}
		if !ast.IsArrayType(iterType) {
			return c.errAt(s.Pos, "foreach requires an array type, got %s", typeName(iterType))
		}
		elemType := ast.ElementType(iterType)
		c.pushScope()
		if s.IndexVar != "" {
			c.define(s.IndexVar, ast.TypeInt)
		}
		c.define(s.ValueVar, elemType)
		c.loopDepth++
		for _, stmt := range s.Body {
			if err := c.checkStmt(stmt, returnType); err != nil {
				return err
			}
		}
		c.loopDepth--
		c.popScope()
		return nil

	case *ast.BreakStmt:
		if c.loopDepth == 0 {
			return c.errAt(s.Pos, "'break' outside of loop")
		}
		return nil

	case *ast.ContinueStmt:
		if c.loopDepth == 0 {
			return c.errAt(s.Pos, "'continue' outside of loop")
		}
		return nil

	case *ast.IncrementStmt:
		varType, ok := c.resolve(s.Name)
		if !ok {
			return c.errAt(s.Pos, "undefined variable '%s'", s.Name)
		}
		if c.isConst(s.Name) {
			return c.errAt(s.Pos, "cannot modify const variable '%s'", s.Name)
		}
		// Unwrap primitive ref for numeric check
		if ast.IsRefType(varType) && ast.IsValueType(ast.RefInnerType(varType)) {
			varType = ast.RefInnerType(varType)
		}
		if !isNumericType(varType) {
			return c.errAt(s.Pos, "'++' requires numeric variable, got %s", typeName(varType))
		}
		return nil

	case *ast.DecrementStmt:
		varType, ok := c.resolve(s.Name)
		if !ok {
			return c.errAt(s.Pos, "undefined variable '%s'", s.Name)
		}
		if c.isConst(s.Name) {
			return c.errAt(s.Pos, "cannot modify const variable '%s'", s.Name)
		}
		// Unwrap primitive ref for numeric check
		if ast.IsRefType(varType) && ast.IsValueType(ast.RefInnerType(varType)) {
			varType = ast.RefInnerType(varType)
		}
		if !isNumericType(varType) {
			return c.errAt(s.Pos, "'--' requires numeric variable, got %s", typeName(varType))
		}
		return nil

	case *ast.CompoundAssignStmt:
		varType, ok := c.resolve(s.Name)
		if !ok {
			return c.errAt(s.Pos, "undefined variable '%s'", s.Name)
		}
		if c.isConst(s.Name) {
			return c.errAt(s.Pos, "cannot modify const variable '%s'", s.Name)
		}
		// Unwrap primitive ref for numeric check
		if ast.IsRefType(varType) && ast.IsValueType(ast.RefInnerType(varType)) {
			varType = ast.RefInnerType(varType)
		}
		if !isNumericType(varType) {
			return c.errAt(s.Pos, "compound assignment requires numeric variable, got %s", typeName(varType))
		}
		valType, err := c.checkExpr(s.Value)
		if err != nil {
			return err
		}
		if !isNumericType(valType) {
			return c.errAt(s.Pos, "compound assignment value must be numeric, got %s", typeName(valType))
		}
		return nil

	case *ast.BlockStmt:
		c.pushScope()
		for _, stmt := range s.Stmts {
			if err := c.checkStmt(stmt, returnType); err != nil {
				return err
			}
		}
		c.popScope()
		return nil

	case *ast.AssignStmt:
		varType, ok := c.resolve(s.Name)
		if !ok {
			return c.errAt(s.Pos, "undefined variable '%s'", s.Name)
		}
		if c.isConst(s.Name) {
			return c.errAt(s.Pos, "cannot reassign const variable '%s'", s.Name)
		}
		// Check if RHS is an #[owned] variable — aliasing is not allowed
		if ident, ok := s.Value.(*ast.Ident); ok {
			rhsAnnots := c.resolveAnnotations(ident.Name)
			if ast.HasAnnotation(rhsAnnots, ast.AnnotOwned) {
				return c.errAt(s.Pos, "cannot alias #[owned] variable '%s'", ident.Name)
			}
		}
		// Pre-annotate db.col() with the expected return type
		if call, ok := s.Value.(*ast.CallExpr); ok && call.Module == "db" && call.Name == "col" {
			call.ResolvedType = varType
		}
		// Pre-annotate json.decode() with the expected struct type
		if call, ok := s.Value.(*ast.CallExpr); ok && call.Module == "json" && call.Name == "decode" {
			call.ResolvedType = varType
		}
		exprType, err := c.checkExpr(s.Value)
		if err != nil {
			return err
		}
		// For primitive ref targets, unwrap for type comparison (allow int -> &int assignment)
		cmpType := varType
		if ast.IsRefType(varType) && ast.IsValueType(ast.RefInnerType(varType)) {
			cmpType = ast.RefInnerType(varType)
		}
		if exprType != cmpType && !canAssign(cmpType, exprType, s.Value) {
			return c.errAt(s.Pos, "type mismatch in assignment: expected %s, got %s", typeName(cmpType), typeName(exprType))
		}
		return nil

	case *ast.IndexAssignStmt:
		arrType, err := c.checkExpr(s.Array)
		if err != nil {
			return err
		}
		if ast.IsMapType(arrType) {
			idxType, err := c.checkExpr(s.Index)
			if err != nil {
				return err
			}
			keyType := ast.MapKeyType(arrType)
			if idxType != keyType {
				return c.errAt(s.Pos, "map key must be %s, got %s", typeName(keyType), typeName(idxType))
			}
			valType, err := c.checkExpr(s.Value)
			if err != nil {
				return err
			}
			expectedValType := ast.MapValueType(arrType)
			if valType != expectedValType && !canAssign(expectedValType, valType, s.Value) {
				return c.errAt(s.Pos, "type mismatch in map assignment: expected %s, got %s", typeName(expectedValType), typeName(valType))
			}
			return nil
		}
		if !ast.IsArrayType(arrType) {
			return c.errAt(s.Pos, "index assignment requires an array or map type, got %s", typeName(arrType))
		}
		idxType, err := c.checkExpr(s.Index)
		if err != nil {
			return err
		}
		if idxType != ast.TypeInt {
			return c.errAt(s.Pos, "array index must be int, got %s", typeName(idxType))
		}
		valType, err := c.checkExpr(s.Value)
		if err != nil {
			return err
		}
		elemType := ast.ElementType(arrType)
		if valType != elemType && !canAssign(elemType, valType, s.Value) {
			return c.errAt(s.Pos, "type mismatch in index assignment: expected %s, got %s", typeName(elemType), typeName(valType))
		}
		return nil

	case *ast.FieldAssignStmt:
		objType, err := c.checkExpr(s.Object)
		if err != nil {
			return err
		}
		// Unwrap ref type for field assignment
		if ast.IsRefType(objType) {
			objType = ast.RefInnerType(objType)
		}
		if !ast.IsStructType(objType) {
			return c.errAt(s.Pos, "field assignment requires a struct type, got %s", typeName(objType))
		}
		def := ast.GetStructDef(objType)
		var fieldType ast.Type
		found := false
		for _, f := range def.Fields {
			if f.Name == s.Field {
				fieldType = f.Type
				found = true
				break
			}
		}
		if !found {
			return c.errAt(s.Pos, "struct '%s' has no field '%s'", def.Name, s.Field)
		}
		valType, err := c.checkExpr(s.Value)
		if err != nil {
			return err
		}
		if valType != fieldType && !canAssign(fieldType, valType, s.Value) {
			return c.errAt(s.Pos, "type mismatch in field assignment: expected %s, got %s", typeName(fieldType), typeName(valType))
		}
		return nil

	case *ast.TryCatchStmt:
		// Check try body
		c.pushScope()
		for _, stmt := range s.Body {
			if err := c.checkStmt(stmt, returnType); err != nil {
				return err
			}
		}
		c.popScope()

		// Check catch body (if present)
		if s.CatchBody != nil {
			c.pushScope()
			// Define the catch variable as Exception struct type
			excType, ok := ast.LookupStructType("Exception")
			if !ok {
				return c.errAt(s.Pos, "built-in Exception type not registered")
			}
			c.define(s.CatchVar, excType)
			for _, stmt := range s.CatchBody {
				if err := c.checkStmt(stmt, returnType); err != nil {
					return err
				}
			}
			c.popScope()
		}

		// Check finally body (if present)
		if s.FinallyBody != nil {
			c.pushScope()
			for _, stmt := range s.FinallyBody {
				if err := c.checkStmt(stmt, returnType); err != nil {
					return err
				}
			}
			c.popScope()
		}
		return nil

	case *ast.ThrowStmt:
		exprType, err := c.checkExpr(s.Value)
		if err != nil {
			return err
		}
		excType, ok := ast.LookupStructType("Exception")
		if !ok {
			return c.errAt(s.Pos, "built-in Exception type not registered")
		}
		if exprType != excType {
			return c.errAt(s.Pos, "throw requires Exception type, got %s", typeName(exprType))
		}
		return nil

	case *ast.SendStmt:
		if s.Target != nil {
			// send(channel, value)
			chanType, err := c.checkExpr(s.Target)
			if err != nil {
				return err
			}
			if !ast.IsChanType(chanType) {
				return c.errAt(s.Pos, "send target must be a channel, got %s", typeName(chanType))
			}
			valType, err := c.checkExpr(s.Value)
			if err != nil {
				return err
			}
			if valType != ast.ChanElemType(chanType) {
				return c.errAt(s.Pos, "send type mismatch: channel expects %s, got %s", typeName(ast.ChanElemType(chanType)), typeName(valType))
			}
		} else {
			// send(value) — inside a spawn block, type checked via SpawnExpr inference
			_, err := c.checkExpr(s.Value)
			if err != nil {
				return err
			}
		}
		return nil

	case *ast.SwitchStmt:
		tagType, err := c.checkExpr(s.Tag)
		if err != nil {
			return err
		}
		// Tag must be a primitive type suitable for comparison
		if !isPrimitiveType(tagType) {
			return c.errAt(s.Pos, "switch tag must be a primitive type (int, string, char, long, double, bool), got %s", typeName(tagType))
		}
		// Check each case
		for _, sc := range s.Cases {
			for _, val := range sc.Values {
				valType, err := c.checkExpr(val)
				if err != nil {
					return err
				}
				if !canAssign(tagType, valType, val) {
					return c.errAt(sc.Pos, "case value type %s does not match switch tag type %s", typeName(valType), typeName(tagType))
				}
			}
			c.pushScope()
			for _, stmt := range sc.Body {
				if err := c.checkStmt(stmt, returnType); err != nil {
					return err
				}
			}
			c.popScope()
		}
		// Check default
		if s.Default != nil {
			c.pushScope()
			for _, stmt := range s.Default {
				if err := c.checkStmt(stmt, returnType); err != nil {
					return err
				}
			}
			c.popScope()
		}
		return nil

	case *ast.DeferStmt:
		_, err := c.checkExpr(s.Expr)
		return err

	case *ast.DestructureLetStmt:
		exprType, err := c.checkExpr(s.Value)
		if err != nil {
			return err
		}
		if !ast.IsStructType(exprType) {
			return c.errAt(s.Pos, "destructuring requires a struct type, got %s", typeName(exprType))
		}
		def := ast.GetStructDef(exprType)
		if def == nil {
			return c.errAt(s.Pos, "cannot destructure unknown struct type")
		}
		for _, name := range s.Names {
			found := false
			for _, f := range def.Fields {
				if f.Name == name {
					c.define(name, f.Type)
					if s.IsConst {
						c.defineConst(name)
					}
					found = true
					break
				}
			}
			if !found {
				return c.errAt(s.Pos, "struct '%s' has no field '%s'", def.Name, name)
			}
		}
		return nil

	default:
		return fmt.Errorf("unknown statement type")
	}
}
