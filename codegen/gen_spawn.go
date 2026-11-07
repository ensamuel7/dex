package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/resolve"
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
		var sw strings.Builder
		sw.WriteString(fmt.Sprintf("void %s(void* _raw) {\n", wrapperName))
		sw.WriteString(fmt.Sprintf("    %s* _ctx = (%s*)_raw;\n", ctxType, ctxType))
		sw.WriteString("    DexChan* _ch = _ctx->_ch;\n")
		for _, cv := range captured {
			sw.WriteString(fmt.Sprintf("    %s %s = _ctx->%s;\n", g.cType(cv.typ), cv.name, cv.name))
		}
		sw.WriteString("    free(_raw);\n")

		// The body runs on another thread, in a function of its own, so its
		// locals are its own scope's: registered in the enclosing function's,
		// they were released there, where they do not exist.
		savedScopes := g.scopeStack
		restoreHoist := g.isolateFrame()
		g.scopeStack = nil
		g.pushScope()
		var bodyBuf strings.Builder
		for _, stmt := range e.Body {
			g.genStmt(&bodyBuf, stmt, 1)
		}
		g.popScope(&bodyBuf, "    ")
		g.scopeStack = savedScopes
		restoreHoist()
		sw.WriteString(bodyBuf.String())
		// The context holds a reference to every capture (taken at the call
		// site below), released here once the body is done with it.
		for _, cv := range captured {
			var rel strings.Builder
			g.emitReleaseVar(&rel, "    ", cv.name, cv.typ)
			sw.WriteString(rel.String())
		}
		sw.WriteString("    dex_release(_ch);\n")
		g.spawnWrappers.WriteString(sw.String())
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

		argTypes, coerced := g.spawnArgTypes(call)
		// Build context struct with channel + args
		g.spawnWrappers.WriteString(fmt.Sprintf("typedef struct { DexChan* _ch;"))
		for i := range call.Args {
			g.spawnWrappers.WriteString(fmt.Sprintf(" %s _a%d;", g.cType(argTypes[i]), i))
		}
		g.spawnWrappers.WriteString(fmt.Sprintf(" } %s;\n", ctxType))

		// Build wrapper function
		g.spawnWrappers.WriteString(fmt.Sprintf("void %s(void* _raw) {\n", wrapperName))
		g.spawnWrappers.WriteString(fmt.Sprintf("    %s* _ctx = (%s*)_raw;\n", ctxType, ctxType))
		g.spawnWrappers.WriteString("    DexChan* _ch = _ctx->_ch;\n")
		for i := range call.Args {
			g.spawnWrappers.WriteString(fmt.Sprintf("    %s _a%d = _ctx->_a%d;\n", g.cType(argTypes[i]), i, i))
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
		for i := range call.Args {
			var rel strings.Builder
			g.emitReleaseVar(&rel, "    ", fmt.Sprintf("_a%d", i), argTypes[i])
			g.spawnWrappers.WriteString(rel.String())
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
			switch {
			case coerced[i] && ast.IsInterfaceType(argTypes[i]):
				g.genInterfaceValue(out, arg, argTypes[i])
				out.WriteString("; ")
			case coerced[i]:
				g.genOptionalStructOwned(out, arg, argTypes[i])
				out.WriteString("; ")
			default:
				g.genExpr(out, arg)
				out.WriteString("; ")
				g.emitRetainMovedArg(out, fmt.Sprintf("_spawn_ctx->_a%d", i), arg)
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
	var result []capturedVar
	for name := range resolve.FreeNames(body) {
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
	return !resolve.DeclaredInFrame(g.currentFn.Body)[name]
}

func (g *Generator) emitRetainMovedArg(out *strings.Builder, target string, arg ast.Expr) {
	t := g.typeOfExpr(arg)
	if ast.IsStructType(t) || ast.IsPayloadEnumType(t) {
		g.emitRetainPushedStructFields(out, target, t, arg)
		return
	}
	if !g.borrowsHeapValue(arg) {
		return
	}
	var ret strings.Builder
	g.emitRetainValue(&ret, "", target, t)
	out.WriteString(strings.ReplaceAll(ret.String(), "\n", " "))
}

func (g *Generator) isolateFrame() func() {
	prelude, temps, registered, mark := g.stmtPrelude, g.stmtTemps, g.stmtTempsRegistered, g.stmtTempMark
	tryDepth, inLoop, loopDepth, loopTryDepth := g.tryDepth, g.isInLoop, g.loopDepth, g.loopTryDepth
	g.stmtPrelude, g.stmtTemps, g.stmtTempsRegistered, g.stmtTempMark = nil, nil, false, ""
	g.tryDepth, g.isInLoop, g.loopDepth, g.loopTryDepth = 0, false, 0, 0
	return func() {
		g.stmtPrelude, g.stmtTemps, g.stmtTempsRegistered, g.stmtTempMark = prelude, temps, registered, mark
		g.tryDepth, g.isInLoop, g.loopDepth, g.loopTryDepth = tryDepth, inLoop, loopDepth, loopTryDepth
	}
}

func (g *Generator) spawnArgTypes(call *ast.CallExpr) ([]ast.Type, []bool) {
	types := make([]ast.Type, len(call.Args))
	coerced := make([]bool, len(call.Args))
	var params []ast.Param
	if fn, ok := g.funcs[call.Name]; ok && call.Module == "" {
		params = fn.Params
	}
	for i, arg := range call.Args {
		types[i] = g.typeOfExpr(arg)
		if i >= len(params) {
			continue
		}
		pt := params[i].Type
		if ast.IsInterfaceType(pt) && !ast.IsInterfaceType(types[i]) {
			types[i], coerced[i] = pt, true
		} else if ast.IsStructOptionalType(pt) && !ast.IsOptionalType(types[i]) {
			types[i], coerced[i] = pt, true
		}
	}
	return types, coerced
}
