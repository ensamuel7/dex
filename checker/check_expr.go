package checker

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

func (c *Checker) checkExpr(expr ast.Expr) (ast.Type, error) {
	switch e := expr.(type) {
	case *ast.IntLit:
		return ast.TypeInt, nil

	case *ast.FloatLit:
		return ast.TypeDouble, nil

	case *ast.BoolLit:
		return ast.TypeBool, nil

	case *ast.StringLit:
		return ast.TypeString, nil

	case *ast.NullLit:
		return ast.TypeNull, nil

	case *ast.MutexLit:
		return ast.TypeMutex, nil

	case *ast.CharLit:
		return ast.TypeChar, nil

	case *ast.Ident:
		typ, ok := c.resolve(e.Name)
		if !ok {
			// Check if it's a function reference
			if sig, found := c.funcs[e.Name]; found {
				return ast.FuncTypeOf(sig.Params, sig.ReturnType), nil
			}
			return 0, c.errAt(e.Pos, "undefined variable '%s'", e.Name)
		}
		return typ, nil

	case *ast.BinaryExpr:
		leftType, err := c.checkExpr(e.Left)
		if err != nil {
			return 0, err
		}
		rightType, err := c.checkExpr(e.Right)
		if err != nil {
			return 0, err
		}
		// Auto-unwrap primitive refs for arithmetic/comparison
		if ast.IsRefType(leftType) && ast.IsValueType(ast.RefInnerType(leftType)) {
			leftType = ast.RefInnerType(leftType)
		}
		if ast.IsRefType(rightType) && ast.IsValueType(ast.RefInnerType(rightType)) {
			rightType = ast.RefInnerType(rightType)
		}

		switch e.Op {
		case ast.BinAdd:
			if leftType == ast.TypeString && rightType == ast.TypeString {
				return ast.TypeString, nil
			}
			if isNumericType(leftType) && isNumericType(rightType) {
				if leftType != rightType {
					e.LeftType = leftType
					e.RightType = rightType
					e.HasMixedTypes = true
				}
				return widerNumericType(leftType, rightType), nil
			}
			// String coercion: string + T or T + string
			if (leftType == ast.TypeString && isStringifiable(rightType)) ||
				(rightType == ast.TypeString && isStringifiable(leftType)) {
				return ast.TypeString, nil
			}
			return 0, c.errAt(e.Pos, "'+' requires matching numeric or string operands")

		case ast.BinSub, ast.BinMul, ast.BinDiv:
			if !isNumericType(leftType) || !isNumericType(rightType) {
				return 0, c.errAt(e.Pos, "arithmetic operators require numeric operands")
			}
			if leftType != rightType {
				e.LeftType = leftType
				e.RightType = rightType
				e.HasMixedTypes = true
			}
			return widerNumericType(leftType, rightType), nil

		case ast.BinMod:
			if (leftType != ast.TypeChar && leftType != ast.TypeInt && leftType != ast.TypeLong) ||
				(rightType != ast.TypeChar && rightType != ast.TypeInt && rightType != ast.TypeLong) {
				return 0, c.errAt(e.Pos, "'%%' requires char, int, or long operands")
			}
			if leftType != rightType {
				e.LeftType = leftType
				e.RightType = rightType
				e.HasMixedTypes = true
			}
			return widerNumericType(leftType, rightType), nil

		case ast.BinEq, ast.BinNeq:
			// Allow null comparisons with optional types
			if leftType == ast.TypeNull && ast.IsOptionalType(rightType) {
				e.LeftType = leftType
				e.RightType = rightType
				return ast.TypeBool, nil
			}
			if rightType == ast.TypeNull && ast.IsOptionalType(leftType) {
				e.LeftType = leftType
				e.RightType = rightType
				return ast.TypeBool, nil
			}
			if leftType == rightType {
				return ast.TypeBool, nil
			}
			if isNumericType(leftType) && isNumericType(rightType) {
				e.LeftType = leftType
				e.RightType = rightType
				e.HasMixedTypes = true
				return ast.TypeBool, nil
			}
			return 0, c.errAt(e.Pos, "equality operators require matching types, or both numeric")

		case ast.BinStrictEq, ast.BinStrictNeq:
			if leftType != rightType {
				return 0, c.errAt(e.Pos, "strict equality requires matching types")
			}
			return ast.TypeBool, nil

		case ast.BinLt, ast.BinGt, ast.BinLte, ast.BinGte:
			if !isNumericType(leftType) || !isNumericType(rightType) {
				return 0, c.errAt(e.Pos, "comparison operators require numeric operands")
			}
			if leftType != rightType {
				e.LeftType = leftType
				e.RightType = rightType
				e.HasMixedTypes = true
			}
			return ast.TypeBool, nil

		case ast.BinAnd, ast.BinOr:
			if leftType != ast.TypeBool || rightType != ast.TypeBool {
				return 0, c.errAt(e.Pos, "logical operators require bool operands")
			}
			return ast.TypeBool, nil
		}

		return 0, c.errAt(e.Pos, "unknown binary operator")

	case *ast.UnaryExpr:
		operandType, err := c.checkExpr(e.Operand)
		if err != nil {
			return 0, err
		}
		// Auto-unwrap primitive refs for unary operations
		if ast.IsRefType(operandType) && ast.IsValueType(ast.RefInnerType(operandType)) {
			operandType = ast.RefInnerType(operandType)
		}

		switch e.Op {
		case ast.UnaryNeg:
			if !isNumericType(operandType) {
				return 0, c.errAt(e.Pos, "unary '-' requires numeric operand")
			}
			return operandType, nil
		case ast.UnaryNot:
			if operandType != ast.TypeBool {
				return 0, c.errAt(e.Pos, "unary '!' requires bool operand")
			}
			return ast.TypeBool, nil
		}

		return 0, c.errAt(e.Pos, "unknown unary operator")

	case *ast.ObjectLitExpr:
		// An object literal is always a json.Value: it is the only object literal
		// the language has, so it needs no annotation even when nested.
		for i, v := range e.Values {
			vt, err := c.checkExpr(v)
			if err != nil {
				return 0, err
			}
			if !isJsonEncodable(vt) {
				return 0, c.errAt(e.Pos, "value for key '%s' is %s, which has no JSON representation", e.Keys[i], typeName(vt))
			}
			if err := c.markJsonValue(v); err != nil {
				return 0, err
			}
		}
		return ast.TypeJsonValue, nil

	case *ast.ArrayLitExpr:
		if e.AsJsonValue {
			// Built as a JSON array, so the elements are free to differ in type.
			for _, elem := range e.Elems {
				et, err := c.checkExpr(elem)
				if err != nil {
					return 0, err
				}
				if !isJsonEncodable(et) {
					return 0, c.errAt(e.Pos, "array element is %s, which has no JSON representation", typeName(et))
				}
				if err := c.markJsonValue(elem); err != nil {
					return 0, err
				}
			}
			return ast.TypeJsonValue, nil
		}
		if len(e.Elems) == 0 {
			// A target type may already have been supplied, as for map literals.
			if e.ElemType != 0 || ast.IsStructType(e.ElemType) {
				if ast.IsStructType(e.ElemType) {
					return ast.StructArrayTypeOf(e.ElemType), nil
				}
				return ast.ArrayTypeOf(e.ElemType), nil
			}
			return 0, c.errAt(e.Pos, "cannot infer type of empty array literal")
		}
		firstType, err := c.checkExpr(e.Elems[0])
		if err != nil {
			return 0, err
		}
		for i := 1; i < len(e.Elems); i++ {
			t, err := c.checkExpr(e.Elems[i])
			if err != nil {
				return 0, err
			}
			if t != firstType {
				return 0, c.errAt(e.Pos, "array elements must be the same type: expected %s, got %s at index %d (annotate the target as json.Value for a mixed array)", typeName(firstType), typeName(t), i)
			}
		}
		e.ElemType = firstType
		return ast.ArrayTypeOf(firstType), nil

	case *ast.IndexExpr:
		arrType, err := c.checkExpr(e.Array)
		if err != nil {
			return 0, err
		}
		if arrType == ast.TypeJsonValue {
			// v[0] indexes an array, v["k"] a key. Both yield a json.Value, so a
			// path can be walked without stopping to say what shape it is.
			idxType, err := c.checkExpr(e.Index)
			if err != nil {
				return 0, err
			}
			if idxType != ast.TypeInt && idxType != ast.TypeString {
				return 0, c.errAt(e.Pos, "json.Value index must be int or string, got %s", typeName(idxType))
			}
			return ast.TypeJsonValue, nil
		}
		if ast.IsMapType(arrType) {
			idxType, err := c.checkExpr(e.Index)
			if err != nil {
				return 0, err
			}
			keyType := ast.MapKeyType(arrType)
			if idxType != keyType {
				return 0, c.errAt(e.Pos, "map key must be %s, got %s", typeName(keyType), typeName(idxType))
			}
			return ast.MapValueType(arrType), nil
		}
		if !ast.IsArrayType(arrType) {
			return 0, c.errAt(e.Pos, "index operator requires an array or map type, got %s", typeName(arrType))
		}
		idxType, err := c.checkExpr(e.Index)
		if err != nil {
			return 0, err
		}
		if idxType != ast.TypeInt {
			return 0, c.errAt(e.Pos, "array index must be int, got %s", typeName(idxType))
		}
		return ast.ElementType(arrType), nil

	case *ast.SliceExpr:
		arrType, err := c.checkExpr(e.Array)
		if err != nil {
			return 0, err
		}
		if !ast.IsArrayType(arrType) {
			return 0, c.errAt(e.Pos, "slice operator requires an array type, got %s", typeName(arrType))
		}
		if e.Start != nil {
			startType, err := c.checkExpr(e.Start)
			if err != nil {
				return 0, err
			}
			if startType != ast.TypeInt {
				return 0, c.errAt(e.Pos, "slice start must be int, got %s", typeName(startType))
			}
		}
		if e.End != nil {
			endType, err := c.checkExpr(e.End)
			if err != nil {
				return 0, err
			}
			if endType != ast.TypeInt {
				return 0, c.errAt(e.Pos, "slice end must be int, got %s", typeName(endType))
			}
		}
		return arrType, nil

	case *ast.StructLitExpr:
		t, ok := ast.LookupStructType(e.Name)
		if !ok {
			return 0, c.errAt(e.Pos, "unknown struct type '%s'", e.Name)
		}
		def := ast.GetStructDef(t)
		if len(e.FieldNames) > len(def.Fields) {
			return 0, c.errAt(e.Pos, "struct '%s' has %d fields, got %d", e.Name, len(def.Fields), len(e.FieldNames))
		}
		for i, fn := range e.FieldNames {
			// Find the field in the definition
			found := false
			for _, f := range def.Fields {
				if f.Name == fn {
					// A literal with no type of its own takes the field's type,
					// the same way a `let` with an annotation supplies one.
					if err := c.applyTargetType(e.FieldValues[i], f.Type); err != nil {
						return 0, err
					}
					valType, err := c.checkExpr(e.FieldValues[i])
					if err != nil {
						return 0, err
					}
					if !canAssign(f.Type, valType, e.FieldValues[i]) {
						return 0, c.errAt(e.Pos, "field '%s' of struct '%s': expected %s, got %s", fn, e.Name, typeName(f.Type), typeName(valType))
					}
					found = true
					break
				}
			}
			if !found {
				return 0, c.errAt(e.Pos, "struct '%s' has no field '%s'", e.Name, fn)
			}
		}
		return t, nil

	case *ast.FieldAccessExpr:
		objType, err := c.checkExpr(e.Object)
		if err != nil {
			return 0, err
		}
		// Unwrap ref type for field access
		if ast.IsRefType(objType) {
			objType = ast.RefInnerType(objType)
		}
		if !ast.IsStructType(objType) {
			return 0, c.errAt(e.Pos, "field access requires a struct type, got %s", typeName(objType))
		}
		def := ast.GetStructDef(objType)
		for _, f := range def.Fields {
			if f.Name == e.Field {
				return f.Type, nil
			}
		}
		// Not a field — a method named without calling it is a method value: the
		// method bound to this receiver, usable anywhere a function value is.
		if methods, ok := c.structMethods[def.Name]; ok {
			if sig, found := methods[e.Field]; found {
				e.IsMethodValue = true
				e.StructType = objType
				return ast.FuncTypeOf(sig.Params, sig.ReturnType), nil
			}
		}
		return 0, c.errAt(e.Pos, "struct '%s' has no field or method '%s'", def.Name, e.Field)

	case *ast.SpawnExpr:
		if e.Body != nil {
			// Check for captured variables with restricted annotations
			spawnUsed := make(map[string]bool)
			spawnDefined := make(map[string]bool)
			collectReferencedVars(e.Body, spawnUsed, spawnDefined)
			for name := range spawnUsed {
				if spawnDefined[name] {
					continue
				}
				annots := c.resolveAnnotations(name)
				if ast.HasAnnotation(annots, ast.AnnotOwned) {
					return 0, c.errAt(e.Pos, "cannot capture #[owned] variable '%s' in spawn block (would alias ownership)", name)
				}
				if ast.HasAnnotation(annots, ast.AnnotNoEscape) {
					return 0, c.errAt(e.Pos, "cannot capture #[noEscape] variable '%s' in spawn block (may escape scope)", name)
				}
				if ast.HasAnnotation(annots, ast.AnnotRegion) {
					return 0, c.errAt(e.Pos, "cannot capture #[region] variable '%s' in spawn block (region-bound, may escape scope)", name)
				}
			}

			c.pushScope()
			sendType := ast.TypeVoid
			for _, stmt := range e.Body {
				if err := c.checkStmt(stmt, ast.TypeVoid); err != nil {
					return 0, err
				}
				if ss, ok := stmt.(*ast.SendStmt); ok && ss.Target == nil {
					t, err := c.checkExpr(ss.Value)
					if err != nil {
						return 0, err
					}
					sendType = t
				}
			}
			c.popScope()
			e.ReturnType = sendType
			return ast.TaskTypeOf(sendType), nil
		}
		if e.Call != nil {
			call, ok := e.Call.(*ast.CallExpr)
			if !ok {
				return 0, c.errAt(e.Pos, "spawn expression must be a function call")
			}
			retType, err := c.checkExpr(call)
			if err != nil {
				return 0, err
			}
			e.ReturnType = retType
			return ast.TaskTypeOf(retType), nil
		}
		return 0, c.errAt(e.Pos, "spawn expression must have a body or function call")

	case *ast.ChannelExpr:
		return ast.ChanTypeOf(e.ElemType), nil

	case *ast.EnumAccessExpr:
		def := ast.GetEnumDef(e.EnumType)
		if def == nil {
			return 0, c.errAt(e.Pos, "unknown enum type '%s'", e.EnumName)
		}
		found := false
		for _, v := range def.Variants {
			if v == e.Variant {
				found = true
				break
			}
		}
		if !found {
			return 0, c.errAt(e.Pos, "enum '%s' has no variant '%s'", e.EnumName, e.Variant)
		}
		return e.EnumType, nil

	case *ast.StringInterpExpr:
		// Each non-string part must be stringifiable
		for _, part := range e.Parts {
			if _, ok := part.(*ast.StringLit); ok {
				continue
			}
			partType, err := c.checkExpr(part)
			if err != nil {
				return 0, err
			}
			if partType != ast.TypeString && !isStringifiable(partType) {
				return 0, c.errAt(e.Pos, "interpolated expression must be stringifiable, got %s", typeName(partType))
			}
		}
		return ast.TypeString, nil

	case *ast.MatchExpr:
		tagType, err := c.checkExpr(e.Tag)
		if err != nil {
			return 0, err
		}
		if len(e.Arms) == 0 {
			return 0, c.errAt(e.Pos, "match expression must have at least one arm")
		}
		hasWildcard := false
		var resultType ast.Type
		for i, arm := range e.Arms {
			if arm.IsWildcard {
				hasWildcard = true
			} else {
				for _, pat := range arm.Patterns {
					patType, err := c.checkExpr(pat)
					if err != nil {
						return 0, err
					}
					if !canAssign(tagType, patType, pat) && patType != tagType {
						return 0, c.errAt(arm.Pos, "match pattern type %s does not match tag type %s", typeName(patType), typeName(tagType))
					}
				}
			}
			bodyType, err := c.checkExpr(arm.Body)
			if err != nil {
				return 0, err
			}
			if i == 0 {
				resultType = bodyType
			} else if bodyType != resultType {
				return 0, c.errAt(arm.Pos, "match arm body type %s does not match first arm type %s", typeName(bodyType), typeName(resultType))
			}
		}
		if !hasWildcard {
			return 0, c.errAt(e.Pos, "match expression must have a wildcard '_' arm for exhaustiveness")
		}
		e.Type = resultType
		return resultType, nil

	case *ast.LambdaExpr:
		// Check for captured variables with restricted annotations
		used := make(map[string]bool)
		defined := make(map[string]bool)
		for _, p := range e.Params {
			defined[p.Name] = true
		}
		collectReferencedVars(e.Body, used, defined)
		for name := range used {
			if defined[name] {
				continue
			}
			// This is a captured variable — check its annotations
			annots := c.resolveAnnotations(name)
			if ast.HasAnnotation(annots, ast.AnnotOwned) {
				return 0, c.errAt(e.Pos, "cannot capture #[owned] variable '%s' in closure (would alias ownership)", name)
			}
			if ast.HasAnnotation(annots, ast.AnnotNoEscape) {
				return 0, c.errAt(e.Pos, "cannot capture #[noEscape] variable '%s' in closure (may escape scope)", name)
			}
			if ast.HasAnnotation(annots, ast.AnnotRegion) {
				return 0, c.errAt(e.Pos, "cannot capture #[region] variable '%s' in closure (region-bound, may escape scope)", name)
			}
		}

		// Push scope and define params
		c.pushScope()
		var paramTypes []ast.Type
		for _, p := range e.Params {
			c.define(p.Name, p.Type)
			paramTypes = append(paramTypes, p.Type)
		}
		// Check body
		for _, stmt := range e.Body {
			if err := c.checkStmt(stmt, e.ReturnType); err != nil {
				c.popScope()
				return 0, err
			}
		}
		c.popScope()
		return ast.FuncTypeOf(paramTypes, e.ReturnType), nil

	case *ast.MapLitExpr:
		if e.AsJsonValue {
			// `{}` where a json.Value is expected is the empty JSON object.
			return ast.TypeJsonValue, nil
		}
		// A target type may already have been supplied — by a let annotation, or
		// by the field this literal initialises.
		if ast.IsMapType(e.MapType) {
			return e.MapType, nil
		}
		return 0, c.errAt(e.Pos, "cannot infer type of empty map literal")

	case *ast.ReceiveExpr:
		srcType, err := c.checkExpr(e.Source)
		if err != nil {
			return 0, err
		}
		if ast.IsChanType(srcType) {
			return ast.ChanElemType(srcType), nil
		}
		if ast.IsTaskType(srcType) {
			return ast.TaskReturnType(srcType), nil
		}
		return 0, c.errAt(e.Pos, "receive() requires a channel or task handle, got %s", typeName(srcType))

	case *ast.CallExpr:
		// Method call on an arbitrary receiver expression, e.g. parsed[0].asInt().
		// Only types whose methods do not depend on a variable name can be
		// reached this way; the rest still go through the named-receiver path.
		if e.Recv != nil {
			recvType, err := c.checkExpr(e.Recv)
			if err != nil {
				return 0, err
			}
			if ast.IsRefType(recvType) {
				recvType = ast.RefInnerType(recvType)
			}
			if recvType == ast.TypeJsonValue {
				ret, err := c.checkJsonValueMethod(e.Name, e.Args, e.Pos)
				if err != nil {
					return 0, err
				}
				// Codegen needs the result type to declare the temp it releases
				// the receiver through.
				e.ResolvedType = ret
				return ret, nil
			}
			return 0, c.errAt(e.Pos, "cannot call method '%s' on a %s expression; assign it to a variable first", e.Name, typeName(recvType))
		}

		// Qualified call: module.func()
		if e.Module != "" {
			// Check if module is actually a variable (array method call)
			varType, isVar := c.resolve(e.Module)
			if isVar && ast.IsArrayType(varType) {
				retType, err := c.checkArrayMethod(e.Module, varType, e.Name, e.Args)
				if err != nil {
					return 0, c.errAt(e.Pos, "%s", err)
				}
				return retType, nil
			}

			// Check if module is a map variable (map method call)
			if isVar && ast.IsMapType(varType) {
				retType, err := c.checkMapMethod(e.Module, varType, e.Name, e.Args, e.Pos)
				if err != nil {
					return 0, err
				}
				return retType, nil
			}

			// Check if module is a json.Value variable
			if isVar && varType == ast.TypeJsonValue {
				retType, err := c.checkJsonValueMethod(e.Name, e.Args, e.Pos)
				if err != nil {
					return 0, err
				}
				e.ResolvedType = retType
				return retType, nil
			}

			// Check if module is a string variable (string method call)
			if isVar && varType == ast.TypeString {
				retType, err := c.checkStringMethod(e.Module, e.Name, e.Args, e.Pos)
				if err != nil {
					return 0, err
				}
				return retType, nil
			}

			// Check if module is a mutex variable (mutex method call)
			if isVar && varType == ast.TypeMutex {
				switch e.Name {
				case "lock":
					if len(e.Args) != 0 {
						return 0, c.errAt(e.Pos, "mutex.lock() takes no arguments, got %d", len(e.Args))
					}
					return ast.TypeVoid, nil
				case "unlock":
					if len(e.Args) != 0 {
						return 0, c.errAt(e.Pos, "mutex.unlock() takes no arguments, got %d", len(e.Args))
					}
					return ast.TypeVoid, nil
				default:
					return 0, c.errAt(e.Pos, "mutex has no method '%s'", e.Name)
				}
			}

			// Check if module is a StringBuilder variable (StringBuilder method call)
			if isVar && varType == ast.TypeStringBuilder {
				retType, err := c.checkStringBuilderMethod(e.Module, e.Name, e.Args, e.Pos)
				if err != nil {
					return 0, err
				}
				return retType, nil
			}

			// Resolve dotted field access: self.database -> look up field type
			// This handles struct methods that call methods on struct-typed fields.
			if !isVar && strings.Contains(e.Module, ".") {
				parts := strings.SplitN(e.Module, ".", 2)
				baseType, baseOk := c.resolve(parts[0])
				// Unwrap ref type for dotted path resolution
				if baseOk && ast.IsRefType(baseType) {
					baseType = ast.RefInnerType(baseType)
				}
				if baseOk && ast.IsStructType(baseType) {
					structDef := ast.GetStructDef(baseType)
					if structDef != nil {
						for _, field := range structDef.Fields {
							if field.Name == parts[1] {
								varType = field.Type
								isVar = true
								break
							}
						}
					}
				}
			}

			// Unwrap ref type for method calls
			if isVar && ast.IsRefType(varType) {
				varType = ast.RefInnerType(varType)
			}
			// Array method call on dotted field (e.g. charger.connectors.len())
			if isVar && ast.IsArrayType(varType) {
				retType, err := c.checkArrayMethod(e.Module, varType, e.Name, e.Args)
				if err != nil {
					return 0, err
				}
				return retType, nil
			}
			// Map method call on dotted field (e.g. req.params.get("id"))
			if isVar && ast.IsMapType(varType) {
				retType, err := c.checkMapMethod(e.Module, varType, e.Name, e.Args, e.Pos)
				if err != nil {
					return 0, err
				}
				return retType, nil
			}
			// json.Value method call on dotted field (e.g. msg.payload.asInt())
			if isVar && varType == ast.TypeJsonValue {
				retType, err := c.checkJsonValueMethod(e.Name, e.Args, e.Pos)
				if err != nil {
					return 0, err
				}
				e.ResolvedType = retType
				return retType, nil
			}
			// String method call on dotted field (e.g. req.path.len())
			if isVar && varType == ast.TypeString {
				retType, err := c.checkStringMethod(e.Module, e.Name, e.Args, e.Pos)
				if err != nil {
					return 0, err
				}
				return retType, nil
			}
			// Mutex method call on dotted field (e.g. self.mu.lock())
			if isVar && varType == ast.TypeMutex {
				switch e.Name {
				case "lock":
					if len(e.Args) != 0 {
						return 0, c.errAt(e.Pos, "mutex.lock() takes no arguments, got %d", len(e.Args))
					}
					return ast.TypeVoid, nil
				case "unlock":
					if len(e.Args) != 0 {
						return 0, c.errAt(e.Pos, "mutex.unlock() takes no arguments, got %d", len(e.Args))
					}
					return ast.TypeVoid, nil
				default:
					return 0, c.errAt(e.Pos, "mutex has no method '%s'", e.Name)
				}
			}
			// Struct method call: instance.method()
			if isVar && ast.IsStructType(varType) {
				structDef := ast.GetStructDef(varType)
				if structDef != nil {
					if methods, ok := c.structMethods[structDef.Name]; ok {
						if methodSig, ok := methods[e.Name]; ok {
							e.IsMethodCall = true
							e.StructType = varType
							if len(e.Args) != len(methodSig.Params) {
								return 0, c.errAt(e.Pos, "%s.%s() takes exactly %d argument(s), got %d", e.Module, e.Name, len(methodSig.Params), len(e.Args))
							}
							for i, arg := range e.Args {
								argType, err := c.checkExpr(arg)
								if err != nil {
									return 0, err
								}
								if argType != methodSig.Params[i] && !canAssign(methodSig.Params[i], argType, e.Args[i]) {
									return 0, c.errAt(e.Pos, "%s.%s() argument %d must be %s, got %s", e.Module, e.Name, i+1, typeName(methodSig.Params[i]), typeName(argType))
								}
							}
							return methodSig.ReturnType, nil
						}
					}
				}
			}

			// Constructor call from user module: module.StructName(args)
			if c.userModules[e.Module] {
				if ctorParams, ok := c.structConstructors[e.Name]; ok {
					e.IsConstructor = true
					structType, stOk := ast.LookupStructType(e.Name)
					if !stOk {
						return 0, c.errAt(e.Pos, "unknown struct type '%s'", e.Name)
					}
					e.StructType = structType
					if len(e.Args) != len(ctorParams) {
						return 0, c.errAt(e.Pos, "%s() constructor takes exactly %d argument(s), got %d", e.Name, len(ctorParams), len(e.Args))
					}
					for i, arg := range e.Args {
						argType, err := c.checkExpr(arg)
						if err != nil {
							return 0, err
						}
						if !canAssign(ctorParams[i], argType, arg) {
							return 0, c.errAt(e.Pos, "%s() constructor argument %d must be %s, got %s", e.Name, i+1, typeName(ctorParams[i]), typeName(argType))
						}
					}
					return structType, nil
				}
			}

			// User module call: module.func() → look up prefixed name
			if c.userModules[e.Module] {
				prefixedName := e.Module + "_" + e.Name
				sig, ok := c.funcs[prefixedName]
				if !ok {
					return 0, c.errAt(e.Pos, "undefined function '%s' in module '%s'", e.Name, e.Module)
				}
				if sig.IsPrivate {
					return 0, c.errAt(e.Pos, "function '%s' in module '%s' is private", e.Name, e.Module)
				}
				if len(e.Args) != len(sig.Params) {
					return 0, c.errAt(e.Pos, "%s.%s() takes exactly %d argument(s), got %d", e.Module, e.Name, len(sig.Params), len(e.Args))
				}
				for i, arg := range e.Args {
					argType, err := c.checkExpr(arg)
					if err != nil {
						return 0, err
					}
					if argType != sig.Params[i] && !canAssign(sig.Params[i], argType, e.Args[i]) {
						return 0, c.errAt(e.Pos, "%s.%s() argument %d must be %s, got %s", e.Module, e.Name, i+1, typeName(sig.Params[i]), typeName(argType))
					}
				}
				return sig.ReturnType, nil
			}

			mod, ok := c.imports[e.Module]
			if !ok {
				return 0, c.errAt(e.Pos, "module '%s' is not imported", e.Module)
			}
			c.warnIfDeprecated(e.Module, e.Name, e.Pos)

			if t, handled, err := c.checkStdlibCall(e, mod); handled {
				return t, err
			}

			funcDef, ok := mod.Funcs[e.Name]
			if !ok {
				return 0, c.errAt(e.Pos, "undefined function '%s' in module '%s'", e.Name, e.Module)
			}
			if len(e.Args) != len(funcDef.Params) {
				return 0, c.errAt(e.Pos, "%s.%s() takes exactly %d argument(s), got %d", e.Module, e.Name, len(funcDef.Params), len(e.Args))
			}
			for i, arg := range e.Args {
				argType, err := c.checkExpr(arg)
				if err != nil {
					return 0, err
				}
				if argType != funcDef.Params[i] && !canAssign(funcDef.Params[i], argType, e.Args[i]) {
					return 0, c.errAt(e.Pos, "%s.%s() argument %d must be %s, got %s", e.Module, e.Name, i+1, typeName(funcDef.Params[i]), typeName(argType))
				}
			}
			return funcDef.ReturnType, nil
		}

		// Built-in: assert(condition: bool)
		if e.Name == "assert" {
			if len(e.Args) != 1 {
				return 0, c.errAt(e.Pos, "assert() takes exactly 1 argument, got %d", len(e.Args))
			}
			argType, err := c.checkExpr(e.Args[0])
			if err != nil {
				return 0, err
			}
			if argType != ast.TypeBool {
				return 0, c.errAt(e.Pos, "assert() argument must be bool, got %s", typeName(argType))
			}
			return ast.TypeVoid, nil
		}

		// Built-in: close(channel)
		if e.Name == "close" {
			if len(e.Args) != 1 {
				return 0, c.errAt(e.Pos, "close() takes exactly 1 argument, got %d", len(e.Args))
			}
			argType, err := c.checkExpr(e.Args[0])
			if err != nil {
				return 0, err
			}
			if !ast.IsChanType(argType) {
				return 0, c.errAt(e.Pos, "close() argument must be a channel, got %s", typeName(argType))
			}
			return ast.TypeVoid, nil
		}

		// Check if calling through a function-typed variable (indirect call)
		if varType, ok := c.resolve(e.Name); ok && ast.IsFuncType(varType) {
			fnParams := ast.FuncTypeParams(varType)
			fnReturn := ast.FuncTypeReturn(varType)
			if len(e.Args) != len(fnParams) {
				return 0, c.errAt(e.Pos, "function variable '%s' expects %d arguments, got %d", e.Name, len(fnParams), len(e.Args))
			}
			for i, arg := range e.Args {
				argType, err := c.checkExpr(arg)
				if err != nil {
					return 0, err
				}
				if argType != fnParams[i] {
					return 0, c.errAt(e.Pos, "argument %d of '%s': expected %s, got %s", i+1, e.Name, typeName(fnParams[i]), typeName(argType))
				}
			}
			return fnReturn, nil
		}

		// Built-in StringBuilder constructor
		if e.Name == "StringBuilder" && e.Module == "" {
			if len(e.Args) != 0 {
				return 0, c.errAt(e.Pos, "StringBuilder() takes no arguments")
			}
			return ast.TypeStringBuilder, nil
		}

		// Unqualified constructor call: StructName(args)
		if ctorParams, hasCtor := c.structConstructors[e.Name]; hasCtor {
			e.IsConstructor = true
			structType, stOk := ast.LookupStructType(e.Name)
			if !stOk {
				return 0, c.errAt(e.Pos, "unknown struct type '%s'", e.Name)
			}
			e.StructType = structType
			if len(e.Args) != len(ctorParams) {
				return 0, c.errAt(e.Pos, "%s() constructor takes exactly %d argument(s), got %d", e.Name, len(ctorParams), len(e.Args))
			}
			for i, arg := range e.Args {
				argType, err := c.checkExpr(arg)
				if err != nil {
					return 0, err
				}
				if !canAssign(ctorParams[i], argType, arg) {
					return 0, c.errAt(e.Pos, "%s() constructor argument %d must be %s, got %s", e.Name, i+1, typeName(ctorParams[i]), typeName(argType))
				}
			}
			return structType, nil
		}

		// Unqualified call: user-defined function
		sig, ok := c.funcs[e.Name]
		if !ok {
			return 0, c.errAt(e.Pos, "undefined function '%s'", e.Name)
		}
		// Handle named arguments: reorder args to match parameter order
		if len(e.ArgNames) > 0 {
			if err := c.resolveNamedArgs(e, sig); err != nil {
				return 0, err
			}
		}
		// Handle default parameters: fill in missing args
		if len(e.Args) < len(sig.Params) {
			if err := c.fillDefaultArgs(e, sig); err != nil {
				return 0, err
			}
		}
		if len(e.Args) != len(sig.Params) {
			return 0, c.errAt(e.Pos, "function '%s' expects %d arguments, got %d", e.Name, len(sig.Params), len(e.Args))
		}
		for i, arg := range e.Args {
			argType, err := c.checkExpr(arg)
			if err != nil {
				return 0, err
			}
			if argType != sig.Params[i] && !canAssign(sig.Params[i], argType, arg) {
				return 0, c.errAt(e.Pos, "argument %d of '%s': expected %s, got %s", i+1, e.Name, typeName(sig.Params[i]), typeName(argType))
			}
		}
		return sig.ReturnType, nil

	default:
		return 0, fmt.Errorf("unknown expression type")
	}
}
