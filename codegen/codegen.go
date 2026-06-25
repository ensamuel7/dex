package codegen

import (
	"fmt"
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

	importedModules map[string]*stdlib.Module
	userModules     map[string]bool

	funcs      map[string]*ast.Function
	strVars    map[string]bool      // variables known to be string type
	arrVars    map[string]ast.Type   // variables known to be array type (name -> array type)
	structVars map[string]ast.Type   // variables known to be struct type
	varTypes   map[string]ast.Type   // all variable types for this function scope

	foreachCounter     int            // unique counter for foreach loop variables
	spawnCounter       int            // unique counter for spawn wrapper functions
	spawnWrappers      strings.Builder // collected wrapper functions emitted before main
	routeWrapperCount  int             // unique counter for route handler wrappers

	funcTypedefs   map[ast.Type]string // function type → typedef name (e.g. DexFn_1)
	funcTypedefCnt int                 // counter for unique typedef names

	// Scope tracking for cleanup
	scopeStack     [][]scopeVar
	currentFn      *ast.Function        // current function being generated
	isInLoop       bool                 // whether we're inside a loop body
	loopDepth      int                  // scope depth at loop entry (for break/continue cleanup)
	varAnnotations map[string][]string  // per-variable annotation tracking
}

func New() *Generator {
	return &Generator{
		importedModules: make(map[string]*stdlib.Module),
		userModules:     make(map[string]bool),
		funcs:           make(map[string]*ast.Function),
		strVars:         make(map[string]bool),
		arrVars:         make(map[string]ast.Type),
		structVars:      make(map[string]ast.Type),
		varTypes:        make(map[string]ast.Type),
		funcTypedefs:    make(map[ast.Type]string),
		varAnnotations:  make(map[string][]string),
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

// emitCleanupInnerScopes releases vars from inner scopes down to (not including) targetDepth
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
	}
	return false
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

// CompilerFlags returns extra flags needed for the C compiler based on features used.
// Must be called after Generate().
func (g *Generator) CompilerFlags() []string {
	flags := []string{"-O2"}
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
			g.importedModules[imp.Path] = mod
		}
	}

	// Register user modules
	for _, modName := range program.UserModules {
		g.userModules[modName] = true
	}

	// Index functions
	for i := range program.Functions {
		g.funcs[program.Functions[i].Name] = &program.Functions[i]
	}

	// Pre-scan to determine needed language-level features
	g.scan(program)

	// Arrays depend on the string runtime (DexArrayString uses DexString)
	if g.usesArray {
		g.usesString = true
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

	if g.usesBool {
		inc := "#include <stdbool.h>"
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
		for _, inc := range []string{"#include <stdatomic.h>", "#include <stdlib.h>", "#include <string.h>", "#include <stdio.h>"} {
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

	// Emit array runtime (must come before module runtimes that reference array types)
	if g.usesArray {
		out.WriteString(ArrayRuntime)
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
		for _, fn := range program.Functions {
			if len(fn.Params) == 0 && fn.Name != "main" {
				out.WriteString(fmt.Sprintf("%s %s(void);\n", g.cType(fn.ReturnType), fn.Name))
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
	if g.usesConcurrency {
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

	out.WriteString(funcBuf.String())

	return out.String()
}

func (g *Generator) scan(program *ast.Program) {
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
		g.scanExpr(s.Value)
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
		// Scan for assert usage
		if e.Module == "" && e.Name == "assert" {
			g.usesAssert = true
			g.usesBool = true
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
	}
}

func (g *Generator) genFunction(out *strings.Builder, fn *ast.Function) {
	// Reset var tracking for this function scope
	g.strVars = make(map[string]bool)
	g.arrVars = make(map[string]ast.Type)
	g.structVars = make(map[string]ast.Type)
	g.varTypes = make(map[string]ast.Type)
	g.varAnnotations = make(map[string][]string)
	g.foreachCounter = 0
	g.scopeStack = nil
	g.currentFn = fn
	g.isInLoop = false
	g.loopDepth = 0

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

	for _, stmt := range fn.Body {
		g.genStmt(out, stmt, 1)
	}

	if fnUsesArena {
		out.WriteString("    dex_arena_destroy(_arena);\n")
	}

	// For void main(), insert cleanup + implicit return 0
	if fn.Name == "main" && fn.ReturnType == ast.TypeVoid {
		g.popScope(out, "    ")
		out.WriteString("    return 0;\n")
	} else {
		// Pop function scope (cleanup for functions that fall through without return)
		g.popScope(out, "    ")
	}

	out.WriteString("}\n")
}

func (g *Generator) genStmt(out *strings.Builder, stmt ast.Stmt, indent int) {
	prefix := strings.Repeat("    ", indent)

	switch s := stmt.(type) {
	case *ast.LetStmt:
		g.varTypes[s.Name] = s.Type
		if len(s.Annotations) > 0 {
			g.varAnnotations[s.Name] = s.Annotations
		}
		if s.Type == ast.TypeString {
			g.strVars[s.Name] = true
		}
		if ast.IsArrayType(s.Type) {
			g.arrVars[s.Name] = s.Type
		}
		if ast.IsStructType(s.Type) {
			g.structVars[s.Name] = s.Type
		}
		// Special case for channel/task types: let ch = channel(int), let t = spawn { ... }
		if ast.IsChanType(s.Type) || ast.IsTaskType(s.Type) {
			// Already handled by varTypes above; ensure cType works
		}
		// Special case for receive expression: let val = receive(task)
		if recvExpr, ok := s.Value.(*ast.ReceiveExpr); ok {
			ctyp := g.cType(s.Type)
			out.WriteString(fmt.Sprintf("%s%s %s; dex_chan_recv(", prefix, ctyp, s.Name))
			g.genExpr(out, recvExpr.Source)
			out.WriteString(fmt.Sprintf(", &%s);\n", s.Name))
			g.registerScopeVar(s.Name, s.Type)
			break
		}
		// Special case for array literal value
		if arrLit, ok := s.Value.(*ast.ArrayLitExpr); ok {
			if ast.IsStructArrayType(s.Type) {
				// Struct array: use generic DexArrayStruct
				elemType := ast.ElementType(s.Type)
				elemCType := g.cType(elemType)
				cleanupFn := g.structArrayCleanupFunc(elemType)
				out.WriteString(fmt.Sprintf("%sDexArrayStruct* %s = dex_array_struct_new(sizeof(%s), %s);\n", prefix, s.Name, elemCType, cleanupFn))
				if len(arrLit.Elems) > 0 {
					for _, elem := range arrLit.Elems {
						out.WriteString(fmt.Sprintf("%s{ %s _tmp_elem = ", prefix, elemCType))
						g.genExpr(out, elem)
						out.WriteString(fmt.Sprintf("; dex_array_struct_push(%s, &_tmp_elem); }\n", s.Name))
					}
				}
				g.registerScopeVar(s.Name, s.Type)
				break
			}
			cNewFn := g.arrayNewFunc(s.Type)
			out.WriteString(fmt.Sprintf("%s%s %s = %s();\n", prefix, g.cType(s.Type), s.Name, cNewFn))
			if len(arrLit.Elems) > 0 {
				// Inline initialize data
				for i, elem := range arrLit.Elems {
					if s.Type == ast.TypeArrayString {
						// For string arrays, we need to retain the string element
						out.WriteString(fmt.Sprintf("%s%s->data[%d] = ", prefix, s.Name, i))
						g.genExpr(out, elem)
						out.WriteString(";\n")
						// String literals from genExpr produce +1 refs via dex_string_from_lit
						// so no extra retain needed
					} else {
						out.WriteString(fmt.Sprintf("%s%s->data[%d] = ", prefix, s.Name, i))
						g.genExpr(out, elem)
						out.WriteString(";\n")
					}
				}
				out.WriteString(fmt.Sprintf("%s%s->len = %d;\n", prefix, s.Name, len(arrLit.Elems)))
			}
			g.registerScopeVar(s.Name, s.Type)
			break
		}
		// Special case for string declarations
		if s.Type == ast.TypeString {
			if ast.HasAnnotation(s.Annotations, ast.AnnotRegion) {
				// #[region] string — allocate from arena
				if strLit, ok := s.Value.(*ast.StringLit); ok {
					out.WriteString(fmt.Sprintf("%sDexString* %s = dex_arena_string(_arena, %q, %d);\n", prefix, s.Name, strLit.Value, len(strLit.Value)))
				} else {
					out.WriteString(fmt.Sprintf("%sDexString* %s = ", prefix, s.Name))
					g.genExpr(out, s.Value)
					out.WriteString(";\n")
				}
				g.registerScopeVar(s.Name, s.Type)
				break
			}
			if strLit, ok := s.Value.(*ast.StringLit); ok {
				out.WriteString(fmt.Sprintf("%sDexString* %s = dex_string_from_lit(%q);\n", prefix, s.Name, strLit.Value))
			} else {
				out.WriteString(fmt.Sprintf("%sDexString* %s = ", prefix, s.Name))
				g.genExpr(out, s.Value)
				out.WriteString(";\n")
				// If RHS is a variable reference (borrowed), retain it
				// But not for #[owned] — ownership transfer, no retain
				if _, ok := s.Value.(*ast.Ident); ok {
					if !ast.HasAnnotation(s.Annotations, ast.AnnotOwned) {
						out.WriteString(fmt.Sprintf("%sdex_retain(%s);\n", prefix, s.Name))
					}
				}
			}
			g.registerScopeVar(s.Name, s.Type)
			// Emit debug cycle tracking if annotated
			if ast.HasAnnotation(s.Annotations, ast.AnnotDebugCycles) {
				out.WriteString(fmt.Sprintf("%sdex_cycle_track(%s, %q);\n", prefix, s.Name, s.Name))
			}
			break
		}
		// Special case for weak reference declarations
		if ast.IsWeakType(s.Type) {
			out.WriteString(fmt.Sprintf("%sDexWeakRef* %s = dex_weak_new(", prefix, s.Name))
			g.genExpr(out, s.Value)
			out.WriteString(");\n")
			g.registerScopeVar(s.Name, s.Type)
			break
		}
		constPrefix := ""
		if s.IsConst {
			constPrefix = "const "
		}
		out.WriteString(fmt.Sprintf("%s%s%s %s = ", prefix, constPrefix, g.cType(s.Type), s.Name))
		g.genExpr(out, s.Value)
		out.WriteString(";\n")
		g.registerScopeVar(s.Name, s.Type)
		// Emit debug cycle tracking if annotated
		if ast.HasAnnotation(s.Annotations, ast.AnnotDebugCycles) && ast.IsHeapType(s.Type) {
			out.WriteString(fmt.Sprintf("%sdex_cycle_track(%s, %q);\n", prefix, s.Name, s.Name))
		}

	case *ast.ReturnStmt:
		retType := g.currentFn.ReturnType
		if ast.IsHeapType(retType) {
			// Retain the return value, clean up everything else, then return
			if ident, ok := s.Value.(*ast.Ident); ok {
				out.WriteString(fmt.Sprintf("%sdex_retain(%s);\n", prefix, ident.Name))
				g.emitCleanupAll(out, prefix, "")
				out.WriteString(fmt.Sprintf("%sreturn %s;\n", prefix, ident.Name))
			} else {
				// Expression result — eval into temp
				out.WriteString(fmt.Sprintf("%s%s _ret_tmp = ", prefix, g.cType(retType)))
				g.genExpr(out, s.Value)
				out.WriteString(";\n")
				g.emitCleanupAll(out, prefix, "")
				out.WriteString(fmt.Sprintf("%sreturn _ret_tmp;\n", prefix))
			}
		} else {
			// Non-heap return — clean up all heap vars first
			if g.hasHeapVarsInScope() {
				// Evaluate return value into temp to avoid use-after-free
				ctyp := g.cType(retType)
				if retType != ast.TypeVoid {
					out.WriteString(fmt.Sprintf("%s%s _ret_tmp = ", prefix, ctyp))
					g.genExpr(out, s.Value)
					out.WriteString(";\n")
					g.emitCleanupAll(out, prefix, "")
					out.WriteString(fmt.Sprintf("%sreturn _ret_tmp;\n", prefix))
				} else {
					g.emitCleanupAll(out, prefix, "")
					out.WriteString(fmt.Sprintf("%sreturn;\n", prefix))
				}
			} else {
				out.WriteString(fmt.Sprintf("%sreturn ", prefix))
				g.genExpr(out, s.Value)
				out.WriteString(";\n")
			}
		}

	case *ast.ExprStmt:
		// Fire-and-forget spawn: suppress unused value warning by casting to void
		if _, ok := s.Expr.(*ast.SpawnExpr); ok {
			out.WriteString(prefix + "(void)")
			g.genExpr(out, s.Expr)
			out.WriteString(";\n")
			break
		}
		// Check if this is a call that returns a heap type we need to release
		if call, ok := s.Expr.(*ast.CallExpr); ok {
			callRetType := g.typeOfExpr(call)
			if ast.IsHeapType(callRetType) {
				// Result is discarded, but may be a new allocation — release it
				out.WriteString(fmt.Sprintf("%sdex_release(", prefix))
				g.genExpr(out, s.Expr)
				out.WriteString(");\n")
				break
			}
		}
		out.WriteString(prefix)
		g.genExpr(out, s.Expr)
		out.WriteString(";\n")

	case *ast.AssignStmt:
		varType := g.varTypes[s.Name]
		if ast.IsHeapType(varType) {
			// For heap-typed reassignment: old = var; var = new_val; release(old);
			out.WriteString(fmt.Sprintf("%s{ %s _dex_old = %s; %s = ", prefix, g.cType(varType), s.Name, s.Name))
			g.genExpr(out, s.Value)
			out.WriteString(";")
			// If the RHS is a variable reference (borrowed), retain
			if _, ok := s.Value.(*ast.Ident); ok {
				out.WriteString(fmt.Sprintf(" dex_retain(%s);", s.Name))
			}
			out.WriteString(" dex_release(_dex_old); }\n")
		} else {
			out.WriteString(fmt.Sprintf("%s%s = ", prefix, s.Name))
			g.genExpr(out, s.Value)
			out.WriteString(";\n")
		}

	case *ast.IfStmt:
		out.WriteString(fmt.Sprintf("%sif (", prefix))
		g.genExprNoParen(out, s.Cond)
		out.WriteString(") {\n")
		g.pushScope()
		for _, stmt := range s.Then {
			g.genStmt(out, stmt, indent+1)
		}
		g.popScope(out, prefix+"    ")
		if s.Else != nil {
			out.WriteString(fmt.Sprintf("%s} else {\n", prefix))
			g.pushScope()
			for _, stmt := range s.Else {
				g.genStmt(out, stmt, indent+1)
			}
			g.popScope(out, prefix+"    ")
		}
		out.WriteString(fmt.Sprintf("%s}\n", prefix))

	case *ast.WhileStmt:
		out.WriteString(fmt.Sprintf("%swhile (", prefix))
		g.genExprNoParen(out, s.Cond)
		out.WriteString(") {\n")
		savedLoop := g.isInLoop
		savedDepth := g.loopDepth
		g.isInLoop = true
		g.pushScope()
		g.loopDepth = len(g.scopeStack)
		for _, stmt := range s.Body {
			g.genStmt(out, stmt, indent+1)
		}
		g.popScope(out, prefix+"    ")
		g.isInLoop = savedLoop
		g.loopDepth = savedDepth
		out.WriteString(fmt.Sprintf("%s}\n", prefix))

	case *ast.ForStmt:
		out.WriteString(fmt.Sprintf("%sfor (", prefix))
		g.genForInit(out, s.Init)
		out.WriteString("; ")
		g.genExprNoParen(out, s.Cond)
		out.WriteString("; ")
		g.genForPost(out, s.Post)
		out.WriteString(") {\n")
		savedLoop := g.isInLoop
		savedDepth := g.loopDepth
		g.isInLoop = true
		g.pushScope()
		g.loopDepth = len(g.scopeStack)
		for _, stmt := range s.Body {
			g.genStmt(out, stmt, indent+1)
		}
		g.popScope(out, prefix+"    ")
		g.isInLoop = savedLoop
		g.loopDepth = savedDepth
		out.WriteString(fmt.Sprintf("%s}\n", prefix))

	case *ast.ForeachStmt:
		idx := g.foreachCounter
		g.foreachCounter++
		idxVar := fmt.Sprintf("_foreach_idx_%d", idx)
		// Determine the array expression name for ->len and ->data access
		arrExpr := g.exprToString(s.Iterable)
		// Get element type from the iterable
		arrType := g.typeOfExpr(s.Iterable)
		elemType := ast.ElementType(arrType)
		elemCType := g.cType(elemType)

		out.WriteString(fmt.Sprintf("%sfor (int %s = 0; %s < %s->len; %s++) {\n",
			prefix, idxVar, idxVar, arrExpr, idxVar))
		savedLoop := g.isInLoop
		savedDepth := g.loopDepth
		g.isInLoop = true
		g.pushScope()
		g.loopDepth = len(g.scopeStack)
		// Declare value variable
		innerPrefix := strings.Repeat("    ", indent+1)
		if ast.IsStructArrayType(arrType) {
			out.WriteString(fmt.Sprintf("%s%s %s = *(%s*)dex_array_struct_get(%s, %s);\n",
				innerPrefix, elemCType, s.ValueVar, elemCType, arrExpr, idxVar))
		} else {
			out.WriteString(fmt.Sprintf("%s%s %s = %s->data[%s];\n",
				innerPrefix, elemCType, s.ValueVar, arrExpr, idxVar))
		}
		// Register the value variable type
		g.varTypes[s.ValueVar] = elemType
		if elemType == ast.TypeString {
			g.strVars[s.ValueVar] = true
		}
		if ast.IsStructType(elemType) {
			g.structVars[s.ValueVar] = elemType
		}
		// Declare index variable if used
		if s.IndexVar != "" {
			out.WriteString(fmt.Sprintf("%sint %s = %s;\n", innerPrefix, s.IndexVar, idxVar))
			g.varTypes[s.IndexVar] = ast.TypeInt
		}
		for _, stmt := range s.Body {
			g.genStmt(out, stmt, indent+1)
		}
		g.popScope(out, innerPrefix)
		g.isInLoop = savedLoop
		g.loopDepth = savedDepth
		out.WriteString(fmt.Sprintf("%s}\n", prefix))

	case *ast.BreakStmt:
		if g.isInLoop {
			g.emitCleanupInnerScopes(out, prefix, g.loopDepth)
		}
		out.WriteString(fmt.Sprintf("%sbreak;\n", prefix))

	case *ast.ContinueStmt:
		if g.isInLoop {
			g.emitCleanupInnerScopes(out, prefix, g.loopDepth)
		}
		out.WriteString(fmt.Sprintf("%scontinue;\n", prefix))

	case *ast.IncrementStmt:
		out.WriteString(fmt.Sprintf("%s%s++;\n", prefix, s.Name))

	case *ast.DecrementStmt:
		out.WriteString(fmt.Sprintf("%s%s--;\n", prefix, s.Name))

	case *ast.CompoundAssignStmt:
		out.WriteString(fmt.Sprintf("%s%s %s= ", prefix, s.Name, g.cBinOp(s.Op)))
		g.genExpr(out, s.Value)
		out.WriteString(";\n")

	case *ast.IndexAssignStmt:
		arrType := g.typeOfExpr(s.Array)
		if ast.IsStructArrayType(arrType) {
			elemType := ast.ElementType(arrType)
			elemCType := g.cType(elemType)
			out.WriteString(prefix)
			out.WriteString("dex_bounds_check(")
			g.genExpr(out, s.Index)
			out.WriteString(", ")
			g.genExpr(out, s.Array)
			out.WriteString("->len);\n")
			out.WriteString(fmt.Sprintf("%s{ %s _assign_tmp = ", prefix, elemCType))
			g.genExpr(out, s.Value)
			out.WriteString(fmt.Sprintf("; memcpy(dex_array_struct_get("))
			g.genExpr(out, s.Array)
			out.WriteString(", ")
			g.genExpr(out, s.Index)
			out.WriteString(fmt.Sprintf("), &_assign_tmp, sizeof(%s)); }\n", elemCType))
		} else {
			out.WriteString(prefix)
			out.WriteString("dex_bounds_check(")
			g.genExpr(out, s.Index)
			out.WriteString(", ")
			g.genExpr(out, s.Array)
			out.WriteString("->len);\n")
			out.WriteString(prefix)
			g.genExpr(out, s.Array)
			out.WriteString("->data[")
			g.genExpr(out, s.Index)
			out.WriteString("] = ")
			g.genExpr(out, s.Value)
			out.WriteString(";\n")
		}

	case *ast.FieldAssignStmt:
		out.WriteString(prefix)
		g.genExpr(out, s.Object)
		out.WriteString(fmt.Sprintf(".%s = ", s.Field))
		g.genExpr(out, s.Value)
		out.WriteString(";\n")
		// Emit cycle check if debug(cycles) is enabled and field is heap-typed
		if g.usesDebugCycles {
			fieldType := g.typeOfExpr(s.Value)
			if ast.IsHeapType(fieldType) {
				out.WriteString(fmt.Sprintf("%sdex_cycle_check_assign(&", prefix))
				g.genExpr(out, s.Object)
				out.WriteString(", ")
				g.genExpr(out, s.Value)
				out.WriteString(");\n")
			}
		}

	case *ast.BlockStmt:
		out.WriteString(fmt.Sprintf("%s{\n", prefix))
		g.pushScope()
		for _, stmt := range s.Stmts {
			g.genStmt(out, stmt, indent+1)
		}
		g.popScope(out, prefix+"    ")
		out.WriteString(fmt.Sprintf("%s}\n", prefix))

	case *ast.SendStmt:
		valType := g.typeOfExpr(s.Value)
		ctyp := g.cType(valType)
		out.WriteString(fmt.Sprintf("%s{ %s _send_val = ", prefix, ctyp))
		g.genExpr(out, s.Value)
		out.WriteString("; dex_chan_send(")
		if s.Target != nil {
			g.genExpr(out, s.Target)
		} else {
			out.WriteString("_ch")
		}
		out.WriteString(", &_send_val); }\n")
	}
}

// genForInit generates the init part of a for loop (no trailing semicolon).
func (g *Generator) genForInit(out *strings.Builder, stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		g.varTypes[s.Name] = s.Type
		if s.Type == ast.TypeString {
			g.strVars[s.Name] = true
		}
		if ast.IsArrayType(s.Type) {
			g.arrVars[s.Name] = s.Type
		}
		out.WriteString(fmt.Sprintf("%s %s = ", g.cType(s.Type), s.Name))
		g.genExpr(out, s.Value)
	case *ast.AssignStmt:
		out.WriteString(fmt.Sprintf("%s = ", s.Name))
		g.genExpr(out, s.Value)
	}
}

// genForPost generates the post part of a for loop (no trailing semicolon).
func (g *Generator) genForPost(out *strings.Builder, stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.IncrementStmt:
		out.WriteString(fmt.Sprintf("%s++", s.Name))
	case *ast.DecrementStmt:
		out.WriteString(fmt.Sprintf("%s--", s.Name))
	case *ast.CompoundAssignStmt:
		out.WriteString(fmt.Sprintf("%s %s= ", s.Name, g.cBinOp(s.Op)))
		g.genExpr(out, s.Value)
	case *ast.AssignStmt:
		out.WriteString(fmt.Sprintf("%s = ", s.Name))
		g.genExpr(out, s.Value)
	}
}

// exprToString renders an expression to a string (for use in foreach).
func (g *Generator) exprToString(expr ast.Expr) string {
	var buf strings.Builder
	g.genExpr(&buf, expr)
	return buf.String()
}

// isNewAlloc returns true if the expression produces a +1 ref (new allocation).
// Variable references are borrowed (not +1).
func (g *Generator) isNewAlloc(expr ast.Expr) bool {
	switch expr.(type) {
	case *ast.Ident:
		return false // borrowed reference
	case *ast.StringLit:
		return true // dex_string_from_lit produces +1
	case *ast.CallExpr:
		return true // function calls produce +1
	case *ast.BinaryExpr:
		return true // concat produces +1
	default:
		return true
	}
}

func (g *Generator) genExpr(out *strings.Builder, expr ast.Expr) {
	switch e := expr.(type) {
	case *ast.IntLit:
		out.WriteString(fmt.Sprintf("%d", e.Value))

	case *ast.CharLit:
		switch e.Value {
		case '\'':
			out.WriteString("'\\''")
		case '\\':
			out.WriteString("'\\\\'")
		case '\n':
			out.WriteString("'\\n'")
		case '\t':
			out.WriteString("'\\t'")
		default:
			out.WriteString(fmt.Sprintf("'%c'", e.Value))
		}

	case *ast.FloatLit:
		out.WriteString(fmt.Sprintf("%g", e.Value))

	case *ast.BoolLit:
		if e.Value {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}

	case *ast.StringLit:
		out.WriteString(fmt.Sprintf("dex_string_from_lit(%q)", e.Value))

	case *ast.Ident:
		out.WriteString(e.Name)

	case *ast.BinaryExpr:
		// Check if this is a string operation
		if g.isStringExpr(e.Left) || g.isStringExpr(e.Right) {
			switch e.Op {
			case ast.BinAdd:
				out.WriteString("dex_str_concat(")
				g.genExpr(out, e.Left)
				out.WriteString(", ")
				g.genExpr(out, e.Right)
				out.WriteString(")")
				return
			case ast.BinEq, ast.BinStrictEq:
				out.WriteString("(strcmp(")
				g.genStringData(out, e.Left)
				out.WriteString(", ")
				g.genStringData(out, e.Right)
				out.WriteString(") == 0)")
				return
			case ast.BinNeq, ast.BinStrictNeq:
				out.WriteString("(strcmp(")
				g.genStringData(out, e.Left)
				out.WriteString(", ")
				g.genStringData(out, e.Right)
				out.WriteString(") != 0)")
				return
			}
		}

		// Cross-numeric operations: cast narrower operand to wider type
		if e.HasMixedTypes {
			widerType := g.widerNumericType(e.LeftType, e.RightType)
			castType := g.cType(widerType)
			out.WriteString("(")
			if e.LeftType != widerType {
				out.WriteString(fmt.Sprintf("(%s)", castType))
			}
			g.genExpr(out, e.Left)
			out.WriteString(fmt.Sprintf(" %s ", g.cBinOp(e.Op)))
			if e.Op == ast.BinDiv || e.Op == ast.BinMod {
				out.WriteString(g.nonzeroCheckFunc(widerType) + "(")
			}
			if e.RightType != widerType {
				out.WriteString(fmt.Sprintf("(%s)", castType))
			}
			g.genExpr(out, e.Right)
			if e.Op == ast.BinDiv || e.Op == ast.BinMod {
				out.WriteString(")")
			}
			out.WriteString(")")
			return
		}

		out.WriteString("(")
		g.genExpr(out, e.Left)
		out.WriteString(fmt.Sprintf(" %s ", g.cBinOp(e.Op)))
		if e.Op == ast.BinDiv || e.Op == ast.BinMod {
			rightType := g.typeOfExpr(e.Right)
			out.WriteString(g.nonzeroCheckFunc(rightType) + "(")
			g.genExpr(out, e.Right)
			out.WriteString(")")
		} else {
			g.genExpr(out, e.Right)
		}
		out.WriteString(")")

	case *ast.UnaryExpr:
		out.WriteString("(")
		switch e.Op {
		case ast.UnaryNeg:
			out.WriteString("-")
		case ast.UnaryNot:
			out.WriteString("!")
		}
		g.genExpr(out, e.Operand)
		out.WriteString(")")

	case *ast.IndexExpr:
		// Check if this is a struct array index
		arrType := g.typeOfExpr(e.Array)
		if ast.IsStructArrayType(arrType) {
			elemType := ast.ElementType(arrType)
			elemCType := g.cType(elemType)
			out.WriteString(fmt.Sprintf("(dex_bounds_check("))
			g.genExpr(out, e.Index)
			out.WriteString(", ")
			g.genExpr(out, e.Array)
			out.WriteString(fmt.Sprintf("->len), *(%s*)dex_array_struct_get(", elemCType))
			g.genExpr(out, e.Array)
			out.WriteString(", ")
			g.genExpr(out, e.Index)
			out.WriteString("))")
		} else {
			out.WriteString("(dex_bounds_check(")
			g.genExpr(out, e.Index)
			out.WriteString(", ")
			g.genExpr(out, e.Array)
			out.WriteString("->len), ")
			g.genExpr(out, e.Array)
			out.WriteString("->data[")
			g.genExpr(out, e.Index)
			out.WriteString("])")
		}

	case *ast.ArrayLitExpr:
		// Non-let context — shouldn't normally happen since checker ensures array literals
		// are used in let statements, but handle defensively
		out.WriteString("/* array literal */")

	case *ast.StructLitExpr:
		cName := "Dex_" + e.Name
		out.WriteString(fmt.Sprintf("(%s){ ", cName))
		for i, fn := range e.FieldNames {
			if i > 0 {
				out.WriteString(", ")
			}
			out.WriteString(fmt.Sprintf(".%s = ", fn))
			g.genExpr(out, e.FieldValues[i])
		}
		out.WriteString(" }")

	case *ast.FieldAccessExpr:
		g.genExpr(out, e.Object)
		out.WriteString(fmt.Sprintf(".%s", e.Field))

	case *ast.CallExpr:
		g.genCallExpr(out, e)

	case *ast.SpawnExpr:
		g.genSpawnExpr(out, e)

	case *ast.ChannelExpr:
		ctyp := g.cType(e.ElemType)
		out.WriteString(fmt.Sprintf("dex_chan_new(sizeof(%s), 64)", ctyp))

	case *ast.ReceiveExpr:
		// receive in expression context — should typically be handled by LetStmt
		// Generate a temp variable
		srcType := g.typeOfExpr(e.Source)
		var elemType ast.Type
		if ast.IsChanType(srcType) {
			elemType = ast.ChanElemType(srcType)
		} else if ast.IsTaskType(srcType) {
			elemType = ast.TaskReturnType(srcType)
		} else {
			elemType = ast.TypeInt
		}
		ctyp := g.cType(elemType)
		// This is for nested expressions. For let statements, genStmt handles it specially.
		out.WriteString(fmt.Sprintf("({ %s _recv_tmp; dex_chan_recv(", ctyp))
		g.genExpr(out, e.Source)
		out.WriteString(", &_recv_tmp); _recv_tmp; })")
	}
}

// genStringData generates the raw C string (->data) for use in strcmp etc.
func (g *Generator) genStringData(out *strings.Builder, expr ast.Expr) {
	if _, ok := expr.(*ast.StringLit); ok {
		// String literals: just emit the C string literal directly for strcmp
		out.WriteString(fmt.Sprintf("%q", expr.(*ast.StringLit).Value))
		return
	}
	g.genExpr(out, expr)
	out.WriteString("->data")
}

func (g *Generator) genCallExpr(out *strings.Builder, e *ast.CallExpr) {
	// User module call: emit prefixed function name
	if e.Module != "" && g.userModules[e.Module] {
		out.WriteString(e.Module + "_" + e.Name)
		out.WriteString("(")
		for i, arg := range e.Args {
			if i > 0 {
				out.WriteString(", ")
			}
			g.genExpr(out, arg)
		}
		out.WriteString(")")
		return
	}

	// Special case: fmt.print — polymorphic print for any primitive type
	if e.Module == "fmt" && e.Name == "print" {
		argType := g.typeOfExpr(e.Args[0])
		var fmtStr string
		switch argType {
		case ast.TypeChar:
			fmtStr = "%c"
		case ast.TypeInt:
			fmtStr = "%d"
		case ast.TypeLong:
			fmtStr = "%ld"
		case ast.TypeDouble:
			fmtStr = "%f"
		case ast.TypeString:
			out.WriteString("printf(\"%s\\n\", ")
			g.genExpr(out, e.Args[0])
			out.WriteString("->data)")
			return
		case ast.TypeBool:
			// Print bools as "true"/"false"
			out.WriteString("printf(\"%s\\n\", ")
			out.WriteString("(")
			g.genExpr(out, e.Args[0])
			out.WriteString(") ? \"true\" : \"false\")")
			return
		default:
			fmtStr = "%d"
		}
		out.WriteString(fmt.Sprintf("printf(\"%s\\n\", ", fmtStr))
		g.genExpr(out, e.Args[0])
		out.WriteString(")")
		return
	}
	// json.stringify(array) — special codegen (returns const char*, needs wrapping)
	if e.Module == "json" && e.Name == "stringify" {
		argIdent, ok := e.Args[0].(*ast.Ident)
		if ok {
			arrType := g.arrVars[argIdent.Name]
			if ast.IsStructArrayType(arrType) {
				// Struct array stringify: use dex_json_set_struct_arr with empty obj
				elemType := ast.ElementType(arrType)
				elemCType := g.cType(elemType)
				def := ast.GetStructDef(elemType)
				out.WriteString(fmt.Sprintf("dex_string_from_cstr(dex_json_set_struct_arr(\"{}\", \"_\", %s, sizeof(%s), %d, ", argIdent.Name, elemCType, len(def.Fields)))
				out.WriteString("(DexStructFieldDesc[]){ ")
				for i, f := range def.Fields {
					if i > 0 {
						out.WriteString(", ")
					}
					fieldOffset := fmt.Sprintf("offsetof(%s, %s)", elemCType, f.Name)
					var fieldKind string
					switch f.Type {
					case ast.TypeInt:
						fieldKind = "0"
					case ast.TypeBool:
						fieldKind = "1"
					case ast.TypeString:
						fieldKind = "2"
					case ast.TypeLong:
						fieldKind = "3"
					case ast.TypeDouble:
						fieldKind = "4"
					default:
						fieldKind = "0"
					}
					out.WriteString(fmt.Sprintf("{\"%s\", %s, %s}", f.Name, fieldOffset, fieldKind))
				}
				out.WriteString(" }))")
			} else {
				var fn string
				switch arrType {
				case ast.TypeArrayInt:
					fn = "dex_json_stringify_int"
				case ast.TypeArrayBool:
					fn = "dex_json_stringify_bool"
				case ast.TypeArrayString:
					fn = "dex_json_stringify_str"
				case ast.TypeArrayLong:
					fn = "dex_json_stringify_long"
				case ast.TypeArrayDouble:
					fn = "dex_json_stringify_double"
				case ast.TypeArrayChar:
					fn = "dex_json_stringify_char"
				}
				out.WriteString(fmt.Sprintf("dex_string_from_cstr(%s(%s))", fn, argIdent.Name))
			}
		}
		return
	}

	// json.setArray(obj, key, array) — special codegen
	if e.Module == "json" && e.Name == "setArray" {
		argIdent, ok := e.Args[2].(*ast.Ident)
		if ok {
			arrType := g.arrVars[argIdent.Name]
			if ast.IsStructArrayType(arrType) {
				// Struct array: use dex_json_set_struct_arr
				elemType := ast.ElementType(arrType)
				elemCType := g.cType(elemType)
				def := ast.GetStructDef(elemType)
				out.WriteString("dex_string_from_cstr(dex_json_set_struct_arr(")
				g.genExpr(out, e.Args[0])
				out.WriteString("->data, ")
				g.genExpr(out, e.Args[1])
				out.WriteString("->data, ")
				out.WriteString(argIdent.Name)
				out.WriteString(fmt.Sprintf(", sizeof(%s), %d, ", elemCType, len(def.Fields)))
				out.WriteString("(DexStructFieldDesc[]){ ")
				for i, f := range def.Fields {
					if i > 0 {
						out.WriteString(", ")
					}
					fieldOffset := fmt.Sprintf("offsetof(%s, %s)", elemCType, f.Name)
					var fieldKind string
					switch f.Type {
					case ast.TypeInt:
						fieldKind = "0"
					case ast.TypeBool:
						fieldKind = "1"
					case ast.TypeString:
						fieldKind = "2"
					case ast.TypeLong:
						fieldKind = "3"
					case ast.TypeDouble:
						fieldKind = "4"
					default:
						fieldKind = "0"
					}
					out.WriteString(fmt.Sprintf("{\"%s\", %s, %s}", f.Name, fieldOffset, fieldKind))
				}
				out.WriteString(" }))")
			} else {
				var fn string
				switch arrType {
				case ast.TypeArrayInt:
					fn = "dex_json_set_arr_int"
				case ast.TypeArrayBool:
					fn = "dex_json_set_arr_bool"
				case ast.TypeArrayString:
					fn = "dex_json_set_arr_str"
				case ast.TypeArrayLong:
					fn = "dex_json_set_arr_long"
				case ast.TypeArrayDouble:
					fn = "dex_json_set_arr_double"
				case ast.TypeArrayChar:
					fn = "dex_json_set_arr_char"
				}
				// Bridge: extract ->data for string args, wrap result in dex_string_from_cstr
				out.WriteString(fmt.Sprintf("dex_string_from_cstr(%s(", fn))
				g.genExpr(out, e.Args[0])
				out.WriteString("->data, ")
				g.genExpr(out, e.Args[1])
				out.WriteString("->data, ")
				out.WriteString(argIdent.Name)
				out.WriteString("))")
			}
		}
		return
	}

	// json.set(obj, key, value) — polymorphic: dispatch by value type
	if e.Module == "json" && e.Name == "set" {
		valType := g.typeOfExpr(e.Args[2])
		// Handle struct array: serialize to JSON array of objects
		if ast.IsStructArrayType(valType) {
			elemType := ast.ElementType(valType)
			elemCType := g.cType(elemType)
			def := ast.GetStructDef(elemType)
			// Generate inline serialization using dex_json_set_arr_struct helper pattern
			out.WriteString("dex_string_from_cstr(dex_json_set_struct_arr(")
			g.genExpr(out, e.Args[0])
			out.WriteString("->data, ")
			g.genExpr(out, e.Args[1])
			out.WriteString("->data, ")
			g.genExpr(out, e.Args[2])
			out.WriteString(fmt.Sprintf(", sizeof(%s), %d, ", elemCType, len(def.Fields)))
			// Emit field descriptors as a compound literal
			out.WriteString(fmt.Sprintf("(DexStructFieldDesc[]){ "))
			for i, f := range def.Fields {
				if i > 0 {
					out.WriteString(", ")
				}
				fieldOffset := fmt.Sprintf("offsetof(%s, %s)", elemCType, f.Name)
				var fieldKind string
				switch f.Type {
				case ast.TypeInt:
					fieldKind = "0"
				case ast.TypeBool:
					fieldKind = "1"
				case ast.TypeString:
					fieldKind = "2"
				case ast.TypeLong:
					fieldKind = "3"
				case ast.TypeDouble:
					fieldKind = "4"
				default:
					fieldKind = "0"
				}
				out.WriteString(fmt.Sprintf("{\"%s\", %s, %s}", f.Name, fieldOffset, fieldKind))
			}
			out.WriteString(" }))")
			return
		}
		// Handle primitive arrays with json.set
		if ast.IsArrayType(valType) {
			argIdent, ok := e.Args[2].(*ast.Ident)
			if ok {
				arrType := g.arrVars[argIdent.Name]
				var setArrFn string
				switch arrType {
				case ast.TypeArrayInt:
					setArrFn = "dex_json_set_arr_int"
				case ast.TypeArrayBool:
					setArrFn = "dex_json_set_arr_bool"
				case ast.TypeArrayString:
					setArrFn = "dex_json_set_arr_str"
				case ast.TypeArrayLong:
					setArrFn = "dex_json_set_arr_long"
				case ast.TypeArrayDouble:
					setArrFn = "dex_json_set_arr_double"
				case ast.TypeArrayChar:
					setArrFn = "dex_json_set_arr_char"
				}
				out.WriteString(fmt.Sprintf("dex_string_from_cstr(%s(", setArrFn))
				g.genExpr(out, e.Args[0])
				out.WriteString("->data, ")
				g.genExpr(out, e.Args[1])
				out.WriteString("->data, ")
				out.WriteString(argIdent.Name)
				out.WriteString("))")
			}
			return
		}
		var fn string
		switch valType {
		case ast.TypeInt:
			fn = "dex_json_set_int"
		case ast.TypeBool:
			fn = "dex_json_set_bool"
		case ast.TypeLong:
			fn = "dex_json_set_long"
		case ast.TypeDouble:
			fn = "dex_json_set_double"
		default:
			fn = "dex_json_set"
		}
		// Bridge: string args need ->data, result needs wrapping
		out.WriteString(fmt.Sprintf("dex_string_from_cstr(%s(", fn))
		g.genExpr(out, e.Args[0])
		out.WriteString("->data, ")
		g.genExpr(out, e.Args[1])
		out.WriteString("->data, ")
		// For the value arg: if string type, extract ->data
		if valType == ast.TypeString {
			g.genExpr(out, e.Args[2])
			out.WriteString("->data")
		} else {
			g.genExpr(out, e.Args[2])
		}
		out.WriteString("))")
		return
	}

	// json.new() — returns const char*, wrap
	if e.Module == "json" && e.Name == "new" {
		out.WriteString("dex_string_from_cstr(dex_json_new())")
		return
	}

	// db.col(rows, col) — polymorphic: dispatch by resolved return type
	if e.Module == "db" && e.Name == "col" {
		var fn string
		switch e.ResolvedType {
		case ast.TypeString:
			fn = "dex_db_col_str"
		case ast.TypeBool:
			fn = "dex_db_col_bool"
		case ast.TypeDouble:
			fn = "dex_db_col_double"
		default:
			fn = "dex_db_col_int"
		}
		if e.ResolvedType == ast.TypeString {
			out.WriteString(fmt.Sprintf("dex_string_from_cstr(%s(", fn))
			g.genExpr(out, e.Args[0])
			out.WriteString(", ")
			g.genExpr(out, e.Args[1])
			out.WriteString("))")
		} else {
			out.WriteString(fn + "(")
			g.genExpr(out, e.Args[0])
			out.WriteString(", ")
			g.genExpr(out, e.Args[1])
			out.WriteString(")")
		}
		return
	}

	if e.Module == "http" && e.Name == "route" {
		// route("GET", "/path", handler) -> dex_route("GET", "/path", handler_name)
		// Resolve handler name
		var handlerName string
		switch h := e.Args[2].(type) {
		case *ast.StringLit:
			handlerName = h.Value
		case *ast.Ident:
			handlerName = h.Name
		case *ast.CallExpr:
			handlerName = h.Name
			if h.Module != "" && g.userModules[h.Module] {
				handlerName = h.Module + "_" + h.Name
			}
		}
		// Generate a wrapper that returns Dex_HttpResponse for the route handler
		emitName := handlerName
		if fn, ok := g.funcs[handlerName]; ok {
			// Check if handler already returns HttpResponse — no wrapper needed
			httpRespType, hasHttpResp := ast.LookupStructType("HttpResponse")
			if hasHttpResp && fn.ReturnType == httpRespType {
				// Handler already returns Dex_HttpResponse, use directly
			} else {
				wrapperName := fmt.Sprintf("_dex_route_wrap_%d", g.routeWrapperCount)
				g.routeWrapperCount++
				var w strings.Builder
				w.WriteString(fmt.Sprintf("Dex_HttpResponse %s(void) {\n", wrapperName))
				if fn.ReturnType == ast.TypeString {
					// String handler: wrap as {200, result, "application/json"}
					w.WriteString(fmt.Sprintf("    DexString* _val = %s();\n", handlerName))
					w.WriteString("    return (Dex_HttpResponse){200, _val, dex_string_from_cstr(\"application/json\")};\n")
				} else {
					// Primitive handler: convert to string, then wrap
					var fmtSpec string
					switch fn.ReturnType {
					case ast.TypeInt:
						fmtSpec = "%d"
					case ast.TypeLong:
						fmtSpec = "%ld"
					case ast.TypeDouble:
						fmtSpec = "%f"
					case ast.TypeBool:
						fmtSpec = "%s"
					default:
						fmtSpec = "%d"
					}
					w.WriteString(fmt.Sprintf("    %s _val = %s();\n", g.cType(fn.ReturnType), handlerName))
					w.WriteString("    char _buf[64];\n")
					if fn.ReturnType == ast.TypeBool {
						w.WriteString("    snprintf(_buf, sizeof(_buf), \"%s\", _val ? \"true\" : \"false\");\n")
					} else {
						w.WriteString(fmt.Sprintf("    snprintf(_buf, sizeof(_buf), \"%s\", _val);\n", fmtSpec))
					}
					w.WriteString("    return (Dex_HttpResponse){200, dex_string_from_cstr(_buf), dex_string_from_cstr(\"application/json\")};\n")
				}
				w.WriteString("}\n")
				g.spawnWrappers.WriteString(w.String())
				emitName = wrapperName
			}
		}
		out.WriteString("dex_route(")
		g.genStringArg(out, e.Args[0])
		out.WriteString(", ")
		g.genStringArg(out, e.Args[1])
		out.WriteString(", ")
		out.WriteString(emitName)
		out.WriteString(")")
		return
	}

	// HTTP client functions — bridge string args and wrap string returns
	if e.Module == "http" {
		switch e.Name {
		case "get":
			if len(e.Args) == 2 {
				out.WriteString("dex_http_get_h(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(")")
			} else {
				out.WriteString("dex_http_get(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(")")
			}
			return
		case "post":
			if len(e.Args) == 3 {
				out.WriteString("dex_http_post_h(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[2])
				out.WriteString(")")
			} else {
				out.WriteString("dex_http_post(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(")")
			}
			return
		case "put":
			if len(e.Args) == 3 {
				out.WriteString("dex_http_put_h(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[2])
				out.WriteString(")")
			} else {
				out.WriteString("dex_http_put(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(")")
			}
			return
		case "patch":
			if len(e.Args) == 3 {
				out.WriteString("dex_http_patch_h(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[2])
				out.WriteString(")")
			} else {
				out.WriteString("dex_http_patch(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(")")
			}
			return
		case "delete":
			if len(e.Args) == 2 {
				out.WriteString("dex_http_delete_h(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(")")
			} else {
				out.WriteString("dex_http_delete(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(")")
			}
			return
		case "request":
			out.WriteString("dex_http_request(")
			g.genStringArg(out, e.Args[0])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[1])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[2])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[3])
			out.WriteString(")")
			return
		case "header":
			out.WriteString("dex_string_from_cstr(dex_http_header(")
			g.genStringArg(out, e.Args[0])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[1])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[2])
			out.WriteString("))")
			return
		case "formNew":
			out.WriteString("dex_string_from_cstr(dex_http_form_new())")
			return
		case "formField":
			out.WriteString("dex_string_from_cstr(dex_http_form_field(")
			g.genStringArg(out, e.Args[0])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[1])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[2])
			out.WriteString("))")
			return
		case "formFile":
			out.WriteString("dex_string_from_cstr(dex_http_form_file(")
			g.genStringArg(out, e.Args[0])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[1])
			out.WriteString(", ")
			g.genStringArg(out, e.Args[2])
			out.WriteString("))")
			return
		case "postForm":
			if len(e.Args) == 3 {
				out.WriteString("dex_http_post_form_h(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[2])
				out.WriteString(")")
			} else {
				out.WriteString("dex_http_post_form(")
				g.genStringArg(out, e.Args[0])
				out.WriteString(", ")
				g.genStringArg(out, e.Args[1])
				out.WriteString(")")
			}
			return
		case "listen":
			out.WriteString("dex_listen(")
			g.genExpr(out, e.Args[0])
			out.WriteString(")")
			return
		}
	}

	// Built-in: close(channel)
	if e.Module == "" && e.Name == "close" {
		out.WriteString("dex_chan_close(")
		g.genExpr(out, e.Args[0])
		out.WriteString(")")
		return
	}

	// Built-in: assert(condition)
	if e.Module == "" && e.Name == "assert" {
		out.WriteString("if (!(")
		g.genExpr(out, e.Args[0])
		out.WriteString(")) { fprintf(stderr, \"FAIL: assert failed\\n\"); exit(1); }")
		return
	}

	// Check if this is an array method call (e.Module is a variable name)
	if e.Module != "" {
		if arrType, ok := g.arrVars[e.Module]; ok {
			if ast.IsStructArrayType(arrType) {
				// Struct array methods
				elemType := ast.ElementType(arrType)
				elemCType := g.cType(elemType)
				switch e.Name {
				case "push":
					out.WriteString(fmt.Sprintf("{ %s _push_tmp = ", elemCType))
					g.genExpr(out, e.Args[0])
					out.WriteString("; ")
					// Retain heap fields before push
					def := ast.GetStructDef(elemType)
					if def != nil {
						for _, f := range def.Fields {
							if ast.IsHeapType(f.Type) {
								out.WriteString(fmt.Sprintf("dex_retain(_push_tmp.%s); ", f.Name))
							}
						}
					}
					out.WriteString(fmt.Sprintf("dex_array_struct_push(%s, &_push_tmp); }", e.Module))
					return
				case "len":
					out.WriteString(fmt.Sprintf("%s->len", e.Module))
					return
				case "pop":
					out.WriteString(fmt.Sprintf("dex_array_struct_pop(%s)", e.Module))
					return
				case "remove":
					out.WriteString(fmt.Sprintf("dex_array_struct_remove(%s, ", e.Module))
					g.genExpr(out, e.Args[0])
					out.WriteString(")")
					return
				case "reverse":
					out.WriteString(fmt.Sprintf("dex_array_struct_reverse(%s)", e.Module))
					return
				}
			} else {
				switch e.Name {
				case "push":
					pushFn := g.arrayPushFunc(arrType)
					out.WriteString(fmt.Sprintf("%s(%s, ", pushFn, e.Module))
					g.genExpr(out, e.Args[0])
					out.WriteString(")")
					return
				case "len":
					out.WriteString(fmt.Sprintf("%s->len", e.Module))
					return
				case "pop":
					popFn := g.arrayPopFunc(arrType)
					out.WriteString(fmt.Sprintf("%s(%s)", popFn, e.Module))
					return
				case "remove":
					removeFn := g.arrayRemoveFunc(arrType)
					out.WriteString(fmt.Sprintf("%s(%s, ", removeFn, e.Module))
					g.genExpr(out, e.Args[0])
					out.WriteString(")")
					return
				case "contains":
					containsFn := g.arrayContainsFunc(arrType)
					out.WriteString(fmt.Sprintf("%s(%s, ", containsFn, e.Module))
					g.genExpr(out, e.Args[0])
					out.WriteString(")")
					return
				case "indexOf":
					indexOfFn := g.arrayIndexOfFunc(arrType)
					out.WriteString(fmt.Sprintf("%s(%s, ", indexOfFn, e.Module))
					g.genExpr(out, e.Args[0])
					out.WriteString(")")
					return
				case "reverse":
					reverseFn := g.arrayReverseFunc(arrType)
					out.WriteString(fmt.Sprintf("%s(%s)", reverseFn, e.Module))
					return
				case "sort":
					sortArg := e.Args[0].(*ast.StringLit).Value
					var sortFn string
					if sortArg == "asc" {
						sortFn = g.arraySortAscFunc(arrType)
					} else {
						sortFn = g.arraySortDescFunc(arrType)
					}
					out.WriteString(fmt.Sprintf("%s(%s)", sortFn, e.Module))
					return
				}
			}
		}
	}

	// Qualified call with CName — look up from stdlib
	if e.Module != "" {
		funcDef, ok := stdlib.LookupFunc(e.Module, e.Name)
		if ok && funcDef.CName != "" {
			// Check if function returns string — needs wrapping
			if funcDef.ReturnType == ast.TypeString {
				out.WriteString("dex_string_from_cstr(")
				out.WriteString(funcDef.CName)
				out.WriteString("(")
				for i, arg := range e.Args {
					if i > 0 {
						out.WriteString(", ")
					}
					g.genStdlibArg(out, arg, funcDef, i)
				}
				out.WriteString("))")
			} else {
				out.WriteString(funcDef.CName)
				out.WriteString("(")
				for i, arg := range e.Args {
					if i > 0 {
						out.WriteString(", ")
					}
					g.genStdlibArg(out, arg, funcDef, i)
				}
				out.WriteString(")")
			}
			return
		}
	}

	// User-defined function call
	out.WriteString(e.Name)
	out.WriteString("(")
	for i, arg := range e.Args {
		if i > 0 {
			out.WriteString(", ")
		}
		g.genExpr(out, arg)
	}
	out.WriteString(")")
}

// genStringArg generates a string argument for a stdlib function, extracting ->data
func (g *Generator) genStringArg(out *strings.Builder, arg ast.Expr) {
	argType := g.typeOfExpr(arg)
	if argType == ast.TypeString {
		if strLit, ok := arg.(*ast.StringLit); ok {
			// String literal — just use C string directly
			out.WriteString(fmt.Sprintf("%q", strLit.Value))
		} else {
			g.genExpr(out, arg)
			out.WriteString("->data")
		}
	} else {
		g.genExpr(out, arg)
	}
}

// genStdlibArg generates an argument for a stdlib function with CName,
// bridging DexString* to const char* when the stdlib expects it.
func (g *Generator) genStdlibArg(out *strings.Builder, arg ast.Expr, funcDef *stdlib.FuncDef, idx int) {
	argType := g.typeOfExpr(arg)
	if argType == ast.TypeString {
		// Stdlib functions with CName expect const char*
		if strLit, ok := arg.(*ast.StringLit); ok {
			out.WriteString(fmt.Sprintf("%q", strLit.Value))
		} else {
			g.genExpr(out, arg)
			out.WriteString("->data")
		}
	} else {
		g.genExpr(out, arg)
	}
}

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
		g.spawnWrappers.WriteString("    return NULL;\n")
		g.spawnWrappers.WriteString("}\n")

		// Generate call site inline (using GCC statement expression)
		retCType := g.cType(e.ReturnType)
		if retCType == "void" {
			retCType = "int" // placeholder for sizeof in fire-and-forget
		}
		out.WriteString(fmt.Sprintf("({ DexChan* _spawn_ch = dex_chan_new(sizeof(%s), 64); ", retCType))
		out.WriteString(fmt.Sprintf("%s* _spawn_ctx = (%s*)malloc(sizeof(%s)); ", ctxType, ctxType, ctxType))
		out.WriteString("_spawn_ctx->_ch = _spawn_ch; ")
		for _, cv := range captured {
			out.WriteString(fmt.Sprintf("_spawn_ctx->%s = %s; ", cv.name, cv.name))
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
		if retType != ast.TypeVoid {
			g.spawnWrappers.WriteString(fmt.Sprintf("    %s _ret = %s(", g.cType(retType), call.Name))
		} else {
			g.spawnWrappers.WriteString(fmt.Sprintf("    %s(", call.Name))
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
		g.spawnWrappers.WriteString("    return NULL;\n")
		g.spawnWrappers.WriteString("}\n")

		// Generate call site
		retCType := g.cType(e.ReturnType)
		if retCType == "void" {
			retCType = "int" // use int for void tasks to have a valid sizeof
		}
		out.WriteString(fmt.Sprintf("({ DexChan* _spawn_ch = dex_chan_new(sizeof(%s), 1); ", retCType))
		out.WriteString(fmt.Sprintf("%s* _spawn_ctx = (%s*)malloc(sizeof(%s)); ", ctxType, ctxType, ctxType))
		out.WriteString("_spawn_ctx->_ch = _spawn_ch; ")
		for i, arg := range call.Args {
			out.WriteString(fmt.Sprintf("_spawn_ctx->_a%d = ", i))
			g.genExpr(out, arg)
			out.WriteString("; ")
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
			g.collectUsedVarsExpr(s.Value, used)
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
	}
}

// genExprNoParen generates an expression without wrapping outer parens,
// used for if/while conditions which already provide parens.
func (g *Generator) genExprNoParen(out *strings.Builder, expr ast.Expr) {
	switch e := expr.(type) {
	case *ast.BinaryExpr:
		// Check string comparison in no-paren context too
		if g.isStringExpr(e.Left) || g.isStringExpr(e.Right) {
			switch e.Op {
			case ast.BinEq, ast.BinStrictEq:
				out.WriteString("strcmp(")
				g.genStringData(out, e.Left)
				out.WriteString(", ")
				g.genStringData(out, e.Right)
				out.WriteString(") == 0")
				return
			case ast.BinNeq, ast.BinStrictNeq:
				out.WriteString("strcmp(")
				g.genStringData(out, e.Left)
				out.WriteString(", ")
				g.genStringData(out, e.Right)
				out.WriteString(") != 0")
				return
			}
		}
		// Cross-numeric operations in no-paren context
		if e.HasMixedTypes {
			widerType := g.widerNumericType(e.LeftType, e.RightType)
			castType := g.cType(widerType)
			if e.LeftType != widerType {
				out.WriteString(fmt.Sprintf("(%s)", castType))
			}
			g.genExpr(out, e.Left)
			out.WriteString(fmt.Sprintf(" %s ", g.cBinOp(e.Op)))
			if e.Op == ast.BinDiv || e.Op == ast.BinMod {
				out.WriteString(g.nonzeroCheckFunc(widerType) + "(")
			}
			if e.RightType != widerType {
				out.WriteString(fmt.Sprintf("(%s)", castType))
			}
			g.genExpr(out, e.Right)
			if e.Op == ast.BinDiv || e.Op == ast.BinMod {
				out.WriteString(")")
			}
			return
		}
		g.genExpr(out, e.Left)
		out.WriteString(fmt.Sprintf(" %s ", g.cBinOp(e.Op)))
		if e.Op == ast.BinDiv || e.Op == ast.BinMod {
			rightType := g.typeOfExpr(e.Right)
			out.WriteString(g.nonzeroCheckFunc(rightType) + "(")
			g.genExpr(out, e.Right)
			out.WriteString(")")
		} else {
			g.genExpr(out, e.Right)
		}
	default:
		g.genExpr(out, expr)
	}
}

// isStringExpr checks if an expression is known to produce a string type.
func (g *Generator) isStringExpr(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.StringLit:
		return true
	case *ast.CallExpr:
		// Polymorphic return type: uses ResolvedType if set
		if e.ResolvedType != 0 {
			return e.ResolvedType == ast.TypeString
		}
		// Check stdlib functions
		if e.Module != "" {
			funcDef, ok := stdlib.LookupFunc(e.Module, e.Name)
			if ok {
				return funcDef.ReturnType == ast.TypeString
			}
			return false
		}
		// User-defined functions
		if fn, ok := g.funcs[e.Name]; ok {
			return fn.ReturnType == ast.TypeString
		}
	case *ast.Ident:
		return g.strVars[e.Name]
	case *ast.IndexExpr:
		// Check if indexing a string array
		if ident, ok := e.Array.(*ast.Ident); ok {
			if arrType, ok := g.arrVars[ident.Name]; ok {
				return arrType == ast.TypeArrayString
			}
		}
		return false
	case *ast.BinaryExpr:
		if e.Op == ast.BinAdd {
			return g.isStringExpr(e.Left) || g.isStringExpr(e.Right)
		}
	case *ast.FieldAccessExpr:
		return g.typeOfExpr(e) == ast.TypeString
	}
	return false
}

func (g *Generator) cType(t ast.Type) string {
	switch t {
	case ast.TypeInt:
		return "int"
	case ast.TypeBool:
		return "_Bool"
	case ast.TypeString:
		return "DexString*"
	case ast.TypeLong:
		return "long"
	case ast.TypeDouble:
		return "double"
	case ast.TypeArrayInt:
		return "DexArrayInt*"
	case ast.TypeArrayBool:
		return "DexArrayBool*"
	case ast.TypeArrayString:
		return "DexArrayString*"
	case ast.TypeArrayLong:
		return "DexArrayLong*"
	case ast.TypeArrayDouble:
		return "DexArrayDouble*"
	case ast.TypeChar:
		return "unsigned char"
	case ast.TypeArrayChar:
		return "DexArrayChar*"
	default:
		if ast.IsStructArrayType(t) {
			return "DexArrayStruct*"
		}
		if ast.IsStructType(t) {
			return "Dex_" + ast.StructName(t)
		}
		if ast.IsChanType(t) || ast.IsTaskType(t) {
			return "DexChan*"
		}
		if ast.IsFuncType(t) {
			return g.funcTypedef(t)
		}
		if ast.IsWeakType(t) {
			return "DexWeakRef*"
		}
		return "void"
	}
}

// funcTypedef returns (and lazily registers) a typedef name for a function pointer type.
func (g *Generator) funcTypedef(t ast.Type) string {
	if name, ok := g.funcTypedefs[t]; ok {
		return name
	}
	g.funcTypedefCnt++
	name := fmt.Sprintf("DexFn_%d", g.funcTypedefCnt)
	g.funcTypedefs[t] = name
	return name
}

func (g *Generator) arrayNewFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_new"
	case ast.TypeArrayBool:
		return "dex_array_bool_new"
	case ast.TypeArrayString:
		return "dex_array_string_new"
	case ast.TypeArrayLong:
		return "dex_array_long_new"
	case ast.TypeArrayDouble:
		return "dex_array_double_new"
	case ast.TypeArrayChar:
		return "dex_array_char_new"
	default:
		return ""
	}
}

func (g *Generator) arrayPushFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_push"
	case ast.TypeArrayBool:
		return "dex_array_bool_push"
	case ast.TypeArrayString:
		return "dex_array_string_push"
	case ast.TypeArrayLong:
		return "dex_array_long_push"
	case ast.TypeArrayDouble:
		return "dex_array_double_push"
	case ast.TypeArrayChar:
		return "dex_array_char_push"
	default:
		return ""
	}
}

func (g *Generator) arrayPopFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_pop"
	case ast.TypeArrayBool:
		return "dex_array_bool_pop"
	case ast.TypeArrayString:
		return "dex_array_string_pop"
	case ast.TypeArrayLong:
		return "dex_array_long_pop"
	case ast.TypeArrayDouble:
		return "dex_array_double_pop"
	case ast.TypeArrayChar:
		return "dex_array_char_pop"
	default:
		return ""
	}
}

func (g *Generator) arrayRemoveFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_remove"
	case ast.TypeArrayBool:
		return "dex_array_bool_remove"
	case ast.TypeArrayString:
		return "dex_array_string_remove"
	case ast.TypeArrayLong:
		return "dex_array_long_remove"
	case ast.TypeArrayDouble:
		return "dex_array_double_remove"
	case ast.TypeArrayChar:
		return "dex_array_char_remove"
	default:
		return ""
	}
}

func (g *Generator) arrayContainsFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_contains"
	case ast.TypeArrayBool:
		return "dex_array_bool_contains"
	case ast.TypeArrayString:
		return "dex_array_string_contains"
	case ast.TypeArrayLong:
		return "dex_array_long_contains"
	case ast.TypeArrayDouble:
		return "dex_array_double_contains"
	case ast.TypeArrayChar:
		return "dex_array_char_contains"
	default:
		return ""
	}
}

func (g *Generator) arrayIndexOfFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_indexOf"
	case ast.TypeArrayBool:
		return "dex_array_bool_indexOf"
	case ast.TypeArrayString:
		return "dex_array_string_indexOf"
	case ast.TypeArrayLong:
		return "dex_array_long_indexOf"
	case ast.TypeArrayDouble:
		return "dex_array_double_indexOf"
	case ast.TypeArrayChar:
		return "dex_array_char_indexOf"
	default:
		return ""
	}
}

func (g *Generator) arrayReverseFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_reverse"
	case ast.TypeArrayBool:
		return "dex_array_bool_reverse"
	case ast.TypeArrayString:
		return "dex_array_string_reverse"
	case ast.TypeArrayLong:
		return "dex_array_long_reverse"
	case ast.TypeArrayDouble:
		return "dex_array_double_reverse"
	case ast.TypeArrayChar:
		return "dex_array_char_reverse"
	default:
		return ""
	}
}

func (g *Generator) arraySortAscFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_sort_asc"
	case ast.TypeArrayString:
		return "dex_array_string_sort_asc"
	case ast.TypeArrayLong:
		return "dex_array_long_sort_asc"
	case ast.TypeArrayDouble:
		return "dex_array_double_sort_asc"
	case ast.TypeArrayChar:
		return "dex_array_char_sort_asc"
	default:
		return ""
	}
}

func (g *Generator) arraySortDescFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_sort_desc"
	case ast.TypeArrayString:
		return "dex_array_string_sort_desc"
	case ast.TypeArrayLong:
		return "dex_array_long_sort_desc"
	case ast.TypeArrayDouble:
		return "dex_array_double_sort_desc"
	case ast.TypeArrayChar:
		return "dex_array_char_sort_desc"
	default:
		return ""
	}
}

// structArrayCleanupFunc returns the cleanup function name for a struct type, or "NULL" if no cleanup needed.
func (g *Generator) structArrayCleanupFunc(elemType ast.Type) string {
	def := ast.GetStructDef(elemType)
	if def == nil {
		return "NULL"
	}
	for _, f := range def.Fields {
		if ast.NeedsRelease(f.Type) {
			return "dex_cleanup_" + def.Name
		}
	}
	return "NULL"
}

// structArrayNeedsCleanup returns true if the struct has heap fields that need cleanup.
func (g *Generator) structArrayNeedsCleanup(elemType ast.Type) bool {
	def := ast.GetStructDef(elemType)
	if def == nil {
		return false
	}
	for _, f := range def.Fields {
		if ast.NeedsRelease(f.Type) {
			return true
		}
	}
	return false
}

// typeOfExpr returns the type of an expression based on available information.
func (g *Generator) typeOfExpr(expr ast.Expr) ast.Type {
	switch e := expr.(type) {
	case *ast.CharLit:
		return ast.TypeChar
	case *ast.IntLit:
		return ast.TypeInt
	case *ast.FloatLit:
		return ast.TypeDouble
	case *ast.BoolLit:
		return ast.TypeBool
	case *ast.StringLit:
		return ast.TypeString
	case *ast.Ident:
		if t, ok := g.varTypes[e.Name]; ok {
			return t
		}
		// Check if it's a function reference
		if fn, ok := g.funcs[e.Name]; ok {
			var paramTypes []ast.Type
			for _, p := range fn.Params {
				paramTypes = append(paramTypes, p.Type)
			}
			return ast.FuncTypeOf(paramTypes, fn.ReturnType)
		}
	case *ast.CallExpr:
		// Polymorphic return type: db.col and http client functions use ResolvedType
		if e.ResolvedType != 0 {
			return e.ResolvedType
		}
		if e.Module != "" && g.userModules[e.Module] {
			if fn, ok := g.funcs[e.Module+"_"+e.Name]; ok {
				return fn.ReturnType
			}
		}
		if e.Module != "" {
			funcDef, ok := stdlib.LookupFunc(e.Module, e.Name)
			if ok {
				return funcDef.ReturnType
			}
		}
		if fn, ok := g.funcs[e.Name]; ok {
			return fn.ReturnType
		}
		// Check if calling through a function-typed variable
		if t, ok := g.varTypes[e.Name]; ok && ast.IsFuncType(t) {
			return ast.FuncTypeReturn(t)
		}
	case *ast.BinaryExpr:
		if e.Op == ast.BinAdd && g.isStringExpr(e.Left) {
			return ast.TypeString
		}
		return g.typeOfExpr(e.Left)
	case *ast.UnaryExpr:
		return g.typeOfExpr(e.Operand)
	case *ast.IndexExpr:
		if ident, ok := e.Array.(*ast.Ident); ok {
			if arrType, ok := g.arrVars[ident.Name]; ok {
				return ast.ElementType(arrType)
			}
		}
	case *ast.StructLitExpr:
		if t, ok := ast.LookupStructType(e.Name); ok {
			return t
		}
	case *ast.FieldAccessExpr:
		objType := g.typeOfExpr(e.Object)
		if ast.IsStructType(objType) {
			def := ast.GetStructDef(objType)
			if def != nil {
				for _, f := range def.Fields {
					if f.Name == e.Field {
						return f.Type
					}
				}
			}
		}
	case *ast.SpawnExpr:
		return ast.TaskTypeOf(e.ReturnType)
	case *ast.ChannelExpr:
		return ast.ChanTypeOf(e.ElemType)
	case *ast.ReceiveExpr:
		srcType := g.typeOfExpr(e.Source)
		if ast.IsChanType(srcType) {
			return ast.ChanElemType(srcType)
		}
		if ast.IsTaskType(srcType) {
			return ast.TaskReturnType(srcType)
		}
	}
	return ast.TypeVoid
}

// widerNumericType returns the wider of two numeric types.
// Widening order: int → long → double
func (g *Generator) widerNumericType(a, b ast.Type) ast.Type {
	rank := map[ast.Type]int{
		ast.TypeChar:   0,
		ast.TypeInt:    1,
		ast.TypeLong:   2,
		ast.TypeDouble: 3,
	}
	if rank[a] >= rank[b] {
		return a
	}
	return b
}

func (g *Generator) nonzeroCheckFunc(t ast.Type) string {
	switch t {
	case ast.TypeLong:
		return "dex_check_nonzero_long"
	case ast.TypeDouble:
		return "dex_check_nonzero_double"
	default:
		return "dex_check_nonzero_int"
	}
}

func (g *Generator) cBinOp(op ast.BinOp) string {
	switch op {
	case ast.BinAdd:
		return "+"
	case ast.BinSub:
		return "-"
	case ast.BinMul:
		return "*"
	case ast.BinDiv:
		return "/"
	case ast.BinMod:
		return "%"
	case ast.BinEq:
		return "=="
	case ast.BinNeq:
		return "!="
	case ast.BinStrictEq:
		return "=="
	case ast.BinStrictNeq:
		return "!="
	case ast.BinLt:
		return "<"
	case ast.BinGt:
		return ">"
	case ast.BinLte:
		return "<="
	case ast.BinGte:
		return ">="
	case ast.BinAnd:
		return "&&"
	case ast.BinOr:
		return "||"
	default:
		return "?"
	}
}
