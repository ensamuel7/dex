package checker

import (
	"fmt"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/stdlib"
)

type funcSig struct {
	Params     []ast.Type
	ReturnType ast.Type
	IsPrivate  bool
}

type Checker struct {
	scopes      []map[string]ast.Type
	constScopes []map[string]bool
	funcs       map[string]funcSig
	imports     map[string]*stdlib.Module
	loopDepth   int
}

func New() *Checker {
	return &Checker{
		funcs:   make(map[string]funcSig),
		imports: make(map[string]*stdlib.Module),
	}
}

func (c *Checker) Check(program *ast.Program) error {
	// Validate imports
	for _, imp := range program.Imports {
		mod := stdlib.Lookup(imp.Path)
		if mod == nil {
			return fmt.Errorf("unknown import '%s'", imp.Path)
		}
		c.imports[imp.Path] = mod
	}

	// Validate struct definitions
	seen := map[string]bool{}
	for _, sd := range program.Structs {
		if seen[sd.Name] {
			return fmt.Errorf("duplicate struct type '%s'", sd.Name)
		}
		seen[sd.Name] = true
		fieldNames := map[string]bool{}
		for _, f := range sd.Fields {
			if fieldNames[f.Name] {
				return fmt.Errorf("duplicate field '%s' in struct '%s'", f.Name, sd.Name)
			}
			fieldNames[f.Name] = true
			if !isValidFieldType(f.Type) {
				return fmt.Errorf("invalid type for field '%s' in struct '%s'", f.Name, sd.Name)
			}
		}
	}

	// First pass: register all function signatures
	for _, fn := range program.Functions {
		var paramTypes []ast.Type
		for _, p := range fn.Params {
			paramTypes = append(paramTypes, p.Type)
		}
		c.funcs[fn.Name] = funcSig{
			Params:     paramTypes,
			ReturnType: fn.ReturnType,
			IsPrivate:  fn.IsPrivate,
		}
	}

	// Second pass: check function bodies
	for _, fn := range program.Functions {
		c.pushScope()

		for _, p := range fn.Params {
			c.define(p.Name, p.Type)
		}

		for _, stmt := range fn.Body {
			if err := c.checkStmt(stmt, fn.ReturnType); err != nil {
				return err
			}
		}

		c.popScope()
	}

	return nil
}

func (c *Checker) checkStmt(stmt ast.Stmt, returnType ast.Type) error {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		// Pre-annotate db.col() with the expected return type
		if call, ok := s.Value.(*ast.CallExpr); ok && call.Module == "db" && call.Name == "col" {
			if s.Type == ast.TypeInferred {
				return fmt.Errorf("db.col() requires an explicit type annotation (e.g., let x: int = db.col(...))")
			}
			call.ResolvedType = s.Type
		}

		// Handle type inference
		if s.Type == ast.TypeInferred {
			// Cannot infer type from empty array literal
			if arrLit, ok := s.Value.(*ast.ArrayLitExpr); ok && len(arrLit.Elems) == 0 {
				return fmt.Errorf("cannot infer type of empty array literal; use explicit type annotation")
			}
			exprType, err := c.checkExpr(s.Value)
			if err != nil {
				return err
			}
			s.Type = exprType
			c.define(s.Name, s.Type)
			if s.IsConst {
				c.defineConst(s.Name)
			}
			return nil
		}

		// Handle empty array literal: infer element type from declared type
		if arrLit, ok := s.Value.(*ast.ArrayLitExpr); ok && len(arrLit.Elems) == 0 {
			if !ast.IsArrayType(s.Type) {
				return fmt.Errorf("cannot assign empty array literal to non-array type %s", typeName(s.Type))
			}
			arrLit.ElemType = ast.ElementType(s.Type)
			c.define(s.Name, s.Type)
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
			return fmt.Errorf("type mismatch in let: expected %s, got %s", typeName(s.Type), typeName(exprType))
		}
		c.define(s.Name, s.Type)
		if s.IsConst {
			c.defineConst(s.Name)
		}
		return nil

	case *ast.ReturnStmt:
		// Pre-annotate db.col() with the expected return type
		if call, ok := s.Value.(*ast.CallExpr); ok && call.Module == "db" && call.Name == "col" {
			call.ResolvedType = returnType
		}
		exprType, err := c.checkExpr(s.Value)
		if err != nil {
			return err
		}
		if exprType != returnType {
			return fmt.Errorf("return type mismatch: expected %s, got %s", typeName(returnType), typeName(exprType))
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
			return fmt.Errorf("if condition must be bool, got %s", typeName(condType))
		}
		c.pushScope()
		for _, stmt := range s.Then {
			if err := c.checkStmt(stmt, returnType); err != nil {
				return err
			}
		}
		c.popScope()
		if s.Else != nil {
			c.pushScope()
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
			return fmt.Errorf("while condition must be bool, got %s", typeName(condType))
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
			return fmt.Errorf("for condition must be bool, got %s", typeName(condType))
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
			return fmt.Errorf("foreach requires an array type, got %s", typeName(iterType))
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
			return fmt.Errorf("'break' outside of loop")
		}
		return nil

	case *ast.ContinueStmt:
		if c.loopDepth == 0 {
			return fmt.Errorf("'continue' outside of loop")
		}
		return nil

	case *ast.IncrementStmt:
		varType, ok := c.resolve(s.Name)
		if !ok {
			return fmt.Errorf("undefined variable '%s'", s.Name)
		}
		if c.isConst(s.Name) {
			return fmt.Errorf("cannot modify const variable '%s'", s.Name)
		}
		if !isNumericType(varType) {
			return fmt.Errorf("'++' requires numeric variable, got %s", typeName(varType))
		}
		return nil

	case *ast.DecrementStmt:
		varType, ok := c.resolve(s.Name)
		if !ok {
			return fmt.Errorf("undefined variable '%s'", s.Name)
		}
		if c.isConst(s.Name) {
			return fmt.Errorf("cannot modify const variable '%s'", s.Name)
		}
		if !isNumericType(varType) {
			return fmt.Errorf("'--' requires numeric variable, got %s", typeName(varType))
		}
		return nil

	case *ast.CompoundAssignStmt:
		varType, ok := c.resolve(s.Name)
		if !ok {
			return fmt.Errorf("undefined variable '%s'", s.Name)
		}
		if c.isConst(s.Name) {
			return fmt.Errorf("cannot modify const variable '%s'", s.Name)
		}
		if !isNumericType(varType) {
			return fmt.Errorf("compound assignment requires numeric variable, got %s", typeName(varType))
		}
		valType, err := c.checkExpr(s.Value)
		if err != nil {
			return err
		}
		if !isNumericType(valType) {
			return fmt.Errorf("compound assignment value must be numeric, got %s", typeName(valType))
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
			return fmt.Errorf("undefined variable '%s'", s.Name)
		}
		if c.isConst(s.Name) {
			return fmt.Errorf("cannot reassign const variable '%s'", s.Name)
		}
		// Pre-annotate db.col() with the expected return type
		if call, ok := s.Value.(*ast.CallExpr); ok && call.Module == "db" && call.Name == "col" {
			call.ResolvedType = varType
		}
		exprType, err := c.checkExpr(s.Value)
		if err != nil {
			return err
		}
		if exprType != varType {
			return fmt.Errorf("type mismatch in assignment: expected %s, got %s", typeName(varType), typeName(exprType))
		}
		return nil

	case *ast.IndexAssignStmt:
		arrType, err := c.checkExpr(s.Array)
		if err != nil {
			return err
		}
		if !ast.IsArrayType(arrType) {
			return fmt.Errorf("index assignment requires an array type, got %s", typeName(arrType))
		}
		idxType, err := c.checkExpr(s.Index)
		if err != nil {
			return err
		}
		if idxType != ast.TypeInt {
			return fmt.Errorf("array index must be int, got %s", typeName(idxType))
		}
		valType, err := c.checkExpr(s.Value)
		if err != nil {
			return err
		}
		elemType := ast.ElementType(arrType)
		if valType != elemType {
			return fmt.Errorf("type mismatch in index assignment: expected %s, got %s", typeName(elemType), typeName(valType))
		}
		return nil

	case *ast.FieldAssignStmt:
		objType, err := c.checkExpr(s.Object)
		if err != nil {
			return err
		}
		if !ast.IsStructType(objType) {
			return fmt.Errorf("field assignment requires a struct type, got %s", typeName(objType))
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
			return fmt.Errorf("struct '%s' has no field '%s'", def.Name, s.Field)
		}
		valType, err := c.checkExpr(s.Value)
		if err != nil {
			return err
		}
		if valType != fieldType {
			return fmt.Errorf("type mismatch in field assignment: expected %s, got %s", typeName(fieldType), typeName(valType))
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
				return fmt.Errorf("send target must be a channel, got %s", typeName(chanType))
			}
			valType, err := c.checkExpr(s.Value)
			if err != nil {
				return err
			}
			if valType != ast.ChanElemType(chanType) {
				return fmt.Errorf("send type mismatch: channel expects %s, got %s", typeName(ast.ChanElemType(chanType)), typeName(valType))
			}
		} else {
			// send(value) — inside a spawn block, type checked via SpawnExpr inference
			_, err := c.checkExpr(s.Value)
			if err != nil {
				return err
			}
		}
		return nil

	default:
		return fmt.Errorf("unknown statement type")
	}
}

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

	case *ast.Ident:
		typ, ok := c.resolve(e.Name)
		if !ok {
			// Check if it's a function reference
			if sig, found := c.funcs[e.Name]; found {
				return ast.FuncTypeOf(sig.Params, sig.ReturnType), nil
			}
			return 0, fmt.Errorf("undefined variable '%s'", e.Name)
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
			return 0, fmt.Errorf("'+' requires matching numeric or string operands")

		case ast.BinSub, ast.BinMul, ast.BinDiv:
			if !isNumericType(leftType) || !isNumericType(rightType) {
				return 0, fmt.Errorf("arithmetic operators require numeric operands")
			}
			if leftType != rightType {
				e.LeftType = leftType
				e.RightType = rightType
				e.HasMixedTypes = true
			}
			return widerNumericType(leftType, rightType), nil

		case ast.BinMod:
			if (leftType != ast.TypeInt && leftType != ast.TypeLong) ||
				(rightType != ast.TypeInt && rightType != ast.TypeLong) {
				return 0, fmt.Errorf("'%%' requires int or long operands")
			}
			if leftType != rightType {
				e.LeftType = leftType
				e.RightType = rightType
				e.HasMixedTypes = true
			}
			return widerNumericType(leftType, rightType), nil

		case ast.BinEq, ast.BinNeq:
			if leftType == rightType {
				return ast.TypeBool, nil
			}
			if isNumericType(leftType) && isNumericType(rightType) {
				e.LeftType = leftType
				e.RightType = rightType
				e.HasMixedTypes = true
				return ast.TypeBool, nil
			}
			return 0, fmt.Errorf("equality operators require matching types, or both numeric")

		case ast.BinStrictEq, ast.BinStrictNeq:
			if leftType != rightType {
				return 0, fmt.Errorf("strict equality requires matching types")
			}
			return ast.TypeBool, nil

		case ast.BinLt, ast.BinGt, ast.BinLte, ast.BinGte:
			if !isNumericType(leftType) || !isNumericType(rightType) {
				return 0, fmt.Errorf("comparison operators require numeric operands")
			}
			if leftType != rightType {
				e.LeftType = leftType
				e.RightType = rightType
				e.HasMixedTypes = true
			}
			return ast.TypeBool, nil

		case ast.BinAnd, ast.BinOr:
			if leftType != ast.TypeBool || rightType != ast.TypeBool {
				return 0, fmt.Errorf("logical operators require bool operands")
			}
			return ast.TypeBool, nil
		}

		return 0, fmt.Errorf("unknown binary operator")

	case *ast.UnaryExpr:
		operandType, err := c.checkExpr(e.Operand)
		if err != nil {
			return 0, err
		}

		switch e.Op {
		case ast.UnaryNeg:
			if !isNumericType(operandType) {
				return 0, fmt.Errorf("unary '-' requires numeric operand")
			}
			return operandType, nil
		case ast.UnaryNot:
			if operandType != ast.TypeBool {
				return 0, fmt.Errorf("unary '!' requires bool operand")
			}
			return ast.TypeBool, nil
		}

		return 0, fmt.Errorf("unknown unary operator")

	case *ast.ArrayLitExpr:
		if len(e.Elems) == 0 {
			// Empty array literal — type must be inferred from context (LetStmt handles this)
			return 0, fmt.Errorf("cannot infer type of empty array literal")
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
				return 0, fmt.Errorf("array elements must be the same type: expected %s, got %s at index %d", typeName(firstType), typeName(t), i)
			}
		}
		e.ElemType = firstType
		return ast.ArrayTypeOf(firstType), nil

	case *ast.IndexExpr:
		arrType, err := c.checkExpr(e.Array)
		if err != nil {
			return 0, err
		}
		if !ast.IsArrayType(arrType) {
			return 0, fmt.Errorf("index operator requires an array type, got %s", typeName(arrType))
		}
		idxType, err := c.checkExpr(e.Index)
		if err != nil {
			return 0, err
		}
		if idxType != ast.TypeInt {
			return 0, fmt.Errorf("array index must be int, got %s", typeName(idxType))
		}
		return ast.ElementType(arrType), nil

	case *ast.StructLitExpr:
		t, ok := ast.LookupStructType(e.Name)
		if !ok {
			return 0, fmt.Errorf("unknown struct type '%s'", e.Name)
		}
		def := ast.GetStructDef(t)
		if len(e.FieldNames) != len(def.Fields) {
			return 0, fmt.Errorf("struct '%s' has %d fields, got %d", e.Name, len(def.Fields), len(e.FieldNames))
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
						return 0, fmt.Errorf("field '%s' of struct '%s': expected %s, got %s", fn, e.Name, typeName(f.Type), typeName(valType))
					}
					found = true
					break
				}
			}
			if !found {
				return 0, fmt.Errorf("struct '%s' has no field '%s'", e.Name, fn)
			}
		}
		return t, nil

	case *ast.FieldAccessExpr:
		objType, err := c.checkExpr(e.Object)
		if err != nil {
			return 0, err
		}
		if !ast.IsStructType(objType) {
			return 0, fmt.Errorf("field access requires a struct type, got %s", typeName(objType))
		}
		def := ast.GetStructDef(objType)
		for _, f := range def.Fields {
			if f.Name == e.Field {
				return f.Type, nil
			}
		}
		return 0, fmt.Errorf("struct '%s' has no field '%s'", def.Name, e.Field)

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
				return 0, fmt.Errorf("spawn expression must be a function call")
			}
			retType, err := c.checkExpr(call)
			if err != nil {
				return 0, err
			}
			e.ReturnType = retType
			return ast.TaskTypeOf(retType), nil
		}
		return 0, fmt.Errorf("spawn expression must have a body or function call")

	case *ast.ChannelExpr:
		return ast.ChanTypeOf(e.ElemType), nil

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
		return 0, fmt.Errorf("receive() requires a channel or task handle, got %s", typeName(srcType))

	case *ast.CallExpr:
		// Qualified call: module.func()
		if e.Module != "" {
			// Check if module is actually a variable (array method call)
			varType, isVar := c.resolve(e.Module)
			if isVar && ast.IsArrayType(varType) {
				return c.checkArrayMethod(e.Module, varType, e.Name, e.Args)
			}

			mod, ok := c.imports[e.Module]
			if !ok {
				return 0, fmt.Errorf("module '%s' is not imported", e.Module)
			}

			// Special case: fmt.print(value) — accepts any primitive type
			if e.Module == "fmt" && e.Name == "print" {
				if len(e.Args) != 1 {
					return 0, fmt.Errorf("fmt.print() takes exactly 1 argument, got %d", len(e.Args))
				}
				argType, err := c.checkExpr(e.Args[0])
				if err != nil {
					return 0, err
				}
				if argType != ast.TypeInt && argType != ast.TypeLong && argType != ast.TypeDouble &&
					argType != ast.TypeString && argType != ast.TypeBool {
					return 0, fmt.Errorf("fmt.print() argument must be a primitive type, got %s", typeName(argType))
				}
				return ast.TypeVoid, nil
			}

			// Special case: json.stringify(array) -> string
			if e.Module == "json" && e.Name == "stringify" {
				if len(e.Args) != 1 {
					return 0, fmt.Errorf("json.stringify() takes exactly 1 argument, got %d", len(e.Args))
				}
				argType, err := c.checkExpr(e.Args[0])
				if err != nil {
					return 0, err
				}
				if !ast.IsArrayType(argType) {
					return 0, fmt.Errorf("json.stringify() argument must be an array type, got %s", typeName(argType))
				}
				return ast.TypeString, nil
			}

			// Special case: json.set(obj, key, value) — polymorphic value type
			if e.Module == "json" && e.Name == "set" {
				if len(e.Args) != 3 {
					return 0, fmt.Errorf("json.set() takes exactly 3 arguments, got %d", len(e.Args))
				}
				objType, err := c.checkExpr(e.Args[0])
				if err != nil {
					return 0, err
				}
				if objType != ast.TypeString {
					return 0, fmt.Errorf("json.set() argument 1 must be string, got %s", typeName(objType))
				}
				keyType, err := c.checkExpr(e.Args[1])
				if err != nil {
					return 0, err
				}
				if keyType != ast.TypeString {
					return 0, fmt.Errorf("json.set() argument 2 must be string, got %s", typeName(keyType))
				}
				valType, err := c.checkExpr(e.Args[2])
				if err != nil {
					return 0, err
				}
				switch valType {
				case ast.TypeString, ast.TypeInt, ast.TypeBool, ast.TypeLong, ast.TypeDouble:
					// all valid
				default:
					return 0, fmt.Errorf("json.set() value must be a primitive type, got %s", typeName(valType))
				}
				return ast.TypeString, nil
			}

			// Special case: db.col(rows, col) — return type resolved from context
			if e.Module == "db" && e.Name == "col" {
				if len(e.Args) != 2 {
					return 0, fmt.Errorf("db.col() takes exactly 2 arguments, got %d", len(e.Args))
				}
				for i, arg := range e.Args {
					argType, err := c.checkExpr(arg)
					if err != nil {
						return 0, err
					}
					if argType != ast.TypeInt {
						return 0, fmt.Errorf("db.col() argument %d must be int, got %s", i+1, typeName(argType))
					}
				}
				return e.ResolvedType, nil
			}

			// Special case: json.set_arr(obj, key, array) -> string
			if e.Module == "json" && e.Name == "set_arr" {
				if len(e.Args) != 3 {
					return 0, fmt.Errorf("json.set_arr() takes exactly 3 arguments, got %d", len(e.Args))
				}
				objType, err := c.checkExpr(e.Args[0])
				if err != nil {
					return 0, err
				}
				if objType != ast.TypeString {
					return 0, fmt.Errorf("json.set_arr() argument 1 must be string, got %s", typeName(objType))
				}
				keyType, err := c.checkExpr(e.Args[1])
				if err != nil {
					return 0, err
				}
				if keyType != ast.TypeString {
					return 0, fmt.Errorf("json.set_arr() argument 2 must be string, got %s", typeName(keyType))
				}
				arrType, err := c.checkExpr(e.Args[2])
				if err != nil {
					return 0, err
				}
				if !ast.IsArrayType(arrType) {
					return 0, fmt.Errorf("json.set_arr() argument 3 must be an array type, got %s", typeName(arrType))
				}
				return ast.TypeString, nil
			}

			// Special case: http.route handler validation (before generic arg check)
			if e.Module == "http" && e.Name == "route" {
				if len(e.Args) != 3 {
					return 0, fmt.Errorf("http.route() takes exactly 3 arguments, got %d", len(e.Args))
				}
				// Check method (arg 0) and path (arg 1) are strings
				for i := 0; i < 2; i++ {
					argType, err := c.checkExpr(e.Args[i])
					if err != nil {
						return 0, err
					}
					if argType != ast.TypeString {
						return 0, fmt.Errorf("http.route() argument %d must be string, got %s", i+1, typeName(argType))
					}
				}
				// Check handler (arg 2): accept string literal or function reference (Ident)
				var handlerName string
				switch h := e.Args[2].(type) {
				case *ast.StringLit:
					handlerName = h.Value
				case *ast.Ident:
					handlerName = h.Name
				default:
					return 0, fmt.Errorf("http.route() handler (argument 3) must be a function name or string literal")
				}
				sig, ok := c.funcs[handlerName]
				if !ok {
					return 0, fmt.Errorf("http.route() handler '%s' is not a defined function", handlerName)
				}
				if len(sig.Params) != 0 {
					return 0, fmt.Errorf("http.route() handler '%s' must take no parameters", handlerName)
				}
				if sig.ReturnType != ast.TypeString {
					return 0, fmt.Errorf("http.route() handler '%s' must return string", handlerName)
				}
				return ast.TypeVoid, nil
			}

			funcDef, ok := mod.Funcs[e.Name]
			if !ok {
				return 0, fmt.Errorf("undefined function '%s' in module '%s'", e.Name, e.Module)
			}
			if len(e.Args) != len(funcDef.Params) {
				return 0, fmt.Errorf("%s.%s() takes exactly %d argument(s), got %d", e.Module, e.Name, len(funcDef.Params), len(e.Args))
			}
			for i, arg := range e.Args {
				argType, err := c.checkExpr(arg)
				if err != nil {
					return 0, err
				}
				if argType != funcDef.Params[i] {
					return 0, fmt.Errorf("%s.%s() argument %d must be %s, got %s", e.Module, e.Name, i+1, typeName(funcDef.Params[i]), typeName(argType))
				}
			}
			return funcDef.ReturnType, nil
		}

		// Built-in: assert(condition: bool)
		if e.Name == "assert" {
			if len(e.Args) != 1 {
				return 0, fmt.Errorf("assert() takes exactly 1 argument, got %d", len(e.Args))
			}
			argType, err := c.checkExpr(e.Args[0])
			if err != nil {
				return 0, err
			}
			if argType != ast.TypeBool {
				return 0, fmt.Errorf("assert() argument must be bool, got %s", typeName(argType))
			}
			return ast.TypeVoid, nil
		}

		// Built-in: close(channel)
		if e.Name == "close" {
			if len(e.Args) != 1 {
				return 0, fmt.Errorf("close() takes exactly 1 argument, got %d", len(e.Args))
			}
			argType, err := c.checkExpr(e.Args[0])
			if err != nil {
				return 0, err
			}
			if !ast.IsChanType(argType) {
				return 0, fmt.Errorf("close() argument must be a channel, got %s", typeName(argType))
			}
			return ast.TypeVoid, nil
		}

		// Check if calling through a function-typed variable (indirect call)
		if varType, ok := c.resolve(e.Name); ok && ast.IsFuncType(varType) {
			fnParams := ast.FuncTypeParams(varType)
			fnReturn := ast.FuncTypeReturn(varType)
			if len(e.Args) != len(fnParams) {
				return 0, fmt.Errorf("function variable '%s' expects %d arguments, got %d", e.Name, len(fnParams), len(e.Args))
			}
			for i, arg := range e.Args {
				argType, err := c.checkExpr(arg)
				if err != nil {
					return 0, err
				}
				if argType != fnParams[i] {
					return 0, fmt.Errorf("argument %d of '%s': expected %s, got %s", i+1, e.Name, typeName(fnParams[i]), typeName(argType))
				}
			}
			return fnReturn, nil
		}

		// Unqualified call: user-defined function
		sig, ok := c.funcs[e.Name]
		if !ok {
			return 0, fmt.Errorf("undefined function '%s'", e.Name)
		}
		if len(e.Args) != len(sig.Params) {
			return 0, fmt.Errorf("function '%s' expects %d arguments, got %d", e.Name, len(sig.Params), len(e.Args))
		}
		for i, arg := range e.Args {
			argType, err := c.checkExpr(arg)
			if err != nil {
				return 0, err
			}
			if argType != sig.Params[i] {
				return 0, fmt.Errorf("argument %d of '%s': expected %s, got %s", i+1, e.Name, typeName(sig.Params[i]), typeName(argType))
			}
		}
		return sig.ReturnType, nil

	default:
		return 0, fmt.Errorf("unknown expression type")
	}
}

// Scope management

func (c *Checker) pushScope() {
	c.scopes = append(c.scopes, make(map[string]ast.Type))
	c.constScopes = append(c.constScopes, make(map[string]bool))
}

func (c *Checker) popScope() {
	c.scopes = c.scopes[:len(c.scopes)-1]
	c.constScopes = c.constScopes[:len(c.constScopes)-1]
}

func (c *Checker) define(name string, typ ast.Type) {
	c.scopes[len(c.scopes)-1][name] = typ
}

func (c *Checker) defineConst(name string) {
	c.constScopes[len(c.constScopes)-1][name] = true
}

func (c *Checker) isConst(name string) bool {
	for i := len(c.constScopes) - 1; i >= 0; i-- {
		if c.constScopes[i][name] {
			return true
		}
	}
	return false
}

func (c *Checker) resolve(name string) (ast.Type, bool) {
	for i := len(c.scopes) - 1; i >= 0; i-- {
		if typ, ok := c.scopes[i][name]; ok {
			return typ, true
		}
	}
	return 0, false
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

func isValidFieldType(t ast.Type) bool {
	switch t {
	case ast.TypeInt, ast.TypeBool, ast.TypeString, ast.TypeLong, ast.TypeDouble:
		return true
	default:
		return ast.IsStructType(t)
	}
}

func widerNumericType(a, b ast.Type) ast.Type {
	rank := map[ast.Type]int{ast.TypeInt: 0, ast.TypeLong: 1, ast.TypeDouble: 2}
	if rank[a] >= rank[b] {
		return a
	}
	return b
}

func isNumericType(t ast.Type) bool {
	return t == ast.TypeInt || t == ast.TypeLong || t == ast.TypeDouble
}

// isIntLiteral returns true if the expression is an integer literal.
func isIntLiteral(expr ast.Expr) bool {
	_, ok := expr.(*ast.IntLit)
	return ok
}

// isFloatLiteral returns true if the expression is a float literal.
func isFloatLiteral(expr ast.Expr) bool {
	_, ok := expr.(*ast.FloatLit)
	return ok
}

// canAssign checks if an expression of exprType can be assigned to a target of targetType,
// allowing implicit widening of int literals to long/double and float literals to double.
func canAssign(targetType, exprType ast.Type, expr ast.Expr) bool {
	if exprType == targetType {
		return true
	}
	// int literal -> long or double
	if isIntLiteral(expr) && (targetType == ast.TypeLong || targetType == ast.TypeDouble) {
		return true
	}
	// float literal -> double (already types as double, but keep for clarity)
	return false
}

func typeName(t ast.Type) string {
	switch t {
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
	default:
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
		return "unknown"
	}
}
