package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

func (g *Generator) genSpawnExpr(out *strings.Builder, e *ast.SpawnExpr) {
	idx := g.spawnCounter
	g.spawnCounter++
	wrapperName := fmt.Sprintf("_dex_spawn_%d", idx)
	ctxType := fmt.Sprintf("_dex_spawn_%d_ctx", idx)

	if e.Body != nil {
		// Spawn block: spawn { body }
		// Determine captured variables from outer scope used in body
		captured := g.findCapturedVars(e.Body)

		// Build context struct
		g.spawnWrappers.WriteString(fmt.Sprintf("typedef struct { DexChan* _ch;"))
		for _, cv := range captured {
			g.spawnWrappers.WriteString(fmt.Sprintf(" %s %s;", g.cType(cv.typ), cv.name))
		}
		g.spawnWrappers.WriteString(fmt.Sprintf(" } %s;\n", ctxType))

		// Build wrapper function
		g.spawnWrappers.WriteString(fmt.Sprintf("void* %s(void* _raw) {\n", wrapperName))
		g.spawnWrappers.WriteString(fmt.Sprintf("    %s* _ctx = (%s*)_raw;\n", ctxType, ctxType))
		g.spawnWrappers.WriteString("    DexChan* _ch = _ctx->_ch;\n")
		for _, cv := range captured {
			g.spawnWrappers.WriteString(fmt.Sprintf("    %s %s = _ctx->%s;\n", g.cType(cv.typ), cv.name, cv.name))
		}
		g.spawnWrappers.WriteString("    free(_raw);\n")

		// Generate body statements into wrapper
		var bodyBuf strings.Builder
		for _, stmt := range e.Body {
			g.genStmt(&bodyBuf, stmt, 1)
		}
		g.spawnWrappers.WriteString(bodyBuf.String())
		// Release heap-typed captures before returning
		for _, cv := range captured {
			if ast.IsHeapType(cv.typ) {
				g.spawnWrappers.WriteString(fmt.Sprintf("    dex_release(%s);\n", cv.name))
			}
		}
		g.spawnWrappers.WriteString("    dex_release(_ch);\n")
		g.spawnWrappers.WriteString("    return NULL;\n")
		g.spawnWrappers.WriteString("}\n")

		// Generate call site inline (using GCC statement expression)
		retCType := g.cType(e.ReturnType)
		if retCType == "void" {
			retCType = "int" // placeholder for sizeof in fire-and-forget
		}
		out.WriteString(fmt.Sprintf("({ DexChan* _spawn_ch = dex_chan_new(sizeof(%s), 64); ", retCType))
		out.WriteString(fmt.Sprintf("%s* _spawn_ctx = (%s*)malloc(sizeof(%s)); ", ctxType, ctxType, ctxType))
		out.WriteString("_spawn_ctx->_ch = _spawn_ch; dex_retain(_spawn_ch); ")
		for _, cv := range captured {
			out.WriteString(fmt.Sprintf("_spawn_ctx->%s = %s; ", cv.name, cv.name))
			if ast.IsHeapType(cv.typ) {
				out.WriteString(fmt.Sprintf("dex_retain(%s); ", cv.name))
			}
		}
		out.WriteString(fmt.Sprintf("pthread_t _spawn_t_%d; ", idx))
		out.WriteString(fmt.Sprintf("pthread_create(&_spawn_t_%d, NULL, %s, _spawn_ctx); ", idx, wrapperName))
		out.WriteString(fmt.Sprintf("pthread_detach(_spawn_t_%d); ", idx))
		out.WriteString("_spawn_ch; })")
	} else if e.Call != nil {
		// Spawn function call: spawn fn(args)
		call := e.Call.(*ast.CallExpr)

		// Build context struct with channel + args
		g.spawnWrappers.WriteString(fmt.Sprintf("typedef struct { DexChan* _ch;"))
		for i, arg := range call.Args {
			argType := g.typeOfExpr(arg)
			g.spawnWrappers.WriteString(fmt.Sprintf(" %s _a%d;", g.cType(argType), i))
		}
		g.spawnWrappers.WriteString(fmt.Sprintf(" } %s;\n", ctxType))

		// Build wrapper function
		g.spawnWrappers.WriteString(fmt.Sprintf("void* %s(void* _raw) {\n", wrapperName))
		g.spawnWrappers.WriteString(fmt.Sprintf("    %s* _ctx = (%s*)_raw;\n", ctxType, ctxType))
		g.spawnWrappers.WriteString("    DexChan* _ch = _ctx->_ch;\n")
		for i, arg := range call.Args {
			argType := g.typeOfExpr(arg)
			g.spawnWrappers.WriteString(fmt.Sprintf("    %s _a%d = _ctx->_a%d;\n", g.cType(argType), i, i))
		}
		g.spawnWrappers.WriteString("    free(_raw);\n")

		// Call the function
		retType := e.ReturnType
		callName := call.Name
		if call.Module != "" {
			callName = call.Module + "_" + call.Name
		}
		if retType != ast.TypeVoid {
			g.spawnWrappers.WriteString(fmt.Sprintf("    %s _ret = %s(", g.cType(retType), callName))
		} else {
			g.spawnWrappers.WriteString(fmt.Sprintf("    %s(", callName))
		}
		for i := range call.Args {
			if i > 0 {
				g.spawnWrappers.WriteString(", ")
			}
			g.spawnWrappers.WriteString(fmt.Sprintf("_a%d", i))
		}
		g.spawnWrappers.WriteString(");\n")
		if retType != ast.TypeVoid {
			g.spawnWrappers.WriteString("    dex_chan_send(_ch, &_ret);\n")
		}
		// Release heap-typed args before returning
		for i, arg := range call.Args {
			argType := g.typeOfExpr(arg)
			if ast.IsHeapType(argType) {
				g.spawnWrappers.WriteString(fmt.Sprintf("    dex_release(_a%d);\n", i))
			}
		}
		g.spawnWrappers.WriteString("    dex_release(_ch);\n")
		g.spawnWrappers.WriteString("    return NULL;\n")
		g.spawnWrappers.WriteString("}\n")

		// Generate call site
		retCType := g.cType(e.ReturnType)
		if retCType == "void" {
			retCType = "int" // use int for void tasks to have a valid sizeof
		}
		out.WriteString(fmt.Sprintf("({ DexChan* _spawn_ch = dex_chan_new(sizeof(%s), 1); ", retCType))
		out.WriteString(fmt.Sprintf("%s* _spawn_ctx = (%s*)malloc(sizeof(%s)); ", ctxType, ctxType, ctxType))
		out.WriteString("_spawn_ctx->_ch = _spawn_ch; dex_retain(_spawn_ch); ")
		for i, arg := range call.Args {
			out.WriteString(fmt.Sprintf("_spawn_ctx->_a%d = ", i))
			g.genExpr(out, arg)
			out.WriteString("; ")
			argType := g.typeOfExpr(arg)
			if ast.IsHeapType(argType) {
				out.WriteString(fmt.Sprintf("dex_retain(_spawn_ctx->_a%d); ", i))
			}
		}
		out.WriteString(fmt.Sprintf("pthread_t _spawn_t_%d; ", idx))
		out.WriteString(fmt.Sprintf("pthread_create(&_spawn_t_%d, NULL, %s, _spawn_ctx); ", idx, wrapperName))
		out.WriteString(fmt.Sprintf("pthread_detach(_spawn_t_%d); ", idx))
		out.WriteString("_spawn_ch; })")
	}
}

type capturedVar struct {
	name string
	typ  ast.Type
}

// findCapturedVars identifies variables referenced in spawn body that are defined in the outer scope.
func (g *Generator) findCapturedVars(body []ast.Stmt) []capturedVar {
	used := make(map[string]bool)
	defined := make(map[string]bool) // vars defined within the spawn body
	g.collectUsedVars(body, used, defined)

	var result []capturedVar
	for name := range used {
		if defined[name] {
			continue
		}
		if typ, ok := g.varTypes[name]; ok {
			result = append(result, capturedVar{name: name, typ: typ})
		}
	}
	return result
}

func (g *Generator) collectUsedVars(stmts []ast.Stmt, used, defined map[string]bool) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.LetStmt:
			g.collectUsedVarsExpr(s.Value, used)
			defined[s.Name] = true
		case *ast.ExprStmt:
			g.collectUsedVarsExpr(s.Expr, used)
		case *ast.ReturnStmt:
			if s.Value != nil {
				g.collectUsedVarsExpr(s.Value, used)
			}
		case *ast.AssignStmt:
			g.collectUsedVarsExpr(s.Value, used)
			used[s.Name] = true
		case *ast.SendStmt:
			if s.Target != nil {
				g.collectUsedVarsExpr(s.Target, used)
			}
			g.collectUsedVarsExpr(s.Value, used)
		case *ast.IfStmt:
			g.collectUsedVarsExpr(s.Cond, used)
			g.collectUsedVars(s.Then, used, defined)
			g.collectUsedVars(s.Else, used, defined)
		case *ast.WhileStmt:
			g.collectUsedVarsExpr(s.Cond, used)
			g.collectUsedVars(s.Body, used, defined)
		case *ast.ForStmt:
			g.collectUsedVars([]ast.Stmt{s.Init}, used, defined)
			g.collectUsedVarsExpr(s.Cond, used)
			g.collectUsedVars([]ast.Stmt{s.Post}, used, defined)
			g.collectUsedVars(s.Body, used, defined)
		case *ast.ForeachStmt:
			g.collectUsedVarsExpr(s.Iterable, used)
			defined[s.ValueVar] = true
			if s.IndexVar != "" {
				defined[s.IndexVar] = true
			}
			g.collectUsedVars(s.Body, used, defined)
		case *ast.BlockStmt:
			g.collectUsedVars(s.Stmts, used, defined)
		case *ast.IncrementStmt:
			used[s.Name] = true
		case *ast.DecrementStmt:
			used[s.Name] = true
		case *ast.CompoundAssignStmt:
			used[s.Name] = true
			g.collectUsedVarsExpr(s.Value, used)
		case *ast.IndexAssignStmt:
			g.collectUsedVarsExpr(s.Array, used)
			g.collectUsedVarsExpr(s.Index, used)
			g.collectUsedVarsExpr(s.Value, used)
		case *ast.FieldAssignStmt:
			g.collectUsedVarsExpr(s.Object, used)
			g.collectUsedVarsExpr(s.Value, used)
		}
	}
}

func (g *Generator) collectUsedVarsExpr(expr ast.Expr, used map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.Ident:
		used[e.Name] = true
	case *ast.BinaryExpr:
		g.collectUsedVarsExpr(e.Left, used)
		g.collectUsedVarsExpr(e.Right, used)
	case *ast.UnaryExpr:
		g.collectUsedVarsExpr(e.Operand, used)
	case *ast.CallExpr:
		for _, arg := range e.Args {
			g.collectUsedVarsExpr(arg, used)
		}
	case *ast.IndexExpr:
		g.collectUsedVarsExpr(e.Array, used)
		g.collectUsedVarsExpr(e.Index, used)
	case *ast.FieldAccessExpr:
		g.collectUsedVarsExpr(e.Object, used)
	case *ast.ArrayLitExpr:
		for _, elem := range e.Elems {
			g.collectUsedVarsExpr(elem, used)
		}
	case *ast.StructLitExpr:
		for _, v := range e.FieldValues {
			g.collectUsedVarsExpr(v, used)
		}
	case *ast.SpawnExpr:
		if e.Body != nil {
			defined := make(map[string]bool)
			g.collectUsedVars(e.Body, used, defined)
		}
		if e.Call != nil {
			g.collectUsedVarsExpr(e.Call, used)
		}
	case *ast.ReceiveExpr:
		g.collectUsedVarsExpr(e.Source, used)
	case *ast.ChannelExpr:
		// no vars
	case *ast.NullLit:
		// no vars
	case *ast.EnumAccessExpr:
		// no vars
	case *ast.MapLitExpr:
		// no vars
	}
}
