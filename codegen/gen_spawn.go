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
		g.spawnWrappers.WriteString(fmt.Sprintf("void %s(void* _raw) {\n", wrapperName))
		g.spawnWrappers.WriteString(fmt.Sprintf("    %s* _ctx = (%s*)_raw;\n", ctxType, ctxType))
		g.spawnWrappers.WriteString("    DexChan* _ch = _ctx->_ch;\n")
		for _, cv := range captured {
			g.spawnWrappers.WriteString(fmt.Sprintf("    %s %s = _ctx->%s;\n", g.cType(cv.typ), cv.name, cv.name))
		}
		g.spawnWrappers.WriteString("    free(_raw);\n")

		// The body runs on another thread, in a function of its own, so its
		// locals are its own scope's: registered in the enclosing function's,
		// they were released there, where they do not exist.
		savedScopes := g.scopeStack
		g.scopeStack = nil
		g.pushScope()
		var bodyBuf strings.Builder
		for _, stmt := range e.Body {
			g.genStmt(&bodyBuf, stmt, 1)
		}
		g.popScope(&bodyBuf, "    ")
		g.scopeStack = savedScopes
		g.spawnWrappers.WriteString(bodyBuf.String())
		// The context holds a reference to every capture (taken at the call
		// site below), released here once the body is done with it.
		for _, cv := range captured {
			var rel strings.Builder
			g.emitReleaseVar(&rel, "    ", cv.name, cv.typ)
			g.spawnWrappers.WriteString(rel.String())
		}
		g.spawnWrappers.WriteString("    dex_release(_ch);\n")
		// void function — no return needed
		g.spawnWrappers.WriteString("}\n")

		// Generate call site inline (using GCC statement expression)
		retCType := g.cType(e.ReturnType)
		if retCType == "void" {
			retCType = "int" // placeholder for sizeof in fire-and-forget
		}
		out.WriteString(fmt.Sprintf("({ DexChan* _spawn_ch = dex_chan_new(sizeof(%s), 64); ", retCType))
		out.WriteString(fmt.Sprintf("%s* _spawn_ctx = (%s*)malloc(sizeof(%s)); ", ctxType, ctxType, ctxType))
		out.WriteString("_spawn_ctx->_ch = _spawn_ch; dex_retain(_spawn_ch); ")
		// Every capture that holds a reference takes one, whatever its type.
		// Retaining heap types alone left an interface's box and a struct's
		// strings shared with the frame that spawned the thread, which
		// released them on its way out while the thread still read them.
		for _, cv := range captured {
			out.WriteString(fmt.Sprintf("_spawn_ctx->%s = %s; ", cv.name, cv.name))
			var ret strings.Builder
			g.emitRetainValue(&ret, "", cv.name, cv.typ)
			out.WriteString(strings.ReplaceAll(ret.String(), "\n", " "))
		}
		out.WriteString(fmt.Sprintf("dex_spawn_submit(%s, _spawn_ctx); ", wrapperName))
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
		g.spawnWrappers.WriteString(fmt.Sprintf("void %s(void* _raw) {\n", wrapperName))
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
		// void function — no return needed
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
		out.WriteString(fmt.Sprintf("dex_spawn_submit(%s, _spawn_ctx); ", wrapperName))
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
		if g.namesGlobal(name) {
			continue
		}
		if typ, ok := g.varTypes[name]; ok {
			result = append(result, capturedVar{name: name, typ: typ})
		}
	}
	return result
}

// namesGlobal reports whether name, inside the function being generated, means
// a module-level variable rather than a local or a parameter.
//
// A global is not captured. The closure reaches it where it lives, as the
// function it was written in does; a copy would see none of the writes made
// after the closure was built, and a copied mutex is a different mutex, so
// `defer mu.unlock()` released a lock nobody held and left the real one locked
// for good.
//
// A local of the same name shadows the global, so any declaration of the name
// in the function, anywhere in it, keeps the capture. That is conservative: a
// local declared in a sibling block captures where the global was meant. It
// errs toward the behaviour every closure had before.
func (g *Generator) namesGlobal(name string) bool {
	if _, ok := g.globalVars[name]; !ok {
		return false
	}
	if g.currentFn == nil {
		return true
	}
	for _, p := range g.currentFn.Params {
		if p.Name == name {
			return false
		}
	}
	declared := make(map[string]bool)
	g.collectUsedVars(g.currentFn.Body, make(map[string]bool), declared)
	return !declared[name]
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
		case *ast.DestructureLetStmt:
			g.collectUsedVarsExpr(s.Value, used)
			for _, name := range s.Names {
				defined[name] = true
			}
		case *ast.DeferStmt:
			g.collectUsedVarsExpr(s.Expr, used)
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
		// A method call names its receiver in Module rather than as an operand,
		// so `handler.start(port)` uses `handler` and must capture it.
		if e.Module != "" {
			head := e.Module
			if dot := strings.Index(head, "."); dot >= 0 {
				head = head[:dot]
			}
			if _, isVar := g.varTypes[head]; isVar {
				used[head] = true
			}
		}
		g.collectUsedVarsExpr(e.Recv, used)
		for _, arg := range e.Args {
			g.collectUsedVarsExpr(arg, used)
		}
	case *ast.IndexExpr:
		g.collectUsedVarsExpr(e.Array, used)
		g.collectUsedVarsExpr(e.Index, used)
	case *ast.SliceExpr:
		g.collectUsedVarsExpr(e.Array, used)
		if e.Start != nil {
			g.collectUsedVarsExpr(e.Start, used)
		}
		if e.End != nil {
			g.collectUsedVarsExpr(e.End, used)
		}
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
	case *ast.StringInterpExpr:
		for _, part := range e.Parts {
			g.collectUsedVarsExpr(part, used)
		}
	case *ast.MatchExpr:
		g.collectUsedVarsExpr(e.Tag, used)
		for _, arm := range e.Arms {
			for _, pat := range arm.Patterns {
				g.collectUsedVarsExpr(pat, used)
			}
			g.collectUsedVarsExpr(arm.Body, used)
		}
	case *ast.LambdaExpr:
		// Lambda body captures are handled separately
		defined := make(map[string]bool)
		for _, p := range e.Params {
			defined[p.Name] = true
		}
		g.collectUsedVars(e.Body, used, defined)
	}
}
