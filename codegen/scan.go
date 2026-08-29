package codegen

import (
	"github.com/ensamuel7/dex/ast"
)

func (g *Generator) scan(program *ast.Program) {
	// Scan struct field types (for mutex, etc.)
	for _, sd := range program.Structs {
		for _, f := range sd.Fields {
			g.scanType(f.Type)
		}
	}
	// Scan global let declarations
	for i := range program.GlobalLets {
		g.scanStmt(&program.GlobalLets[i])
	}
	for _, fn := range program.Functions {
		g.scanType(fn.ReturnType)
		for _, p := range fn.Params {
			g.scanType(p.Type)
		}
		for _, stmt := range fn.Body {
			g.scanStmt(stmt)
		}
	}
}

func (g *Generator) scanType(t ast.Type) {
	if ast.IsOptionalType(t) {
		g.usesOptional = true
		g.scanType(ast.OptionalInnerType(t))
		return
	}
	if t == ast.TypeBool {
		g.usesBool = true
	}
	if t == ast.TypeString {
		g.usesString = true
	}
	if ast.IsArrayType(t) {
		g.usesArray = true
		g.usesSafety = true
	}
	if ast.IsFuncType(t) {
		g.funcTypedef(t) // register the typedef
		g.usesClosure = true
		g.usesRefcount = true
	}
	if ast.IsWeakType(t) {
		g.usesWeakRef = true
		g.usesRefcount = true
	}
	if ast.IsMapType(t) {
		g.usesMap = true
	}
	if t == ast.TypeStringBuilder {
		g.usesStringBuilder = true
	}
	// json.Value is refcounted like any other heap value, and its runtime lives
	// in the json module, so a program that names the type must import json.
	if t == ast.TypeJsonValue {
		g.usesRefcount = true
		g.usesString = true
	}
	if t == ast.TypeMutex {
		g.usesConcurrency = true
	}
}

func (g *Generator) scanStmt(stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		g.scanType(s.Type)
		g.scanExpr(s.Value)
		// Detect annotation-based feature flags
		if ast.HasAnnotation(s.Annotations, ast.AnnotRegion) {
			g.usesArena = true
		}
		if ast.HasAnnotation(s.Annotations, ast.AnnotDebugCycles) {
			g.usesDebugCycles = true
		}
	case *ast.ReturnStmt:
		if s.Value != nil {
			g.scanExpr(s.Value)
		}
	case *ast.ExprStmt:
		g.scanExpr(s.Expr)
	case *ast.AssignStmt:
		g.scanExpr(s.Value)
	case *ast.IndexAssignStmt:
		g.scanExpr(s.Array)
		g.scanExpr(s.Index)
		g.scanExpr(s.Value)
	case *ast.IfStmt:
		g.scanExpr(s.Cond)
		for _, stmt := range s.Then {
			g.scanStmt(stmt)
		}
		for _, stmt := range s.Else {
			g.scanStmt(stmt)
		}
	case *ast.WhileStmt:
		g.scanExpr(s.Cond)
		for _, stmt := range s.Body {
			g.scanStmt(stmt)
		}
	case *ast.ForStmt:
		g.scanStmt(s.Init)
		g.scanExpr(s.Cond)
		g.scanStmt(s.Post)
		for _, stmt := range s.Body {
			g.scanStmt(stmt)
		}
	case *ast.ForeachStmt:
		g.scanExpr(s.Iterable)
		for _, stmt := range s.Body {
			g.scanStmt(stmt)
		}
	case *ast.BlockStmt:
		for _, stmt := range s.Stmts {
			g.scanStmt(stmt)
		}
	case *ast.FieldAssignStmt:
		g.scanExpr(s.Object)
		g.scanExpr(s.Value)
	case *ast.CompoundAssignStmt:
		g.scanExpr(s.Value)
	case *ast.SendStmt:
		if s.Target != nil {
			g.scanExpr(s.Target)
		}
		g.scanExpr(s.Value)
		g.usesConcurrency = true
	case *ast.TryCatchStmt:
		g.usesExceptions = true
		g.usesString = true
		for _, stmt := range s.Body {
			g.scanStmt(stmt)
		}
		for _, stmt := range s.CatchBody {
			g.scanStmt(stmt)
		}
		for _, stmt := range s.FinallyBody {
			g.scanStmt(stmt)
		}
	case *ast.ThrowStmt:
		g.usesExceptions = true
		g.usesString = true
		g.scanExpr(s.Value)
	case *ast.SwitchStmt:
		g.scanExpr(s.Tag)
		for _, sc := range s.Cases {
			for _, val := range sc.Values {
				g.scanExpr(val)
			}
			for _, stmt := range sc.Body {
				g.scanStmt(stmt)
			}
		}
		for _, stmt := range s.Default {
			g.scanStmt(stmt)
		}
	case *ast.DestructureLetStmt:
		g.scanExpr(s.Value)
	case *ast.DeferStmt:
		g.scanExpr(s.Expr)
	}
}

func (g *Generator) scanExpr(expr ast.Expr) {
	switch e := expr.(type) {
	case *ast.BoolLit:
		g.usesBool = true
	case *ast.StringLit:
		g.usesString = true
	case *ast.BinaryExpr:
		if e.Op == ast.BinDiv || e.Op == ast.BinMod {
			g.usesSafety = true
		}
		g.scanExpr(e.Left)
		g.scanExpr(e.Right)
	case *ast.UnaryExpr:
		g.scanExpr(e.Operand)
	case *ast.CallExpr:
		// Scan for bool usage from json module
		if e.Module == "json" {
			g.usesBool = true
		}
		// Scan for StringBuilder usage
		if e.Module == "" && e.Name == "StringBuilder" {
			g.usesStringBuilder = true
		}
		// Scan for assert usage
		if e.Module == "" && e.Name == "assert" {
			g.usesAssert = true
			g.usesBool = true
		}
		// Scan for mutex method usage
		if e.Module != "" && (e.Name == "lock" || e.Name == "unlock") {
			g.usesConcurrency = true
		}
		// Scan for string method usage — conservatively detect potential string methods.
		// False positives (e.g. array.len()) are harmless since the included static
		// functions will simply be unused and stripped by the C compiler.
		if e.Module != "" {
			switch e.Name {
			case "len", "contains", "startsWith", "endsWith", "indexOf",
				"toLower", "toUpper", "trim", "split", "substring", "replace", "charAt",
				"isEmpty", "isAlphanumeric", "isAlpha", "isDigit", "isNumeric", "isWhitespace",
				"containsUppercase", "containsLowercase", "containsDigit":
				g.usesStringMethods = true
			}
		}
		for _, arg := range e.Args {
			g.scanExpr(arg)
		}
	case *ast.ArrayLitExpr:
		g.usesArray = true
		for _, elem := range e.Elems {
			g.scanExpr(elem)
		}
	case *ast.IndexExpr:
		g.scanExpr(e.Array)
		g.scanExpr(e.Index)
	case *ast.SliceExpr:
		g.scanExpr(e.Array)
		if e.Start != nil {
			g.scanExpr(e.Start)
		}
		if e.End != nil {
			g.scanExpr(e.End)
		}
	case *ast.StructLitExpr:
		for _, v := range e.FieldValues {
			g.scanExpr(v)
		}
	case *ast.FieldAccessExpr:
		g.scanExpr(e.Object)
	case *ast.SpawnExpr:
		g.usesConcurrency = true
		if e.Body != nil {
			for _, stmt := range e.Body {
				g.scanStmt(stmt)
			}
		}
		if e.Call != nil {
			g.scanExpr(e.Call)
		}
	case *ast.ChannelExpr:
		g.usesConcurrency = true
	case *ast.ReceiveExpr:
		g.usesConcurrency = true
		g.scanExpr(e.Source)
	case *ast.NullLit:
		// null literal — optional usage detected via type scanning
	case *ast.EnumAccessExpr:
		// no-op: enum values are compile-time constants
	case *ast.MapLitExpr:
		g.usesMap = true
	case *ast.StringInterpExpr:
		g.usesString = true
		g.usesStringBuilder = true
		for _, part := range e.Parts {
			g.scanExpr(part)
		}
	case *ast.MatchExpr:
		g.scanExpr(e.Tag)
		for _, arm := range e.Arms {
			for _, pat := range arm.Patterns {
				g.scanExpr(pat)
			}
			g.scanExpr(arm.Body)
		}
	case *ast.LambdaExpr:
		for _, p := range e.Params {
			g.scanType(p.Type)
		}
		g.scanType(e.ReturnType)
		for _, stmt := range e.Body {
			g.scanStmt(stmt)
		}
	}
}

// functionUsesRegion checks if any let statement in the function uses #[region].
func (g *Generator) functionUsesRegion(fn *ast.Function) bool {
	for _, stmt := range fn.Body {
		if g.stmtUsesRegion(stmt) {
			return true
		}
	}
	return false
}

func (g *Generator) stmtUsesRegion(stmt ast.Stmt) bool {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		return ast.HasAnnotation(s.Annotations, ast.AnnotRegion)
	case *ast.IfStmt:
		for _, st := range s.Then {
			if g.stmtUsesRegion(st) {
				return true
			}
		}
		for _, st := range s.Else {
			if g.stmtUsesRegion(st) {
				return true
			}
		}
	case *ast.WhileStmt:
		for _, st := range s.Body {
			if g.stmtUsesRegion(st) {
				return true
			}
		}
	case *ast.ForStmt:
		for _, st := range s.Body {
			if g.stmtUsesRegion(st) {
				return true
			}
		}
	case *ast.BlockStmt:
		for _, st := range s.Stmts {
			if g.stmtUsesRegion(st) {
				return true
			}
		}
	case *ast.TryCatchStmt:
		for _, st := range s.Body {
			if g.stmtUsesRegion(st) {
				return true
			}
		}
		for _, st := range s.CatchBody {
			if g.stmtUsesRegion(st) {
				return true
			}
		}
		for _, st := range s.FinallyBody {
			if g.stmtUsesRegion(st) {
				return true
			}
		}
	case *ast.SwitchStmt:
		for _, sc := range s.Cases {
			for _, st := range sc.Body {
				if g.stmtUsesRegion(st) {
					return true
				}
			}
		}
		for _, st := range s.Default {
			if g.stmtUsesRegion(st) {
				return true
			}
		}
	}
	return false
}
