package checker

import (
	"fmt"

	"github.com/ensamuel7/dex/ast"
)

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
	case "isAlphanumeric", "isAlpha", "isDigit", "isNumeric", "isWhitespace", "isEmpty",
		"containsUppercase", "containsLowercase", "containsDigit":
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
