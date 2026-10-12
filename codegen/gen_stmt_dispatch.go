package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/resolve"
)

func (g *Generator) genFunction(out *strings.Builder, fn *ast.Function) {
	// Reset var tracking for this function scope
	g.strVars = make(map[string]bool)
	g.arrVars = make(map[string]ast.Type)
	g.structVars = make(map[string]ast.Type)
	g.mapVars = make(map[string]ast.Type)
	g.sbVars = make(map[string]bool)
	g.varTypes = make(map[string]ast.Type)
	g.varAnnotations = make(map[string][]string)
	g.foreachCounter = 0
	g.scopeStack = nil
	g.currentFn = fn
	g.isInLoop = false
	g.loopDepth = 0
	g.narrowedVars = make(map[string]string)
	g.narrowedTypes = make(map[string]ast.Type)
	g.fnHasDefer = containsDefer(fn.Body)

	// Register global variables so all functions can resolve them
	for name, typ := range g.globalVars {
		g.varTypes[name] = typ
		if typ == ast.TypeString {
			g.strVars[name] = true
		}
		if ast.IsArrayType(typ) {
			g.arrVars[name] = typ
		}
		if ast.IsStructType(typ) {
			g.structVars[name] = typ
		}
		if ast.IsMapType(typ) {
			g.mapVars[name] = typ
		}
		if typ == ast.TypeStringBuilder {
			g.sbVars[name] = true
		}
	}

	// Register params (not tracked in scope — callee-borrows convention)
	for _, p := range fn.Params {
		g.varTypes[p.Name] = p.Type
		if p.Type == ast.TypeString {
			g.strVars[p.Name] = true
		}
		if ast.IsArrayType(p.Type) {
			g.arrVars[p.Name] = p.Type
		}
		if ast.IsStructType(p.Type) {
			g.structVars[p.Name] = p.Type
		}
		if ast.IsMapType(p.Type) {
			g.mapVars[p.Name] = p.Type
		}
		if p.Type == ast.TypeStringBuilder {
			g.sbVars[p.Name] = true
		}
	}

	// For main(), always emit "int main" in C regardless of Dex return type
	retType := g.cType(fn.ReturnType)
	if fn.Name == "main" {
		retType = "int"
	}
	name := fn.Name

	if fn.ReturnType == ast.TypeString && len(fn.Params) == 0 {
		out.WriteString(fmt.Sprintf("%s %s(void) {\n", retType, name))
	} else {
		out.WriteString(fmt.Sprintf("%s %s(", retType, name))
		for i, p := range fn.Params {
			if i > 0 {
				out.WriteString(", ")
			}
			out.WriteString(fmt.Sprintf("%s %s", g.cType(p.Type), p.Name))
		}
		out.WriteString(") {\n")
	}

	// Check if function uses #[region] — emit arena setup
	fnUsesArena := g.functionUsesRegion(fn)

	// Push function-level scope
	g.pushScope()

	if fnUsesArena {
		out.WriteString("    DexArena* _arena = dex_arena_new(65536);\n")
	}

	// Where this frame's deferred calls start. Taken before the body, so every
	// exit — a return, falling off the end, or an unwind reaching a catch
	// further out — runs exactly the calls this frame registered.
	if g.fnHasDefer {
		out.WriteString("    size_t _dex_defer_base = _dex_defer_top;\n")
	}

	// Line-buffer stdout. C block-buffers it whenever it is not a terminal, so a
	// long-running program whose output is redirected to a file or captured by a
	// container runtime would emit nothing until 4KB had accumulated — its logs
	// would appear late, out of order with stderr, and be lost entirely on a
	// crash.
	if fn.Name == "main" {
		out.WriteString("    setvbuf(stdout, NULL, _IOLBF, 0);\n")
	}

	// Register the leak report before anything can allocate.
	if fn.Name == "main" && g.memCheck && g.usesRefcount {
		out.WriteString("    dex_mc_init();\n")
	}

	// Initialize thread pool for spawn (must happen before any spawn calls)
	if fn.Name == "main" && g.usesConcurrency {
		out.WriteString("    dex_spawn_pool_init();\n")
	}

	// Initialize global variables at the top of main()
	if fn.Name == "main" && len(g.globalLets) > 0 {
		for i := range g.globalLets {
			g.genGlobalInit(out, &g.globalLets[i])
		}
	}

	for _, stmt := range fn.Body {
		g.genStmt(out, stmt, 1)
	}

	if fnUsesArena {
		out.WriteString("    dex_arena_destroy(_arena);\n")
	}

	// Emit deferred calls at function exit (for fall-through paths)
	g.emitDeferredCalls(out, "    ")

	// For void main(), insert cleanup + implicit return 0
	if fn.Name == "main" && fn.ReturnType == ast.TypeVoid {
		// Shut down thread pool (joins all workers, ensuring pending tasks complete)
		if g.usesConcurrency {
			out.WriteString("    dex_spawn_pool_shutdown();\n")
		}
		g.popScope(out, "    ")
		// Release global heap-typed variables
		for _, gl := range g.globalLets {
			if ast.NeedsRelease(gl.Type) {
				g.emitReleaseVar(out, "    ", gl.Name, gl.Type)
			}
		}
		out.WriteString("    return 0;\n")
	} else {
		// Pop function scope (cleanup for functions that fall through without return)
		g.popScope(out, "    ")

		// A function that always leaves by throwing never falls off its end,
		// but C cannot see that through longjmp and warns about the missing
		// return on every build. Saying so explicitly is the whole fix. A body
		// ending in a plain return needs nothing: C can see that one.
		if fn.ReturnType != ast.TypeVoid && !endsInReturn(fn.Body) && neverFallsThrough(fn.Body) {
			out.WriteString("    __builtin_unreachable();\n")
		}
	}

	out.WriteString("}\n")
}

func endsInReturn(body []ast.Stmt) bool {
	if len(body) == 0 {
		return false
	}
	_, ok := body[len(body)-1].(*ast.ReturnStmt)
	return ok
}

// neverFallsThrough reports whether a body always leaves by returning or
// throwing. A try counts when its own body and every handler do: an exception
// no handler claims is re-thrown, which does not return either.
func neverFallsThrough(body []ast.Stmt) bool {
	if len(body) == 0 {
		return false
	}
	switch last := body[len(body)-1].(type) {
	case *ast.ReturnStmt, *ast.ThrowStmt:
		return true
	case *ast.IfStmt:
		return len(last.Else) > 0 && neverFallsThrough(last.Then) && neverFallsThrough(last.Else)
	case *ast.BlockStmt:
		return neverFallsThrough(last.Stmts)
	case *ast.TryCatchStmt:
		if !neverFallsThrough(last.Body) {
			return false
		}
		for i := range last.Catches {
			if !neverFallsThrough(last.Catches[i].Body) {
				return false
			}
		}
		return true
	case *ast.SwitchStmt:
		if len(last.Default) == 0 {
			return false
		}
		for i := range last.Cases {
			if !neverFallsThrough(last.Cases[i].Body) {
				return false
			}
		}
		return neverFallsThrough(last.Default)
	}
	return false
}

// genGlobalInit emits initialization code for a module-level let/const variable.
// The static declaration is already emitted at file scope; this just assigns the value in main().
func (g *Generator) genGlobalInit(out *strings.Builder, gl *ast.LetStmt) {
	prefix := "    "
	name := gl.Name

	// Track type info so other codegen can resolve it
	g.varTypes[name] = gl.Type
	// A const whose value was written at its declaration is already set, and C
	// forbids assigning it here.
	if g.inlinedConsts[name] {
		return
	}
	if gl.Type == ast.TypeString {
		g.strVars[name] = true
	}
	if ast.IsArrayType(gl.Type) {
		g.arrVars[name] = gl.Type
	}
	if ast.IsStructType(gl.Type) {
		g.structVars[name] = gl.Type
	}
	if ast.IsMapType(gl.Type) {
		g.mapVars[name] = gl.Type
	}
	if gl.Type == ast.TypeStringBuilder {
		g.sbVars[name] = true
	}

	// A module-level mutex carries PTHREAD_MUTEX_INITIALIZER on its declaration.
	// That macro expands to a brace initialiser, which is only valid where an
	// initialiser is expected — assigning it here would not compile.
	if _, ok := gl.Value.(*ast.MutexLit); ok {
		return
	}

	switch {
	case gl.Type == ast.TypeString:
		if strLit, ok := gl.Value.(*ast.StringLit); ok {
			out.WriteString(fmt.Sprintf("%s%s = dex_string_from_lit(%q);\n", prefix, name, strLit.Value))
		} else {
			out.WriteString(fmt.Sprintf("%s%s = ", prefix, name))
			g.genExpr(out, gl.Value)
			out.WriteString(";\n")
		}
	case ast.IsMapType(gl.Type):
		if _, ok := gl.Value.(*ast.MapLitExpr); ok {
			suffix := g.mapSuffix(gl.Type)
			out.WriteString(fmt.Sprintf("%s%s = dex_map_%s_new();\n", prefix, name, suffix))
		} else {
			out.WriteString(fmt.Sprintf("%s%s = ", prefix, name))
			g.genExpr(out, gl.Value)
			out.WriteString(";\n")
		}
	case ast.IsArrayType(gl.Type):
		if arrLit, ok := gl.Value.(*ast.ArrayLitExpr); ok {
			if ast.IsStructArrayType(gl.Type) {
				elemType := ast.ElementType(gl.Type)
				elemCType := g.cType(elemType)
				cleanupFn := g.structArrayCleanupFunc(elemType)
				out.WriteString(fmt.Sprintf("%s%s = dex_array_struct_new(sizeof(%s), %s);\n", prefix, name, elemCType, cleanupFn))
				for _, elem := range arrLit.Elems {
					out.WriteString(prefix)
					g.genStructArrayLitElem(out, name, elemType, elem)
					out.WriteString("\n")
				}
			} else {
				cNewFn := g.arrayNewFunc(gl.Type)
				out.WriteString(fmt.Sprintf("%s%s = %s();\n", prefix, name, cNewFn))
				for _, elem := range arrLit.Elems {
					out.WriteString(prefix)
					g.genArrayLitElem(out, name, gl.Type, elem)
					out.WriteString("\n")
				}
			}
		} else {
			out.WriteString(fmt.Sprintf("%s%s = ", prefix, name))
			g.genExpr(out, gl.Value)
			out.WriteString(";\n")
		}
	default:
		out.WriteString(fmt.Sprintf("%s%s = ", prefix, name))
		g.genExpr(out, gl.Value)
		out.WriteString(";\n")
	}
}

// genStmt emits one statement. Declarations and assignments are generated into a
// side buffer first so that any temporaries hoisted while evaluating the
// right-hand side can be declared before the statement and released after it —
// a block would put the declared variable out of scope, so the two halves are
// emitted around the statement rather than wrapped about it.
func (g *Generator) genStmt(out *strings.Builder, stmt ast.Stmt, indent int) {
	g.emitSiteMarker(out, stmt, indent)
	switch stmt.(type) {
	case *ast.LetStmt, *ast.AssignStmt:
	case *ast.ReturnStmt:
		// A return hoists too, but its releases have to land before the return
		// rather than after it, so the ReturnStmt case emits them itself.
		prefix := strings.Repeat("    ", indent)
		savedPrelude, savedTemps := g.stmtPrelude, g.stmtTemps
		g.beginStmtHoist()
		var body strings.Builder
		g.genStmtInner(&body, stmt, indent)
		g.emitHoistPrelude(out, prefix)
		out.WriteString(body.String())
		g.stmtPrelude, g.stmtTemps = savedPrelude, savedTemps
		return
	default:
		g.genStmtInner(out, stmt, indent)
		return
	}

	prefix := strings.Repeat("    ", indent)
	savedPrelude, savedTemps := g.stmtPrelude, g.stmtTemps
	g.beginStmtHoist()
	var body strings.Builder
	g.genStmtInner(&body, stmt, indent)
	g.emitHoistPrelude(out, prefix)
	out.WriteString(body.String())
	g.emitHoistReleases(out, prefix)
	// An assignment replaced what the variable held, and released it. Every
	// branch of the assignment leaves the new value in the variable, so the
	// cleanup entry is pointed at it here — once, rather than in each of them.
	if assign, ok := stmt.(*ast.AssignStmt); ok {
		g.emitUnwindSlotUpdate(out, prefix, assign.Name)
	}
	g.stmtPrelude, g.stmtTemps = savedPrelude, savedTemps
}

// containsDefer reports whether a body registers any deferred call of its own.
// A defer inside a lambda belongs to that lambda's frame, not this one, so the
// walk stops at nested bodies.
func containsDefer(body []ast.Stmt) bool {
	found := false
	for _, stmt := range body {
		resolve.VisitStmtInFrame(stmt, func(st ast.Stmt) {
			if _, ok := st.(*ast.DeferStmt); ok {
				found = true
			}
		}, nil)
	}
	return found
}

// emitSiteMarker records the .dx position of the statement about to be
// generated, so that an allocation made while it runs can be reported against
// it. Only mem-check builds emit anything: DEX_AT is a no-op macro otherwise,
// but the marker is also pure noise in the C that DEX_KEEP_C leaves behind, so
// it is left out entirely rather than emitted and compiled away.
func (g *Generator) emitSiteMarker(out *strings.Builder, stmt ast.Stmt, indent int) {
	if !g.memCheck || !g.usesRefcount {
		return
	}
	pos := ast.StmtPos(stmt)
	if pos.Line == 0 {
		return
	}
	file := pos.File
	if file == "" {
		file = "<unknown>"
	}
	out.WriteString(fmt.Sprintf("%sDEX_AT(%s);\n",
		strings.Repeat("    ", indent),
		cStringLit(fmt.Sprintf("%s:%d", file, pos.Line))))
}

// cStringLit quotes a string for emission as a C literal.
func cStringLit(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteByte(c)
		case '\n':
			b.WriteString("\\n")
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}
