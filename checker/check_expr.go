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
				if argType != ast.TypeInt && argType != ast.TypeLong && argType != ast.TypeDouble &&
					argType != ast.TypeString && argType != ast.TypeBool && argType != ast.TypeChar &&
					!ast.IsEnumType(argType) {
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
				if !ast.IsArrayType(argType) && !ast.IsStructType(argType) {
					return 0, c.errAt(e.Pos, "json.stringify() argument must be an array or struct type, got %s", typeName(argType))
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

func (c *Checker) checkArrayMethod(varName string, arrType ast.Type, method string, args []ast.Expr) (ast.Type, error) {
	switch method {
	case "push":
		if len(args) != 1 {
			return 0, fmt.Errorf("%s.push() takes exactly 1 argument, got %d", varName, len(args))
		}
		argType, err := c.checkExpr(args[0])
		if err != nil {
			return 0, err
		}
		elemType := ast.ElementType(arrType)
		if !canAssign(elemType, argType, args[0]) {
			return 0, fmt.Errorf("%s.push() argument must be %s, got %s", varName, typeName(elemType), typeName(argType))
		}
		return ast.TypeVoid, nil
	case "len":
		if len(args) != 0 {
			return 0, fmt.Errorf("%s.len() takes no arguments, got %d", varName, len(args))
		}
		return ast.TypeInt, nil
	case "pop":
		if len(args) != 0 {
			return 0, fmt.Errorf("%s.pop() takes no arguments, got %d", varName, len(args))
		}
		return ast.ElementType(arrType), nil
	case "remove":
		if len(args) != 1 {
			return 0, fmt.Errorf("%s.remove() takes exactly 1 argument, got %d", varName, len(args))
		}
		argType, err := c.checkExpr(args[0])
		if err != nil {
			return 0, err
		}
		if argType != ast.TypeInt {
			return 0, fmt.Errorf("%s.remove() argument must be int, got %s", varName, typeName(argType))
		}
		return ast.TypeVoid, nil
	case "contains":
		if ast.IsStructArrayType(arrType) {
			return 0, fmt.Errorf("contains() is not supported on struct arrays")
		}
		if len(args) != 1 {
			return 0, fmt.Errorf("%s.contains() takes exactly 1 argument, got %d", varName, len(args))
		}
		argType, err := c.checkExpr(args[0])
		if err != nil {
			return 0, err
		}
		elemType := ast.ElementType(arrType)
		if argType != elemType {
			return 0, fmt.Errorf("%s.contains() argument must be %s, got %s", varName, typeName(elemType), typeName(argType))
		}
		return ast.TypeBool, nil
	case "indexOf":
		if ast.IsStructArrayType(arrType) {
			return 0, fmt.Errorf("indexOf() is not supported on struct arrays")
		}
		if len(args) != 1 {
			return 0, fmt.Errorf("%s.indexOf() takes exactly 1 argument, got %d", varName, len(args))
		}
		argType, err := c.checkExpr(args[0])
		if err != nil {
			return 0, err
		}
		elemType := ast.ElementType(arrType)
		if argType != elemType {
			return 0, fmt.Errorf("%s.indexOf() argument must be %s, got %s", varName, typeName(elemType), typeName(argType))
		}
		return ast.TypeInt, nil
	case "reverse":
		if len(args) != 0 {
			return 0, fmt.Errorf("%s.reverse() takes no arguments, got %d", varName, len(args))
		}
		return ast.TypeVoid, nil
	case "sort":
		if ast.IsStructArrayType(arrType) {
			return 0, fmt.Errorf("sort() is not supported on struct arrays")
		}
		if len(args) != 1 {
			return 0, fmt.Errorf("%s.sort() takes exactly 1 argument, got %d", varName, len(args))
		}
		argType, err := c.checkExpr(args[0])
		if err != nil {
			return 0, err
		}
		if argType != ast.TypeString {
			return 0, fmt.Errorf("%s.sort() argument must be string, got %s", varName, typeName(argType))
		}
		if arrType == ast.TypeArrayBool {
			return 0, fmt.Errorf("sort() is not supported on bool arrays")
		}
		return ast.TypeVoid, nil
	default:
		return 0, fmt.Errorf("undefined method '%s' on array type %s", method, typeName(arrType))
	}
}

// checkStringMethod validates a method call on a string variable (e.g. s.len(), s.contains("x")).
func (c *Checker) checkStringMethod(varName, method string, args []ast.Expr, pos ast.Pos) (ast.Type, error) {
	switch method {
	case "len":
		if len(args) != 0 {
			return 0, c.errAt(pos, "%s.len() takes no arguments, got %d", varName, len(args))
		}
		return ast.TypeInt, nil
	case "contains", "startsWith", "endsWith":
		if len(args) != 1 {
			return 0, c.errAt(pos, "%s.%s() takes exactly 1 argument, got %d", varName, method, len(args))
		}
		argType, err := c.checkExpr(args[0])
		if err != nil {
			return 0, err
		}
		if argType != ast.TypeString {
			return 0, c.errAt(pos, "%s.%s() argument must be string, got %s", varName, method, typeName(argType))
		}
		return ast.TypeBool, nil
	case "indexOf":
		if len(args) != 1 {
			return 0, c.errAt(pos, "%s.indexOf() takes exactly 1 argument, got %d", varName, len(args))
		}
		argType, err := c.checkExpr(args[0])
		if err != nil {
			return 0, err
		}
		if argType != ast.TypeString {
			return 0, c.errAt(pos, "%s.indexOf() argument must be string, got %s", varName, typeName(argType))
		}
		return ast.TypeInt, nil
	case "toLower", "toUpper", "trim":
		if len(args) != 0 {
			return 0, c.errAt(pos, "%s.%s() takes no arguments, got %d", varName, method, len(args))
		}
		return ast.TypeString, nil
	case "split":
		if len(args) != 1 {
			return 0, c.errAt(pos, "%s.split() takes exactly 1 argument, got %d", varName, len(args))
		}
		argType, err := c.checkExpr(args[0])
		if err != nil {
			return 0, err
		}
		if argType != ast.TypeString {
			return 0, c.errAt(pos, "%s.split() argument must be string, got %s", varName, typeName(argType))
		}
		return ast.TypeArrayString, nil
	case "substring":
		if len(args) != 2 {
			return 0, c.errAt(pos, "%s.substring() takes exactly 2 arguments, got %d", varName, len(args))
		}
		for i, arg := range args {
			argType, err := c.checkExpr(arg)
			if err != nil {
				return 0, err
			}
			if argType != ast.TypeInt {
				return 0, c.errAt(pos, "%s.substring() argument %d must be int, got %s", varName, i+1, typeName(argType))
			}
		}
		return ast.TypeString, nil
	case "replace":
		if len(args) != 2 {
			return 0, c.errAt(pos, "%s.replace() takes exactly 2 arguments, got %d", varName, len(args))
		}
		for i, arg := range args {
			argType, err := c.checkExpr(arg)
			if err != nil {
				return 0, err
			}
			if argType != ast.TypeString {
				return 0, c.errAt(pos, "%s.replace() argument %d must be string, got %s", varName, i+1, typeName(argType))
			}
		}
		return ast.TypeString, nil
	case "charAt":
		if len(args) != 1 {
			return 0, c.errAt(pos, "%s.charAt() takes exactly 1 argument, got %d", varName, len(args))
		}
		argType, err := c.checkExpr(args[0])
		if err != nil {
			return 0, err
		}
		if argType != ast.TypeInt {
			return 0, c.errAt(pos, "%s.charAt() argument must be int, got %s", varName, typeName(argType))
		}
		return ast.TypeChar, nil
	case "isAlphanumeric", "isAlpha", "isDigit", "isNumeric", "isWhitespace", "isEmpty":
		if len(args) != 0 {
			return 0, c.errAt(pos, "%s.%s() takes no arguments, got %d", varName, method, len(args))
		}
		return ast.TypeBool, nil
	default:
		return 0, c.errAt(pos, "undefined method '%s' on string type", method)
	}
}

// checkMapMethod validates a method call on a map variable.
func (c *Checker) checkMapMethod(varName string, mapType ast.Type, method string, args []ast.Expr, pos ast.Pos) (ast.Type, error) {
	keyType := ast.MapKeyType(mapType)
	valType := ast.MapValueType(mapType)

	switch method {
	case "set":
		if len(args) != 2 {
			return 0, c.errAt(pos, "%s.set() takes exactly 2 arguments, got %d", varName, len(args))
		}
		argKeyType, err := c.checkExpr(args[0])
		if err != nil {
			return 0, err
		}
		if argKeyType != keyType {
			return 0, c.errAt(pos, "%s.set() key must be %s, got %s", varName, typeName(keyType), typeName(argKeyType))
		}
		argValType, err := c.checkExpr(args[1])
		if err != nil {
			return 0, err
		}
		if argValType != valType {
			return 0, c.errAt(pos, "%s.set() value must be %s, got %s", varName, typeName(valType), typeName(argValType))
		}
		return ast.TypeVoid, nil
	case "get":
		if len(args) != 1 {
			return 0, c.errAt(pos, "%s.get() takes exactly 1 argument, got %d", varName, len(args))
		}
		argType, err := c.checkExpr(args[0])
		if err != nil {
			return 0, err
		}
		if argType != keyType {
			return 0, c.errAt(pos, "%s.get() key must be %s, got %s", varName, typeName(keyType), typeName(argType))
		}
		return valType, nil
	case "has":
		if len(args) != 1 {
			return 0, c.errAt(pos, "%s.has() takes exactly 1 argument, got %d", varName, len(args))
		}
		argType, err := c.checkExpr(args[0])
		if err != nil {
			return 0, err
		}
		if argType != keyType {
			return 0, c.errAt(pos, "%s.has() key must be %s, got %s", varName, typeName(keyType), typeName(argType))
		}
		return ast.TypeBool, nil
	case "remove":
		if len(args) != 1 {
			return 0, c.errAt(pos, "%s.remove() takes exactly 1 argument, got %d", varName, len(args))
		}
		argType, err := c.checkExpr(args[0])
		if err != nil {
			return 0, err
		}
		if argType != keyType {
			return 0, c.errAt(pos, "%s.remove() key must be %s, got %s", varName, typeName(keyType), typeName(argType))
		}
		return ast.TypeVoid, nil
	case "len":
		if len(args) != 0 {
			return 0, c.errAt(pos, "%s.len() takes no arguments, got %d", varName, len(args))
		}
		return ast.TypeInt, nil
	case "clear":
		if len(args) != 0 {
			return 0, c.errAt(pos, "%s.clear() takes no arguments, got %d", varName, len(args))
		}
		return ast.TypeVoid, nil
	case "keys":
		if len(args) != 0 {
			return 0, c.errAt(pos, "%s.keys() takes no arguments, got %d", varName, len(args))
		}
		return ast.ArrayTypeOf(keyType), nil
	case "values":
		if len(args) != 0 {
			return 0, c.errAt(pos, "%s.values() takes no arguments, got %d", varName, len(args))
		}
		return ast.ArrayTypeOf(valType), nil
	default:
		return 0, c.errAt(pos, "undefined method '%s' on map type", method)
	}
}

// checkStringBuilderMethod validates a method call on a StringBuilder variable.
func (c *Checker) checkStringBuilderMethod(varName, method string, args []ast.Expr, pos ast.Pos) (ast.Type, error) {
	switch method {
	case "append":
		if len(args) != 1 {
			return 0, c.errAt(pos, "%s.append() takes exactly 1 argument, got %d", varName, len(args))
		}
		argType, err := c.checkExpr(args[0])
		if err != nil {
			return 0, err
		}
		switch argType {
		case ast.TypeString, ast.TypeInt, ast.TypeLong, ast.TypeDouble, ast.TypeBool, ast.TypeChar:
			// all valid
		default:
			return 0, c.errAt(pos, "%s.append() argument must be string, int, long, double, bool, or char, got %s", varName, typeName(argType))
		}
		return ast.TypeVoid, nil
	case "toString":
		if len(args) != 0 {
			return 0, c.errAt(pos, "%s.toString() takes no arguments, got %d", varName, len(args))
		}
		return ast.TypeString, nil
	case "len":
		if len(args) != 0 {
			return 0, c.errAt(pos, "%s.len() takes no arguments, got %d", varName, len(args))
		}
		return ast.TypeInt, nil
	case "clear":
		if len(args) != 0 {
			return 0, c.errAt(pos, "%s.clear() takes no arguments, got %d", varName, len(args))
		}
		return ast.TypeVoid, nil
	default:
		return 0, c.errAt(pos, "undefined method '%s' on StringBuilder type", method)
	}
}

// extractNullCheck detects null comparison patterns in conditions.
// Returns (varName, isNotNull) where isNotNull=true means "x != null".
func extractNullCheck(cond ast.Expr) (string, bool) {
	binExpr, ok := cond.(*ast.BinaryExpr)
	if !ok {
		return "", false
	}
	if binExpr.Op != ast.BinEq && binExpr.Op != ast.BinNeq {
		return "", false
	}
	// Check x != null or x == null
	if ident, ok := binExpr.Left.(*ast.Ident); ok {
		if _, ok := binExpr.Right.(*ast.NullLit); ok {
			return ident.Name, binExpr.Op == ast.BinNeq
		}
	}
	// Check null != x or null == x
	if _, ok := binExpr.Left.(*ast.NullLit); ok {
		if ident, ok := binExpr.Right.(*ast.Ident); ok {
			return ident.Name, binExpr.Op == ast.BinNeq
		}
	}
	return "", false
}

func isValidFieldType(t ast.Type) bool {
	if ast.IsOptionalType(t) {
		return isValidFieldType(ast.OptionalInnerType(t))
	}
	if ast.IsRefType(t) {
		return ast.IsStructType(ast.RefInnerType(t))
	}
	switch t {
	case ast.TypeInt, ast.TypeBool, ast.TypeString, ast.TypeLong, ast.TypeDouble, ast.TypeChar:
		return true
	default:
		return ast.IsStructType(t) || ast.IsEnumType(t) || ast.IsMapType(t)
	}
}

func widerNumericType(a, b ast.Type) ast.Type {
	rank := map[ast.Type]int{ast.TypeChar: 0, ast.TypeInt: 1, ast.TypeLong: 2, ast.TypeDouble: 3}
	if rank[a] >= rank[b] {
		return a
	}
	return b
}

func isNumericType(t ast.Type) bool {
	return t == ast.TypeChar || t == ast.TypeInt || t == ast.TypeLong || t == ast.TypeDouble
}

func isPrimitiveType(t ast.Type) bool {
	return t == ast.TypeInt || t == ast.TypeBool || t == ast.TypeString ||
		t == ast.TypeChar || t == ast.TypeLong || t == ast.TypeDouble || ast.IsEnumType(t)
}

// isIntLiteral returns true if the expression is an integer literal.
func isIntLiteral(expr ast.Expr) bool {
	_, ok := expr.(*ast.IntLit)
	return ok
}

// isCharLiteral returns true if the expression is a char literal.
func isCharLiteral(expr ast.Expr) bool {
	_, ok := expr.(*ast.CharLit)
	return ok
}

// validateAnnotations checks that annotations are valid and compatible with the type.
func validateAnnotations(annotations []string, typ ast.Type, name string) error {
	if len(annotations) == 0 {
		return nil
	}

	// Validate each annotation is known
	validAnnotations := map[string]bool{
		ast.AnnotOwned:       true,
		ast.AnnotRegion:      true,
		ast.AnnotNoEscape:    true,
		ast.AnnotDebugCycles: true,
	}
	memAnnotCount := 0
	for _, a := range annotations {
		if !validAnnotations[a] {
			return fmt.Errorf("unknown annotation '#[%s]' on '%s'", a, name)
		}
		if a == ast.AnnotOwned || a == ast.AnnotRegion || a == ast.AnnotNoEscape {
			memAnnotCount++
		}
	}

	// Mutual exclusivity: only one of owned/region/noEscape
	if memAnnotCount > 1 {
		return fmt.Errorf("annotations #[owned], #[region], and #[noEscape] are mutually exclusive on '%s'", name)
	}

	// Annotations only on heap types
	if !ast.IsHeapType(typ) {
		return fmt.Errorf("annotations are only allowed on heap types (string, arrays, channels), got %s on '%s'", typeName(typ), name)
	}

	return nil
}

// canAssign checks if an expression of exprType can be assigned to a target of targetType,
// allowing implicit widening of int literals to long/double and float literals to double.
func canAssign(targetType, exprType ast.Type, expr ast.Expr) bool {
	if exprType == targetType {
		return true
	}
	// null -> any optional type
	if exprType == ast.TypeNull && ast.IsOptionalType(targetType) {
		return true
	}
	// T -> T? (wrapping non-optional into optional)
	if ast.IsOptionalType(targetType) {
		inner := ast.OptionalInnerType(targetType)
		if exprType == inner {
			return true
		}
		// Allow widening into optional (e.g. int literal -> long?)
		if canAssign(inner, exprType, expr) {
			return true
		}
	}
	// int literal -> long or double
	if isIntLiteral(expr) && (targetType == ast.TypeLong || targetType == ast.TypeDouble) {
		return true
	}
	// char literal -> int, long, or double
	if isCharLiteral(expr) && (targetType == ast.TypeInt || targetType == ast.TypeLong || targetType == ast.TypeDouble) {
		return true
	}
	// weak<T> accepts T (wrapping a strong ref into a weak ref)
	if ast.IsWeakType(targetType) && exprType == ast.WeakInnerType(targetType) {
		return true
	}
	// T -> &T (implicit address-of: pass struct value to ref param)
	if ast.IsRefType(targetType) && exprType == ast.RefInnerType(targetType) {
		return true
	}
	// float literal -> double (already types as double, but keep for clarity)
	return false
}

func typeName(t ast.Type) string {
	switch t {
	case ast.TypeNull:
		return "null"
	case ast.TypeInt:
		return "int"
	case ast.TypeBool:
		return "bool"
	case ast.TypeString:
		return "string"
	case ast.TypeVoid:
		return "void"
	case ast.TypeLong:
		return "long"
	case ast.TypeDouble:
		return "double"
	case ast.TypeArrayInt:
		return "int[]"
	case ast.TypeArrayBool:
		return "bool[]"
	case ast.TypeArrayString:
		return "string[]"
	case ast.TypeArrayLong:
		return "long[]"
	case ast.TypeArrayDouble:
		return "double[]"
	case ast.TypeChar:
		return "char"
	case ast.TypeArrayChar:
		return "char[]"
	case ast.TypeStringBuilder:
		return "StringBuilder"
	default:
		if ast.IsRefType(t) {
			return "&" + typeName(ast.RefInnerType(t))
		}
		if ast.IsOptionalType(t) {
			return typeName(ast.OptionalInnerType(t)) + "?"
		}
		if ast.IsStructArrayType(t) {
			elemType := ast.ElementType(t)
			if ast.IsEnumType(elemType) {
				return ast.EnumName(elemType) + "[]"
			}
			return ast.StructName(elemType) + "[]"
		}
		if ast.IsStructType(t) {
			return ast.StructName(t)
		}
		if ast.IsChanType(t) {
			return "chan " + typeName(ast.ChanElemType(t))
		}
		if ast.IsTaskType(t) {
			return "task " + typeName(ast.TaskReturnType(t))
		}
		if ast.IsFuncType(t) {
			params := ast.FuncTypeParams(t)
			ret := ast.FuncTypeReturn(t)
			s := "fn("
			for i, p := range params {
				if i > 0 {
					s += ", "
				}
				s += typeName(p)
			}
			s += "): " + typeName(ret)
			return s
		}
		if ast.IsWeakType(t) {
			return "weak<" + typeName(ast.WeakInnerType(t)) + ">"
		}
		if ast.IsEnumType(t) {
			return ast.EnumName(t)
		}
		if ast.IsMapType(t) {
			return "map[" + typeName(ast.MapKeyType(t)) + ", " + typeName(ast.MapValueType(t)) + "]"
		}
		return "unknown"
	}
}
