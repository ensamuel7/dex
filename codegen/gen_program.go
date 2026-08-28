package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/stdlib"
)

func (g *Generator) Generate(program *ast.Program) string {
	// Register imported modules
	for _, imp := range program.Imports {
		mod := stdlib.Lookup(imp.Path)
		if mod != nil {
			key := imp.Path
			if imp.Alias != "" {
				key = imp.Alias
			}
			g.importedModules[key] = mod
		}
	}

	// Register user modules
	for _, modName := range program.UserModules {
		g.userModules[modName] = true
	}

	// Register struct module mapping
	for sName, modName := range program.StructModule {
		g.structModules[sName] = modName
	}

	// Index functions
	for i := range program.Functions {
		g.funcs[program.Functions[i].Name] = &program.Functions[i]
	}

	// Store global lets for codegen
	g.globalLets = program.GlobalLets
	for _, gl := range g.globalLets {
		g.globalVars[gl.Name] = gl.Type
	}

	// Pre-scan to determine needed language-level features
	g.scan(program)

	// Event loop is needed when HTTP or WebSocket server modules are imported.
	// Map runtime is needed for HTTP (HttpRequest.params is map[string,string]).
	// This must run before the dependency resolution chain below.
	for _, imp := range program.Imports {
		if imp.Path == "http" || imp.Path == "ws" {
			g.usesEventLoop = true
		}
		if imp.Path == "http" {
			g.usesMap = true // HttpRequest.params is map[string,string]
		}
	}

	// String methods need both string and array runtimes (split returns DexArrayString)
	if g.usesStringMethods {
		g.usesString = true
		g.usesArray = true
	}

	// Maps need string and array runtimes (for keys/values returning arrays, string keys)
	if g.usesMap {
		g.usesString = true
		g.usesArray = true
	}

	// StringBuilder depends on the string runtime.
	// Also enable StringBuilder when string is used, since genStringConcat
	// auto-lowers 3+ operand chains to StringBuilder at code-gen time.
	if g.usesString {
		g.usesStringBuilder = true
	}
	if g.usesStringBuilder {
		g.usesString = true
	}

	// Arrays depend on the string runtime (DexArrayString uses DexString) and safety runtime (dex_panic)
	if g.usesArray {
		g.usesString = true
		g.usesSafety = true
	}

	// Refcount is needed whenever strings, arrays, or concurrency is used
	// The http and ws runtimes hold their handlers as function values, so the
	// closure representation has to exist whenever either is imported.
	if _, ok := g.importedModules["http"]; ok {
		g.usesClosure = true
	}
	if _, ok := g.importedModules["ws"]; ok {
		g.usesClosure = true
	}
	g.usesRefcount = g.usesString || g.usesArray || g.usesConcurrency || g.usesClosure

	var out strings.Builder

	// Collect and deduplicate includes from imported modules
	emittedIncludes := map[string]bool{}
	for _, mod := range g.importedModules {
		if mod.CIncludes != "" {
			for _, line := range strings.Split(mod.CIncludes, "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !emittedIncludes[line] {
					emittedIncludes[line] = true
					out.WriteString(line + "\n")
				}
			}
		}
	}

	if g.usesBool || g.usesOptional {
		inc := "#include <stdbool.h>"
		if !emittedIncludes[inc] {
			emittedIncludes[inc] = true
			out.WriteString(inc + "\n")
		}
	}

	// string.h is needed by many runtimes (time, string ops, etc.)
	// stddef.h is needed for offsetof in struct encoding
	for _, inc := range []string{"#include <string.h>", "#include <stddef.h>"} {
		if !emittedIncludes[inc] {
			emittedIncludes[inc] = true
			out.WriteString(inc + "\n")
		}
	}

	if g.usesOptional {
		inc := "#include <stdlib.h>"
		if !emittedIncludes[inc] {
			emittedIncludes[inc] = true
			out.WriteString(inc + "\n")
		}
	}

	if g.usesAssert {
		for _, inc := range []string{"#include <stdio.h>", "#include <stdlib.h>"} {
			if !emittedIncludes[inc] {
				emittedIncludes[inc] = true
				out.WriteString(inc + "\n")
			}
		}
	}

	if g.usesConcurrency {
		for _, inc := range []string{"#include <pthread.h>", "#include <stdlib.h>", "#include <string.h>"} {
			if !emittedIncludes[inc] {
				emittedIncludes[inc] = true
				out.WriteString(inc + "\n")
			}
		}
	}

	// Refcount needs these includes
	if g.usesRefcount {
		incs := []string{"#include <stdlib.h>", "#include <string.h>", "#include <stdio.h>"}
		if g.usesConcurrency {
			incs = append([]string{"#include <stdatomic.h>"}, incs...)
		}
		for _, inc := range incs {
			if !emittedIncludes[inc] {
				emittedIncludes[inc] = true
				out.WriteString(inc + "\n")
			}
		}
	}

	// Emit safety runtime (bounds checks, div-zero checks, panic helper)
	if g.usesSafety {
		for _, inc := range []string{"#include <stdio.h>", "#include <stdlib.h>"} {
			if !emittedIncludes[inc] {
				emittedIncludes[inc] = true
				out.WriteString(inc + "\n")
			}
		}
		out.WriteString(SafetyRuntime)
	}

	// Emit refcount runtime (must come before string/array/concurrency runtimes)
	if g.usesRefcount {
		if !g.usesConcurrency {
			out.WriteString("#define DEX_SINGLE_THREADED\n")
		}
		out.WriteString(RefcountRuntime)
	}

	// Emit closure runtime (needs the refcount header, nothing else)
	if g.usesClosure {
		out.WriteString(ClosureRuntime)
	}

	// Emit weak reference runtime (must come after refcount runtime)
	if g.usesWeakRef {
		out.WriteString(WeakRefRuntime)
	}

	// Emit string runtime (language-level feature for + on strings)
	if g.usesString {
		out.WriteString(StringRuntime)
	}

	// Emit StringBuilder runtime (must come after string runtime, before array)
	if g.usesStringBuilder {
		out.WriteString(StringBuilderRuntime)
	}

	// Emit array runtime (must come before module runtimes that reference array types)
	if g.usesArray {
		// Signals to the json module runtime, which is emitted later, that array
		// types exist and json.Value.keys() can be compiled in.
		out.WriteString("#define DEX_HAVE_ARRAYS 1\n")
		out.WriteString(ArrayRuntime)
	}

	// Emit string methods runtime (needs both DexString and DexArrayString for split)
	if g.usesStringMethods {
		out.WriteString(StringMethodsRuntime)
	}

	// Emit map runtime (needs DexString, DexArray for keys/values)
	if g.usesMap {
		out.WriteString(MapRuntime)
	}

	// Emit concurrency runtime
	if g.usesConcurrency {
		out.WriteString(ConcurrencyRuntime)
	}

	// Emit thread pool runtime (must come after concurrency, before event loop)
	if g.usesConcurrency || g.usesEventLoop {
		out.WriteString(ThreadPoolRuntime)
	}

	// Emit arena runtime (for #[region] annotations)
	if g.usesArena {
		out.WriteString(ArenaRuntime)
	}

	// Emit cycle debug runtime (for #[debug(cycles)] annotations)
	if g.usesDebugCycles {
		out.WriteString(CycleDebugRuntime)
	}

	// Emit optional runtime (value-type optional wrappers)
	if g.usesOptional {
		out.WriteString(OptionalRuntime)
	}

	// Emit exception runtime (setjmp/longjmp-based)
	if g.usesExceptions {
		out.WriteString(ExceptionRuntime)
	}

	// Emit built-in Exception struct typedef (before module/user struct typedefs)
	if g.usesExceptions {
		out.WriteString("typedef struct {\n")
		out.WriteString("    DexString* message;\n")
		out.WriteString("} Dex_Exception;\n")
	}

	// json.Value is defined by the json module runtime, which is emitted after
	// this point; the name has to exist first so a user struct can hold one.
	if _, ok := g.importedModules["json"]; ok {
		out.WriteString("typedef struct DexJsonValue DexJsonValue;\n")
	}

	// Emit struct typedefs (module-provided types first, then user-defined)
	for _, mod := range g.importedModules {
		for _, sd := range mod.Types {
			out.WriteString(fmt.Sprintf("typedef struct {\n"))
			for _, f := range sd.Fields {
				out.WriteString(fmt.Sprintf("    %s %s;\n", g.cType(f.Type), f.Name))
			}
			out.WriteString(fmt.Sprintf("} Dex_%s;\n", sd.Name))
		}
	}
	// A struct holding another struct by value must be declared after it, so the
	// definitions are ordered by that dependency rather than by source order.
	for _, sd := range orderStructsByDependency(program.Structs) {
		out.WriteString(fmt.Sprintf("typedef struct {\n"))
		for _, f := range sd.Fields {
			out.WriteString(fmt.Sprintf("    %s %s;\n", g.cType(f.Type), f.Name))
		}
		out.WriteString(fmt.Sprintf("} Dex_%s;\n", sd.Name))
	}
	// Emit enum typedefs
	for _, ed := range program.Enums {
		out.WriteString("typedef enum { ")
		for i, v := range ed.Variants {
			if i > 0 {
				out.WriteString(", ")
			}
			out.WriteString(fmt.Sprintf("Dex_%s_%s = %d", ed.Name, v, i))
		}
		out.WriteString(fmt.Sprintf(" } Dex_%s;\n", ed.Name))
	}
	// Emit cleanup functions for structs that have heap fields (used by struct arrays)
	for _, sd := range program.Structs {
		hasHeapFields := false
		for _, f := range sd.Fields {
			if ast.NeedsRelease(f.Type) {
				hasHeapFields = true
				break
			}
		}
		if hasHeapFields {
			out.WriteString(fmt.Sprintf("static void dex_cleanup_%s(void* ptr) {\n", sd.Name))
			out.WriteString(fmt.Sprintf("    Dex_%s* s = (Dex_%s*)ptr;\n", sd.Name, sd.Name))
			for _, f := range sd.Fields {
				if ast.IsHeapType(f.Type) {
					out.WriteString(fmt.Sprintf("    dex_release(s->%s);\n", f.Name))
				}
			}
			out.WriteString("}\n")
		}
	}

	// Emit global variable declarations as static C variables
	for _, gl := range program.GlobalLets {
		ctyp := g.cType(gl.Type)
		if _, isMutex := gl.Value.(*ast.MutexLit); isMutex {
			// Must be initialised here: the macro is a brace initialiser, so it
			// cannot be assigned later from main().
			out.WriteString(fmt.Sprintf("static %s %s = PTHREAD_MUTEX_INITIALIZER;\n", ctyp, gl.Name))
		} else if gl.IsConst && !ast.IsHeapType(gl.Type) && !ast.IsStructType(gl.Type) {
			// A const must carry its value here: C forbids assigning one later,
			// which is where every other global gets initialised. Only a literal
			// can be written at file scope, so anything more involved keeps the
			// deferred initialisation and gives up the C-level const — Dex still
			// enforces immutability in the checker either way.
			if literal, ok := constInitializer(gl.Value); ok {
				out.WriteString(fmt.Sprintf("static const %s %s = %s;\n", ctyp, gl.Name, literal))
				g.inlinedConsts[gl.Name] = true
			} else {
				out.WriteString(fmt.Sprintf("static %s %s;\n", ctyp, gl.Name))
			}
		} else {
			out.WriteString(fmt.Sprintf("static %s %s;\n", ctyp, gl.Name))
		}
	}
	if len(program.GlobalLets) > 0 {
		out.WriteString("\n")
	}

	// Emit interface typedef structs (vtable-based)
	for _, ifaceDef := range program.Interfaces {
		out.WriteString(fmt.Sprintf("typedef struct {\n"))
		out.WriteString("    void* _data;\n")
		out.WriteString("    struct {\n")
		for _, m := range ifaceDef.Methods {
			out.WriteString(fmt.Sprintf("        %s (*%s)(void*", g.cType(m.ReturnType), m.Name))
			for _, pt := range m.Params {
				out.WriteString(fmt.Sprintf(", %s", g.cType(pt)))
			}
			out.WriteString(");\n")
		}
		out.WriteString("    } _vtable;\n")
		out.WriteString(fmt.Sprintf("} Dex_%s;\n", ifaceDef.Name))
	}

	// Emit event loop runtime (must come before HTTP/WS module runtimes)
	if g.usesEventLoop {
		out.WriteString(EventLoopRuntime)
	}

	// Emit C runtime from imported modules
	for _, mod := range g.importedModules {
		if mod.CRuntime != "" {
			out.WriteString(mod.CRuntime)
		}
	}

	// Emit JSON codecs for primitive array struct fields. These must come after
	// both the array runtime and the json module runtime, since they bridge the
	// two; emitting them here keeps json.c free of any link-time dependency on
	// the conditionally-emitted array runtime.
	g.emitArrayFieldCodecs(&out, program)

	if out.Len() > 0 {
		out.WriteString("\n")
	}

	// Forward declarations for handler functions used by HTTP
	if _, ok := g.importedModules["http"]; ok {
		httpReqType, _ := ast.LookupStructType("HttpRequest")
		for _, fn := range program.Functions {
			if fn.Name == "main" {
				continue
			}
			if len(fn.Params) == 0 {
				out.WriteString(fmt.Sprintf("%s %s(void);\n", g.cType(fn.ReturnType), fn.Name))
			} else if len(fn.Params) == 1 && fn.Params[0].Type == httpReqType {
				out.WriteString(fmt.Sprintf("%s %s(Dex_HttpRequest %s);\n", g.cType(fn.ReturnType), fn.Name, fn.Params[0].Name))
			}
		}
		out.WriteString("\n")
	}

	// Forward declarations for handler functions used by WebSocket
	if _, ok := g.importedModules["ws"]; ok {
		wsConnType, _ := ast.LookupStructType("Conn")
		for _, fn := range program.Functions {
			if fn.Name == "main" {
				continue
			}
			// handleMessage / handleConnect: fn(ws.Conn, string): void
			if len(fn.Params) == 2 && fn.Params[0].Type == wsConnType && fn.Params[1].Type == ast.TypeString {
				out.WriteString(fmt.Sprintf("%s %s(Dex_Conn %s, DexString* %s);\n",
					g.cType(fn.ReturnType), fn.Name, fn.Params[0].Name, fn.Params[1].Name))
			}
			// handleDisconnect: fn(ws.Conn): void
			if len(fn.Params) == 1 && fn.Params[0].Type == wsConnType {
				out.WriteString(fmt.Sprintf("%s %s(Dex_Conn %s);\n",
					g.cType(fn.ReturnType), fn.Name, fn.Params[0].Name))
			}
		}
		out.WriteString("\n")
	}

	// Emit forward declarations for user module functions
	if len(program.UserModules) > 0 {
		for _, fn := range program.Functions {
			if fn.Name == "main" {
				continue
			}
			// Check if this function belongs to a user module (prefixed name)
			isUserModFn := false
			for _, modName := range program.UserModules {
				if strings.HasPrefix(fn.Name, modName+"_") {
					isUserModFn = true
					break
				}
			}
			if !isUserModFn {
				continue
			}
			retType := g.cType(fn.ReturnType)
			out.WriteString(fmt.Sprintf("%s %s(", retType, fn.Name))
			for i, p := range fn.Params {
				if i > 0 {
					out.WriteString(", ")
				}
				out.WriteString(fmt.Sprintf("%s %s", g.cType(p.Type), p.Name))
			}
			if len(fn.Params) == 0 {
				out.WriteString("void")
			}
			out.WriteString(");\n")
		}
		out.WriteString("\n")
	}

	// Emit forward declarations for all user-defined functions
	for _, fn := range program.Functions {
		if fn.Name == "main" {
			continue
		}
		retType := g.cType(fn.ReturnType)
		out.WriteString(fmt.Sprintf("%s %s(", retType, fn.Name))
		for i, p := range fn.Params {
			if i > 0 {
				out.WriteString(", ")
			}
			out.WriteString(fmt.Sprintf("%s %s", g.cType(p.Type), p.Name))
		}
		if len(fn.Params) == 0 {
			out.WriteString("void")
		}
		out.WriteString(");\n")
	}
	out.WriteString("\n")

	// Emit forward declarations for flattened struct method functions
	hasStructMethods := false
	for _, sd := range program.Structs {
		if len(sd.Methods) > 0 {
			hasStructMethods = true
			break
		}
	}
	if hasStructMethods {
		for _, fn := range program.Functions {
			if fn.Name == "main" {
				continue
			}
			// Check if this is a flattened struct method
			for _, sd := range program.Structs {
				prefix := sd.Name + "_"
				if len(sd.Methods) > 0 && strings.HasPrefix(fn.Name, prefix) {
					retType := g.cType(fn.ReturnType)
					out.WriteString(fmt.Sprintf("%s %s(", retType, fn.Name))
					for i, p := range fn.Params {
						if i > 0 {
							out.WriteString(", ")
						}
						out.WriteString(fmt.Sprintf("%s %s", g.cType(p.Type), p.Name))
					}
					if len(fn.Params) == 0 {
						out.WriteString("void")
					}
					out.WriteString(");\n")
					break
				}
			}
		}
		out.WriteString("\n")
	}

	// First pass: generate functions to collect spawn wrappers
	var funcBuf strings.Builder
	for i, fn := range program.Functions {
		if i > 0 {
			funcBuf.WriteString("\n")
		}
		g.genFunction(&funcBuf, &fn)
	}

	// Emit function pointer typedefs. These are written after the bodies have
	// been generated, because generating a body can introduce a new function
	// type — a method value handed to a route, say — and every one of them has
	// to be declared before the wrappers below use it.
	// Emit function pointer typedefs
	for t, name := range g.funcTypedefs {
		params := ast.FuncTypeParams(t)
		retType := ast.FuncTypeReturn(t)
		// The environment is a hidden first argument, so a plain function and a
		// closure over locals are invoked identically.
		out.WriteString(fmt.Sprintf("typedef %s (*%s)(void*", g.cType(retType), name))
		if len(params) > 0 {
			for _, p := range params {
				out.WriteString(", ")
				out.WriteString(g.cType(p))
			}
		}
		out.WriteString(");\n")
	}

	// Emit closure environment typedefs and wrappers. They come before the spawn
	// and lambda wrappers, which may themselves build closures.
	if g.closureTypes.Len() > 0 {
		out.WriteString(g.closureTypes.String())
		out.WriteString("\n")
	}
	if g.closureWrappers.Len() > 0 {
		out.WriteString(g.closureWrappers.String())
		out.WriteString("\n")
	}

	// Emit spawn wrappers (before function bodies)
	if g.spawnWrappers.Len() > 0 {
		out.WriteString(g.spawnWrappers.String())
		out.WriteString("\n")
	}

	// Emit lambda wrappers (before function bodies)
	if g.lambdaWrappers.Len() > 0 {
		out.WriteString(g.lambdaWrappers.String())
		out.WriteString("\n")
	}

	out.WriteString(funcBuf.String())

	return out.String()
}

// orderStructsByDependency returns the struct definitions ordered so that any
// struct held by value inside another comes first. C requires the inner type to
// be complete at the point the outer one is declared, and source order gives no
// such guarantee across modules. A cycle is impossible by value — a struct
// cannot contain itself — but the visiting set guards against one anyway rather
// than recursing forever on malformed input.
func orderStructsByDependency(structs []ast.StructDef) []ast.StructDef {
	byName := make(map[string]ast.StructDef, len(structs))
	for _, sd := range structs {
		byName[sd.Name] = sd
	}

	var ordered []ast.StructDef
	emitted := make(map[string]bool, len(structs))
	visiting := make(map[string]bool, len(structs))

	var visit func(sd ast.StructDef)
	visit = func(sd ast.StructDef) {
		if emitted[sd.Name] || visiting[sd.Name] {
			return
		}
		visiting[sd.Name] = true
		for _, f := range sd.Fields {
			// Only by-value struct fields force an ordering; a pointer field
			// (a struct array, an optional) only needs the name.
			if !ast.IsStructType(f.Type) {
				continue
			}
			if dep, ok := byName[ast.StructName(f.Type)]; ok {
				visit(dep)
			}
		}
		visiting[sd.Name] = false
		emitted[sd.Name] = true
		ordered = append(ordered, sd)
	}

	for _, sd := range structs {
		visit(sd)
	}
	return ordered
}

// constInitializer renders an expression as a C file-scope constant, reporting
// false when it is not a literal and so cannot be written there.
func constInitializer(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.IntLit:
		return fmt.Sprintf("%d", e.Value), true
	case *ast.FloatLit:
		return fmt.Sprintf("%g", e.Value), true
	case *ast.BoolLit:
		if e.Value {
			return "1", true
		}
		return "0", true
	case *ast.CharLit:
		return fmt.Sprintf("%d", int(e.Value)), true
	case *ast.UnaryExpr:
		if e.Op != ast.UnaryNeg {
			return "", false
		}
		inner, ok := constInitializer(e.Operand)
		if !ok {
			return "", false
		}
		return "-" + inner, true
	}
	return "", false
}
