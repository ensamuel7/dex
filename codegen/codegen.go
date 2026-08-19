package codegen

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/stdlib"
)

type scopeVar struct {
	name string
	typ  ast.Type
}

type Generator struct {
	usesBool        bool
	usesString      bool
	usesArray       bool
	usesAssert      bool
	usesConcurrency bool
	usesSafety      bool
	usesRefcount    bool
	usesArena       bool
	usesDebugCycles bool
	usesWeakRef     bool
	usesOptional    bool
	usesExceptions    bool
	usesStringMethods bool
	usesMap           bool
	usesEventLoop       bool
	usesStringBuilder   bool

	tryCatchCounter int
	switchCounter   int

	importedModules map[string]*stdlib.Module
	userModules     map[string]bool
	structModules   map[string]string // structName -> moduleName

	funcs      map[string]*ast.Function
	strVars    map[string]bool      // variables known to be string type
	arrVars    map[string]ast.Type   // variables known to be array type (name -> array type)
	structVars map[string]ast.Type   // variables known to be struct type
	mapVars    map[string]ast.Type   // variables known to be map type (name -> map type)
	sbVars     map[string]bool      // variables known to be StringBuilder type
	varTypes   map[string]ast.Type   // all variable types for this function scope

	printCounter       int            // unique counter for print temp variables
	foreachCounter     int            // unique counter for foreach loop variables
	spawnCounter       int            // unique counter for spawn wrapper functions
	spawnWrappers      strings.Builder // collected wrapper functions emitted before main
	routeWrapperCount  int             // unique counter for route handler wrappers
	matchCounter       int            // unique counter for match expression temp vars
	lambdaCounter      int            // unique counter for lambda/closure functions
	lambdaWrappers     strings.Builder // collected lambda functions emitted before main

	funcTypedefs   map[ast.Type]string // function type → typedef name (e.g. DexFn_1)
	funcTypedefCnt int                 // counter for unique typedef names

	// Scope tracking for cleanup
	scopeStack     [][]scopeVar
	currentFn      *ast.Function        // current function being generated
	isInLoop       bool                 // whether we're inside a loop body
	loopDepth      int                  // scope depth at loop entry (for break/continue cleanup)
	varAnnotations map[string][]string  // per-variable annotation tracking

	// Type narrowing for optional types in null checks
	narrowedVars   map[string]string   // varName -> narrowed C var name in current scope
	narrowStack    []map[string]string // stack of narrowedVars snapshots for scope management

	// Statement-level temp tracking for string concat intermediates
	pendingReleases []string // temp var names to release after current statement
	tempCounter     int      // unique counter for temp names

	// Defer tracking
	deferExprs []ast.Expr // accumulated defer expressions for current function (LIFO order)
}

func New() *Generator {
	return &Generator{
		importedModules: make(map[string]*stdlib.Module),
		userModules:     make(map[string]bool),
		structModules:   make(map[string]string),
		funcs:           make(map[string]*ast.Function),
		strVars:         make(map[string]bool),
		arrVars:         make(map[string]ast.Type),
		structVars:      make(map[string]ast.Type),
		mapVars:         make(map[string]ast.Type),
		sbVars:          make(map[string]bool),
		varTypes:        make(map[string]ast.Type),
		funcTypedefs:    make(map[ast.Type]string),
		varAnnotations:  make(map[string][]string),
		narrowedVars:    make(map[string]string),
	}
}

// Scope management
func (g *Generator) pushScope() {
	g.scopeStack = append(g.scopeStack, nil)
}

func (g *Generator) popScope(out *strings.Builder, prefix string) {
	if len(g.scopeStack) == 0 {
		return
	}
	scope := g.scopeStack[len(g.scopeStack)-1]
	g.scopeStack = g.scopeStack[:len(g.scopeStack)-1]
	for i := len(scope) - 1; i >= 0; i-- {
		sv := scope[i]
		g.emitReleaseVar(out, prefix, sv.name, sv.typ)
	}
}

func (g *Generator) registerScopeVar(name string, typ ast.Type) {
	if !ast.NeedsRelease(typ) {
		return
	}
	if len(g.scopeStack) == 0 {
		return
	}
	idx := len(g.scopeStack) - 1
	g.scopeStack[idx] = append(g.scopeStack[idx], scopeVar{name: name, typ: typ})
}

func (g *Generator) emitReleaseVar(out *strings.Builder, prefix, name string, typ ast.Type) {
	if ast.IsOptionalType(typ) {
		inner := ast.OptionalInnerType(typ)
		if ast.IsValueType(inner) {
			// Value-type optional: no cleanup needed
			return
		}
		if ast.IsHeapType(inner) {
			// Heap-type optional: guard with null check
			out.WriteString(fmt.Sprintf("%sif (%s) { dex_release(%s); }\n", prefix, name, name))
			return
		}
		if ast.IsStructType(inner) {
			// Struct-type optional: free heap fields then free pointer
			def := ast.GetStructDef(inner)
			if def != nil {
				out.WriteString(fmt.Sprintf("%sif (%s) {\n", prefix, name))
				for _, f := range def.Fields {
					if ast.NeedsRelease(f.Type) {
						g.emitReleaseVar(out, prefix+"    ", name+"->"+f.Name, f.Type)
					}
				}
				out.WriteString(fmt.Sprintf("%s    free(%s);\n", prefix, name))
				out.WriteString(fmt.Sprintf("%s}\n", prefix))
			}
			return
		}
		return
	}
	if ast.IsHeapType(typ) {
		annots := g.varAnnotations[name]
		if ast.HasAnnotation(annots, ast.AnnotOwned) {
			out.WriteString(fmt.Sprintf("%sdex_owned_free(%s);\n", prefix, name))
		} else if ast.HasAnnotation(annots, ast.AnnotRegion) {
			// Skip — arena handles it
		} else {
			out.WriteString(fmt.Sprintf("%sdex_release(%s);\n", prefix, name))
		}
	} else if ast.IsStructType(typ) {
		def := ast.GetStructDef(typ)
		if def != nil {
			for _, f := range def.Fields {
				if ast.NeedsRelease(f.Type) {
					g.emitReleaseVar(out, prefix, name+"."+f.Name, f.Type)
				}
			}
		}
	}
}

// emitCleanupAll releases all vars in ALL scopes (for return statements), skipping exceptVar
func (g *Generator) emitCleanupAll(out *strings.Builder, prefix string, exceptVar string) {
	for i := len(g.scopeStack) - 1; i >= 0; i-- {
		scope := g.scopeStack[i]
		for j := len(scope) - 1; j >= 0; j-- {
			sv := scope[j]
			if sv.name == exceptVar {
				continue
			}
			g.emitReleaseVar(out, prefix, sv.name, sv.typ)
		}
	}
}

// emitCleanupInnerScopes releases vars from inner scopes down to (and including) targetDepth
func (g *Generator) emitCleanupInnerScopes(out *strings.Builder, prefix string, targetDepth int) {
	for i := len(g.scopeStack) - 1; i >= targetDepth; i-- {
		scope := g.scopeStack[i]
		for j := len(scope) - 1; j >= 0; j-- {
			sv := scope[j]
			g.emitReleaseVar(out, prefix, sv.name, sv.typ)
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

// emitDeferredCalls emits accumulated defer expressions in LIFO order (last defer first).
func (g *Generator) emitDeferredCalls(out *strings.Builder, prefix string) {
	for i := len(g.deferExprs) - 1; i >= 0; i-- {
		out.WriteString(prefix)
		g.genExpr(out, g.deferExprs[i])
		out.WriteString(";\n")
	}
}

// hasDefers returns true if there are pending deferred expressions.
func (g *Generator) hasDefers() bool {
	return len(g.deferExprs) > 0
}

// hasHeapVarsInScope checks if there are any heap vars tracked in any scope
func (g *Generator) hasHeapVarsInScope() bool {
	for _, scope := range g.scopeStack {
		if len(scope) > 0 {
			return true
		}
	}
	return false
}

// nextTemp returns a unique temporary variable name for string intermediates.
func (g *Generator) nextTemp() string {
	name := fmt.Sprintf("_dex_tmp_%d", g.tempCounter)
	g.tempCounter++
	return name
}

// CompilerFlags returns extra flags needed for the C compiler based on features used.
// Must be called after Generate().
func (g *Generator) CompilerFlags() []string {
	flags := []string{
		"-O3", "-flto",
		// Security hardening
		"-fstack-protector-strong",
		"-D_FORTIFY_SOURCE=2",
		// Format string security warnings
		"-Wformat", "-Wformat-security",
		// Suppress expected noise from static runtime functions
		"-Wno-unused-function",
	}
	if runtime.GOOS == "darwin" {
		flags = append(flags, "-Wl,-dead_strip")
	} else if runtime.GOOS == "linux" {
		flags = append(flags,
			"-Wl,--gc-sections",
			// Position-independent executable (default on macOS, explicit on Linux)
			"-fPIE", "-pie",
		)
	}
	if g.usesConcurrency {
		flags = append(flags, "-pthread")
	}
	for _, mod := range g.importedModules {
		flags = append(flags, mod.CFlags...)
	}
	return flags
}

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

	// Pre-scan to determine needed language-level features
	g.scan(program)

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

	// Event loop is needed when HTTP or WebSocket server modules are imported
	for _, imp := range program.Imports {
		if imp.Path == "http" || imp.Path == "ws" {
			g.usesEventLoop = true
			break
		}
	}

	// Refcount is needed whenever strings, arrays, or concurrency is used
	g.usesRefcount = g.usesString || g.usesArray || g.usesConcurrency

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
	for _, sd := range program.Structs {
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

	// Emit function pointer typedefs
	for t, name := range g.funcTypedefs {
		params := ast.FuncTypeParams(t)
		retType := ast.FuncTypeReturn(t)
		out.WriteString(fmt.Sprintf("typedef %s (*%s)(", g.cType(retType), name))
		if len(params) == 0 {
			out.WriteString("void")
		} else {
			for i, p := range params {
				if i > 0 {
					out.WriteString(", ")
				}
				out.WriteString(g.cType(p))
			}
		}
		out.WriteString(");\n")
	}

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

func (g *Generator) scan(program *ast.Program) {
	// Scan struct field types (for mutex, etc.)
	for _, sd := range program.Structs {
		for _, f := range sd.Fields {
			g.scanType(f.Type)
		}
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
				"toLower", "toUpper", "trim", "split", "substring", "replace", "charAt":
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

