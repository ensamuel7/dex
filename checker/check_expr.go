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

	case *ast.ArrayLitExpr:
		if len(e.Elems) == 0 {
			// Empty array literal — type must be inferred from context (LetStmt handles this)
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
				return 0, c.errAt(e.Pos, "array elements must be the same type: expected %s, got %s at index %d", typeName(firstType), typeName(t), i)
			}
		}
		e.ElemType = firstType
		return ast.ArrayTypeOf(firstType), nil

	case *ast.IndexExpr:
		arrType, err := c.checkExpr(e.Array)
		if err != nil {
			return 0, err
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
		return 0, c.errAt(e.Pos, "struct '%s' has no field '%s'", def.Name, e.Field)

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
		// Empty map literal — type must be inferred from context (LetStmt handles this)
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
			// Map method call on dotted field (e.g. req.params.get("id"))
			if isVar && ast.IsMapType(varType) {
				retType, err := c.checkMapMethod(e.Module, varType, e.Name, e.Args, e.Pos)
				if err != nil {
					return 0, err
				}
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

			// Special case: fmt.print/fmt.println — accepts any primitive type
			if e.Module == "fmt" && (e.Name == "print" || e.Name == "println") {
				if len(e.Args) != 1 {
					return 0, c.errAt(e.Pos, "fmt.%s() takes exactly 1 argument, got %d", e.Name, len(e.Args))
				}
				argType, err := c.checkExpr(e.Args[0])
				if err != nil {
					return 0, err
				}
				// Auto-unwrap ref types for printing (e.g., &User -> User)
				printType := argType
				if ast.IsRefType(printType) {
					printType = ast.RefInnerType(printType)
				}
				if printType != ast.TypeInt && printType != ast.TypeLong && printType != ast.TypeDouble &&
					printType != ast.TypeString && printType != ast.TypeBool && printType != ast.TypeChar &&
					!ast.IsEnumType(printType) && !ast.IsArrayType(printType) && !ast.IsStructType(printType) {
					return 0, c.errAt(e.Pos, "fmt.%s() argument must be a primitive type, got %s", e.Name, typeName(argType))
				}
				return ast.TypeVoid, nil
			}

			// Special case: json.stringify(array or struct) -> string
			if e.Module == "json" && e.Name == "stringify" {
				if len(e.Args) != 1 {
					return 0, c.errAt(e.Pos, "json.stringify() takes exactly 1 argument, got %d", len(e.Args))
				}
				argType, err := c.checkExpr(e.Args[0])
				if err != nil {
					return 0, err
				}
				if !ast.IsArrayType(argType) && !ast.IsStructType(argType) && !ast.IsMapType(argType) {
					return 0, c.errAt(e.Pos, "json.stringify() argument must be an array, struct, or map type, got %s", typeName(argType))
				}
				if ast.IsMapType(argType) && ast.MapKeyType(argType) != ast.TypeString {
					return 0, c.errAt(e.Pos, "json.stringify() map key type must be string for JSON serialization, got %s", typeName(ast.MapKeyType(argType)))
				}
				return ast.TypeString, nil
			}

			// Special case: json.objectify(jsonStr) -> struct (resolved from context)
			if e.Module == "json" && e.Name == "objectify" {
				if len(e.Args) != 1 {
					return 0, c.errAt(e.Pos, "json.objectify() takes exactly 1 argument, got %d", len(e.Args))
				}
				argType, err := c.checkExpr(e.Args[0])
				if err != nil {
					return 0, err
				}
				if argType != ast.TypeString {
					return 0, c.errAt(e.Pos, "json.objectify() argument must be string, got %s", typeName(argType))
				}
				if e.ResolvedType == 0 || !ast.IsStructType(e.ResolvedType) {
					return 0, c.errAt(e.Pos, "json.objectify() requires an explicit struct type annotation (e.g., let x: MyStruct = json.objectify(...))")
				}
				return e.ResolvedType, nil
			}

			// Special case: json.set(obj, key, value) — polymorphic value type
			if e.Module == "json" && e.Name == "set" {
				if len(e.Args) != 3 {
					return 0, c.errAt(e.Pos, "json.set() takes exactly 3 arguments, got %d", len(e.Args))
				}
				objType, err := c.checkExpr(e.Args[0])
				if err != nil {
					return 0, err
				}
				if objType != ast.TypeString {
					return 0, c.errAt(e.Pos, "json.set() argument 1 must be string, got %s", typeName(objType))
				}
				keyType, err := c.checkExpr(e.Args[1])
				if err != nil {
					return 0, err
				}
				if keyType != ast.TypeString {
					return 0, c.errAt(e.Pos, "json.set() argument 2 must be string, got %s", typeName(keyType))
				}
				valType, err := c.checkExpr(e.Args[2])
				if err != nil {
					return 0, err
				}
				switch valType {
				case ast.TypeString, ast.TypeInt, ast.TypeBool, ast.TypeLong, ast.TypeDouble:
					// all valid
				default:
					if !ast.IsArrayType(valType) {
						return 0, c.errAt(e.Pos, "json.set() value must be a primitive type or array, got %s", typeName(valType))
					}
				}
				return ast.TypeString, nil
			}

			// Special case: db.col(rows, col) — return type resolved from context
			if e.Module == "db" && e.Name == "col" {
				if len(e.Args) != 2 {
					return 0, c.errAt(e.Pos, "db.col() takes exactly 2 arguments, got %d", len(e.Args))
				}
				for i, arg := range e.Args {
					argType, err := c.checkExpr(arg)
					if err != nil {
						return 0, err
					}
					if argType != ast.TypeInt {
						return 0, c.errAt(e.Pos, "db.col() argument %d must be int, got %s", i+1, typeName(argType))
					}
				}
				return e.ResolvedType, nil
			}

			// Special case: json.arrayPush(arr, value) — polymorphic value type
			if e.Module == "json" && e.Name == "arrayPush" {
				if len(e.Args) != 2 {
					return 0, c.errAt(e.Pos, "json.arrayPush() takes exactly 2 arguments, got %d", len(e.Args))
				}
				arrType, err := c.checkExpr(e.Args[0])
				if err != nil {
					return 0, err
				}
				if arrType != ast.TypeString {
					return 0, c.errAt(e.Pos, "json.arrayPush() argument 1 must be string, got %s", typeName(arrType))
				}
				valType, err := c.checkExpr(e.Args[1])
				if err != nil {
					return 0, err
				}
				switch valType {
				case ast.TypeString, ast.TypeInt, ast.TypeBool, ast.TypeLong, ast.TypeDouble:
					// all valid
				default:
					return 0, c.errAt(e.Pos, "json.arrayPush() value must be a primitive type, got %s", typeName(valType))
				}
				return ast.TypeString, nil
			}

			// Special case: json.setArray(obj, key, array) -> string
			if e.Module == "json" && e.Name == "setArray" {
				if len(e.Args) != 3 {
					return 0, c.errAt(e.Pos, "json.setArray() takes exactly 3 arguments, got %d", len(e.Args))
				}
				objType, err := c.checkExpr(e.Args[0])
				if err != nil {
					return 0, err
				}
				if objType != ast.TypeString {
					return 0, c.errAt(e.Pos, "json.setArray() argument 1 must be string, got %s", typeName(objType))
				}
				keyType, err := c.checkExpr(e.Args[1])
				if err != nil {
					return 0, err
				}
				if keyType != ast.TypeString {
					return 0, c.errAt(e.Pos, "json.setArray() argument 2 must be string, got %s", typeName(keyType))
				}
				arrType, err := c.checkExpr(e.Args[2])
				if err != nil {
					return 0, err
				}
				if !ast.IsArrayType(arrType) {
					return 0, c.errAt(e.Pos, "json.setArray() argument 3 must be an array type, got %s", typeName(arrType))
				}
				return ast.TypeString, nil
			}

			// Special case: http.route handler validation (before generic arg check)
			if e.Module == "http" && e.Name == "route" {
				if len(e.Args) != 3 {
					return 0, c.errAt(e.Pos, "http.route() takes exactly 3 arguments, got %d", len(e.Args))
				}
				// Check method (arg 0) and path (arg 1) are strings
				for i := 0; i < 2; i++ {
					argType, err := c.checkExpr(e.Args[i])
					if err != nil {
						return 0, err
					}
					if argType != ast.TypeString {
						return 0, c.errAt(e.Pos, "http.route() argument %d must be string, got %s", i+1, typeName(argType))
					}
				}
				// Check handler (arg 2): accept string literal, function reference (Ident), or function call
				var handlerName string
				switch h := e.Args[2].(type) {
				case *ast.StringLit:
					handlerName = h.Value
				case *ast.Ident:
					handlerName = h.Name
					if _, exists := c.funcs[handlerName]; !exists {
						for mod := range c.userModules {
							prefixed := mod + "_" + handlerName
							if _, ok := c.funcs[prefixed]; ok {
								handlerName = prefixed
								break
							}
						}
					}
				case *ast.CallExpr:
					if len(h.Args) != 0 {
						return 0, c.errAt(e.Pos, "http.route() handler function must take no arguments")
					}
					handlerName = h.Name
					if h.Module != "" && c.userModules[h.Module] {
						handlerName = h.Module + "_" + h.Name
					}
				default:
					return 0, c.errAt(e.Pos, "http.route() handler (argument 3) must be a function name, string literal, or function call")
				}
				sig, ok := c.funcs[handlerName]
				if !ok {
					return 0, c.errAt(e.Pos, "http.route() handler '%s' is not a defined function", handlerName)
				}
				// Allow 0-param handlers or 1-param handlers taking http.HttpRequest
				if len(sig.Params) == 0 {
					// OK: no-param handler (backward compat)
				} else if len(sig.Params) == 1 {
					httpReqType, hasReq := ast.LookupStructType("HttpRequest")
					if !hasReq {
						return 0, c.errAt(e.Pos, "HttpRequest type not registered (internal error)")
					}
					if sig.Params[0] != httpReqType {
						return 0, c.errAt(e.Pos, "http.route() handler '%s' parameter must be http.HttpRequest, got %s", handlerName, typeName(sig.Params[0]))
					}
				} else {
					return 0, c.errAt(e.Pos, "http.route() handler '%s' must take 0 or 1 parameter (http.HttpRequest)", handlerName)
				}
				if sig.ReturnType == ast.TypeVoid {
					return 0, c.errAt(e.Pos, "http.route() handler '%s' must have a return type", handlerName)
				}
				return ast.TypeVoid, nil
			}

			// Special case: http.response(statusCode, body, contentType) -> HttpResponse
			if e.Module == "http" && e.Name == "response" {
				httpRespType, ok := ast.LookupStructType("HttpResponse")
				if !ok {
					return 0, c.errAt(e.Pos, "HttpResponse type not registered (internal error)")
				}
				if len(e.Args) != 3 {
					return 0, c.errAt(e.Pos, "http.response() takes exactly 3 arguments, got %d", len(e.Args))
				}
				arg0Type, err := c.checkExpr(e.Args[0])
				if err != nil {
					return 0, err
				}
				if arg0Type != ast.TypeInt {
					return 0, c.errAt(e.Pos, "http.response() argument 1 must be int, got %s", typeName(arg0Type))
				}
				for i := 1; i < 3; i++ {
					argType, err := c.checkExpr(e.Args[i])
					if err != nil {
						return 0, err
					}
					if argType != ast.TypeString {
						return 0, c.errAt(e.Pos, "http.response() argument %d must be string, got %s", i+1, typeName(argType))
					}
				}
				e.ResolvedType = httpRespType
				return httpRespType, nil
			}

			// Special case: ws.handleMessage(handler) — validate handler signature
			if e.Module == "ws" && e.Name == "handleMessage" {
				if len(e.Args) != 1 {
					return 0, c.errAt(e.Pos, "ws.handleMessage() takes exactly 1 argument, got %d", len(e.Args))
				}
				var handlerName string
				switch h := e.Args[0].(type) {
				case *ast.Ident:
					handlerName = h.Name
					if _, exists := c.funcs[handlerName]; !exists {
						for mod := range c.userModules {
							prefixed := mod + "_" + handlerName
							if _, ok := c.funcs[prefixed]; ok {
								handlerName = prefixed
								break
							}
						}
					}
				case *ast.CallExpr:
					if len(h.Args) != 0 {
						return 0, c.errAt(e.Pos, "ws.handleMessage() handler must take exactly 2 parameters")
					}
					handlerName = h.Name
					if h.Module != "" && c.userModules[h.Module] {
						handlerName = h.Module + "_" + h.Name
					}
				default:
					return 0, c.errAt(e.Pos, "ws.handleMessage() argument must be a function name")
				}
				sig, ok := c.funcs[handlerName]
				if !ok {
					return 0, c.errAt(e.Pos, "ws.handleMessage() handler '%s' is not a defined function", handlerName)
				}
				if len(sig.Params) != 2 {
					return 0, c.errAt(e.Pos, "ws.handleMessage() handler '%s' must take 2 parameters (ws.Conn, string), got %d", handlerName, len(sig.Params))
				}
				wsConnType, hasConn := ast.LookupStructType("Conn")
				if !hasConn {
					return 0, c.errAt(e.Pos, "ws.Conn type not registered (internal error)")
				}
				if sig.Params[0] != wsConnType {
					return 0, c.errAt(e.Pos, "ws.handleMessage() handler '%s' parameter 1 must be ws.Conn, got %s", handlerName, typeName(sig.Params[0]))
				}
				if sig.Params[1] != ast.TypeString {
					return 0, c.errAt(e.Pos, "ws.handleMessage() handler '%s' parameter 2 must be string, got %s", handlerName, typeName(sig.Params[1]))
				}
				if sig.ReturnType != ast.TypeVoid {
					return 0, c.errAt(e.Pos, "ws.handleMessage() handler '%s' must return void", handlerName)
				}
				return ast.TypeVoid, nil
			}

			// Special case: ws.handleConnect(handler) — validate handler signature fn(ws.Conn, string): void
			if e.Module == "ws" && e.Name == "handleConnect" {
				if len(e.Args) != 1 {
					return 0, c.errAt(e.Pos, "ws.handleConnect() takes exactly 1 argument, got %d", len(e.Args))
				}
				var handlerName string
				switch h := e.Args[0].(type) {
				case *ast.Ident:
					handlerName = h.Name
					if _, exists := c.funcs[handlerName]; !exists {
						for mod := range c.userModules {
							prefixed := mod + "_" + handlerName
							if _, ok := c.funcs[prefixed]; ok {
								handlerName = prefixed
								break
							}
						}
					}
				case *ast.CallExpr:
					if len(h.Args) != 0 {
						return 0, c.errAt(e.Pos, "ws.handleConnect() handler must take exactly 2 parameters")
					}
					handlerName = h.Name
					if h.Module != "" && c.userModules[h.Module] {
						handlerName = h.Module + "_" + h.Name
					}
				default:
					return 0, c.errAt(e.Pos, "ws.handleConnect() argument must be a function name")
				}
				sig, ok := c.funcs[handlerName]
				if !ok {
					return 0, c.errAt(e.Pos, "ws.handleConnect() handler '%s' is not a defined function", handlerName)
				}
				if len(sig.Params) != 2 {
					return 0, c.errAt(e.Pos, "ws.handleConnect() handler '%s' must take 2 parameters (ws.Conn, string), got %d", handlerName, len(sig.Params))
				}
				wsConnType, hasConn := ast.LookupStructType("Conn")
				if !hasConn {
					return 0, c.errAt(e.Pos, "ws.Conn type not registered (internal error)")
				}
				if sig.Params[0] != wsConnType {
					return 0, c.errAt(e.Pos, "ws.handleConnect() handler '%s' parameter 1 must be ws.Conn, got %s", handlerName, typeName(sig.Params[0]))
				}
				if sig.Params[1] != ast.TypeString {
					return 0, c.errAt(e.Pos, "ws.handleConnect() handler '%s' parameter 2 must be string, got %s", handlerName, typeName(sig.Params[1]))
				}
				if sig.ReturnType != ast.TypeVoid {
					return 0, c.errAt(e.Pos, "ws.handleConnect() handler '%s' must return void", handlerName)
				}
				return ast.TypeVoid, nil
			}

			// Special case: ws.handleDisconnect(handler) — validate handler signature fn(ws.Conn): void
			if e.Module == "ws" && e.Name == "handleDisconnect" {
				if len(e.Args) != 1 {
					return 0, c.errAt(e.Pos, "ws.handleDisconnect() takes exactly 1 argument, got %d", len(e.Args))
				}
				var handlerName string
				switch h := e.Args[0].(type) {
				case *ast.Ident:
					handlerName = h.Name
					if _, exists := c.funcs[handlerName]; !exists {
						for mod := range c.userModules {
							prefixed := mod + "_" + handlerName
							if _, ok := c.funcs[prefixed]; ok {
								handlerName = prefixed
								break
							}
						}
					}
				case *ast.CallExpr:
					if len(h.Args) != 0 {
						return 0, c.errAt(e.Pos, "ws.handleDisconnect() handler must take exactly 1 parameter")
					}
					handlerName = h.Name
					if h.Module != "" && c.userModules[h.Module] {
						handlerName = h.Module + "_" + h.Name
					}
				default:
					return 0, c.errAt(e.Pos, "ws.handleDisconnect() argument must be a function name")
				}
				sig, ok := c.funcs[handlerName]
				if !ok {
					return 0, c.errAt(e.Pos, "ws.handleDisconnect() handler '%s' is not a defined function", handlerName)
				}
				if len(sig.Params) != 1 {
					return 0, c.errAt(e.Pos, "ws.handleDisconnect() handler '%s' must take 1 parameter (ws.Conn), got %d", handlerName, len(sig.Params))
				}
				wsConnType, hasConn := ast.LookupStructType("Conn")
				if !hasConn {
					return 0, c.errAt(e.Pos, "ws.Conn type not registered (internal error)")
				}
				if sig.Params[0] != wsConnType {
					return 0, c.errAt(e.Pos, "ws.handleDisconnect() handler '%s' parameter 1 must be ws.Conn, got %s", handlerName, typeName(sig.Params[0]))
				}
				if sig.ReturnType != ast.TypeVoid {
					return 0, c.errAt(e.Pos, "ws.handleDisconnect() handler '%s' must return void", handlerName)
				}
				return ast.TypeVoid, nil
			}

			// Special case: ws.connect(url) -> ws.Conn
			if e.Module == "ws" && e.Name == "connect" {
				wsConnType, ok := ast.LookupStructType("Conn")
				if !ok {
					return 0, c.errAt(e.Pos, "ws.Conn type not registered (internal error)")
				}
				if len(e.Args) != 1 {
					return 0, c.errAt(e.Pos, "ws.connect() takes exactly 1 argument, got %d", len(e.Args))
				}
				argType, err := c.checkExpr(e.Args[0])
				if err != nil {
					return 0, err
				}
				if argType != ast.TypeString {
					return 0, c.errAt(e.Pos, "ws.connect() argument must be string, got %s", typeName(argType))
				}
				e.ResolvedType = wsConnType
				return wsConnType, nil
			}

			// Special case: ws.send(conn, msg)
			if e.Module == "ws" && e.Name == "send" {
				if len(e.Args) != 2 {
					return 0, c.errAt(e.Pos, "ws.send() takes exactly 2 arguments, got %d", len(e.Args))
				}
				wsConnType, ok := ast.LookupStructType("Conn")
				if !ok {
					return 0, c.errAt(e.Pos, "ws.Conn type not registered (internal error)")
				}
				arg0Type, err := c.checkExpr(e.Args[0])
				if err != nil {
					return 0, err
				}
				if arg0Type != wsConnType {
					return 0, c.errAt(e.Pos, "ws.send() argument 1 must be ws.Conn, got %s", typeName(arg0Type))
				}
				arg1Type, err := c.checkExpr(e.Args[1])
				if err != nil {
					return 0, err
				}
				if arg1Type != ast.TypeString {
					return 0, c.errAt(e.Pos, "ws.send() argument 2 must be string, got %s", typeName(arg1Type))
				}
				return ast.TypeVoid, nil
			}

			// Special case: ws.receive(conn) -> string
			if e.Module == "ws" && e.Name == "receive" {
				if len(e.Args) != 1 {
					return 0, c.errAt(e.Pos, "ws.receive() takes exactly 1 argument, got %d", len(e.Args))
				}
				wsConnType, ok := ast.LookupStructType("Conn")
				if !ok {
					return 0, c.errAt(e.Pos, "ws.Conn type not registered (internal error)")
				}
				argType, err := c.checkExpr(e.Args[0])
				if err != nil {
					return 0, err
				}
				if argType != wsConnType {
					return 0, c.errAt(e.Pos, "ws.receive() argument must be ws.Conn, got %s", typeName(argType))
				}
				return ast.TypeString, nil
			}

			// Special case: ws.close(conn)
			if e.Module == "ws" && e.Name == "close" {
				if len(e.Args) != 1 {
					return 0, c.errAt(e.Pos, "ws.close() takes exactly 1 argument, got %d", len(e.Args))
				}
				wsConnType, ok := ast.LookupStructType("Conn")
				if !ok {
					return 0, c.errAt(e.Pos, "ws.Conn type not registered (internal error)")
				}
				argType, err := c.checkExpr(e.Args[0])
				if err != nil {
					return 0, err
				}
				if argType != wsConnType {
					return 0, c.errAt(e.Pos, "ws.close() argument must be ws.Conn, got %s", typeName(argType))
				}
				return ast.TypeVoid, nil
			}

			// Special case: time.setTimeout(fn, ms) and time.setInterval(fn, ms)
			if e.Module == "time" && (e.Name == "setTimeout" || e.Name == "setInterval") {
				if len(e.Args) != 2 {
					return 0, c.errAt(e.Pos, "time.%s() takes exactly 2 arguments, got %d", e.Name, len(e.Args))
				}
				// Arg 0: function name (Ident or CallExpr with 0 args)
				var handlerName string
				switch h := e.Args[0].(type) {
				case *ast.Ident:
					handlerName = h.Name
				case *ast.CallExpr:
					if len(h.Args) != 0 {
						return 0, c.errAt(e.Pos, "time.%s() callback must take no arguments", e.Name)
					}
					handlerName = h.Name
					if h.Module != "" && c.userModules[h.Module] {
						handlerName = h.Module + "_" + h.Name
					}
				default:
					return 0, c.errAt(e.Pos, "time.%s() argument 1 must be a function name", e.Name)
				}
				sig, ok := c.funcs[handlerName]
				if !ok {
					return 0, c.errAt(e.Pos, "time.%s() callback '%s' is not a defined function", e.Name, handlerName)
				}
				if len(sig.Params) != 0 {
					return 0, c.errAt(e.Pos, "time.%s() callback '%s' must take no parameters", e.Name, handlerName)
				}
				// Arg 1: milliseconds (int)
				msType, err := c.checkExpr(e.Args[1])
				if err != nil {
					return 0, err
				}
				if msType != ast.TypeInt {
					return 0, c.errAt(e.Pos, "time.%s() argument 2 must be int, got %s", e.Name, typeName(msType))
				}
				if e.Name == "setInterval" {
					return ast.TypeInt, nil
				}
				return ast.TypeVoid, nil
			}

			// os.exec returning ExecResult
			if e.Module == "os" && e.Name == "exec" {
				execResultType, ok := ast.LookupStructType("ExecResult")
				if !ok {
					return 0, c.errAt(e.Pos, "ExecResult type not registered (internal error)")
				}
				if len(e.Args) != 1 {
					return 0, c.errAt(e.Pos, "os.exec() takes exactly 1 argument, got %d", len(e.Args))
				}
				argType, err := c.checkExpr(e.Args[0])
				if err != nil {
					return 0, err
				}
				if argType != ast.TypeString {
					return 0, c.errAt(e.Pos, "os.exec() argument must be string, got %s", typeName(argType))
				}
				e.ResolvedType = execResultType
				return execResultType, nil
			}

			// HTTP client functions returning HttpResponse
			if e.Module == "http" && (e.Name == "get" || e.Name == "post" || e.Name == "put" || e.Name == "patch" || e.Name == "delete" || e.Name == "request" || e.Name == "postForm") {
				httpRespType, ok := ast.LookupStructType("HttpResponse")
				if !ok {
					return 0, c.errAt(e.Pos, "HttpResponse type not registered (internal error)")
				}
				switch e.Name {
				case "get":
					if len(e.Args) < 1 || len(e.Args) > 2 {
						return 0, c.errAt(e.Pos, "http.get() takes 1-2 arguments, got %d", len(e.Args))
					}
					for i, arg := range e.Args {
						argType, err := c.checkExpr(arg)
						if err != nil {
							return 0, err
						}
						if argType != ast.TypeString {
							return 0, c.errAt(e.Pos, "http.get() argument %d must be string, got %s", i+1, typeName(argType))
						}
					}
				case "post", "put", "patch":
					if len(e.Args) < 2 || len(e.Args) > 3 {
						return 0, c.errAt(e.Pos, "http.%s() takes 2-3 arguments, got %d", e.Name, len(e.Args))
					}
					for i, arg := range e.Args {
						argType, err := c.checkExpr(arg)
						if err != nil {
							return 0, err
						}
						if argType != ast.TypeString {
							return 0, c.errAt(e.Pos, "http.%s() argument %d must be string, got %s", e.Name, i+1, typeName(argType))
						}
					}
				case "delete":
					if len(e.Args) < 1 || len(e.Args) > 2 {
						return 0, c.errAt(e.Pos, "http.delete() takes 1-2 arguments, got %d", len(e.Args))
					}
					for i, arg := range e.Args {
						argType, err := c.checkExpr(arg)
						if err != nil {
							return 0, err
						}
						if argType != ast.TypeString {
							return 0, c.errAt(e.Pos, "http.delete() argument %d must be string, got %s", i+1, typeName(argType))
						}
					}
				case "request":
					if len(e.Args) != 4 {
						return 0, c.errAt(e.Pos, "http.request() takes exactly 4 arguments, got %d", len(e.Args))
					}
					for i, arg := range e.Args {
						argType, err := c.checkExpr(arg)
						if err != nil {
							return 0, err
						}
						if argType != ast.TypeString {
							return 0, c.errAt(e.Pos, "http.request() argument %d must be string, got %s", i+1, typeName(argType))
						}
					}
				case "postForm":
					if len(e.Args) < 2 || len(e.Args) > 3 {
						return 0, c.errAt(e.Pos, "http.postForm() takes 2-3 arguments, got %d", len(e.Args))
					}
					for i, arg := range e.Args {
						argType, err := c.checkExpr(arg)
						if err != nil {
							return 0, err
						}
						if argType != ast.TypeString {
							return 0, c.errAt(e.Pos, "http.postForm() argument %d must be string, got %s", i+1, typeName(argType))
						}
					}
				}
				e.ResolvedType = httpRespType
				return httpRespType, nil
			}

			// HTTP header builder function returning string
			if e.Module == "http" && e.Name == "header" {
				if len(e.Args) != 3 {
					return 0, c.errAt(e.Pos, "http.header() takes exactly 3 arguments, got %d", len(e.Args))
				}
				for i, arg := range e.Args {
					argType, err := c.checkExpr(arg)
					if err != nil {
						return 0, err
					}
					if argType != ast.TypeString {
						return 0, c.errAt(e.Pos, "http.header() argument %d must be string, got %s", i+1, typeName(argType))
					}
				}
				return ast.TypeString, nil
			}

			// Special case: http.listen(port) or http.listen(port, workers)
			if e.Module == "http" && e.Name == "listen" {
				if len(e.Args) < 1 || len(e.Args) > 2 {
					return 0, c.errAt(e.Pos, "http.listen() takes 1-2 arguments, got %d", len(e.Args))
				}
				for i, arg := range e.Args {
					argType, err := c.checkExpr(arg)
					if err != nil {
						return 0, err
					}
					if argType != ast.TypeInt {
						return 0, c.errAt(e.Pos, "http.listen() argument %d must be int, got %s", i+1, typeName(argType))
					}
				}
				return ast.TypeVoid, nil
			}

			// HTTP form builder functions returning string
			if e.Module == "http" && (e.Name == "formNew" || e.Name == "formField" || e.Name == "formFile") {
				switch e.Name {
				case "formNew":
					if len(e.Args) != 0 {
						return 0, c.errAt(e.Pos, "http.formNew() takes no arguments, got %d", len(e.Args))
					}
				case "formField":
					if len(e.Args) != 3 {
						return 0, c.errAt(e.Pos, "http.formField() takes exactly 3 arguments, got %d", len(e.Args))
					}
					for i, arg := range e.Args {
						argType, err := c.checkExpr(arg)
						if err != nil {
							return 0, err
						}
						if argType != ast.TypeString {
							return 0, c.errAt(e.Pos, "http.formField() argument %d must be string, got %s", i+1, typeName(argType))
						}
					}
				case "formFile":
					if len(e.Args) != 3 {
						return 0, c.errAt(e.Pos, "http.formFile() takes exactly 3 arguments, got %d", len(e.Args))
					}
					for i, arg := range e.Args {
						argType, err := c.checkExpr(arg)
						if err != nil {
							return 0, err
						}
						if argType != ast.TypeString {
							return 0, c.errAt(e.Pos, "http.formFile() argument %d must be string, got %s", i+1, typeName(argType))
						}
					}
				}
				return ast.TypeString, nil
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
