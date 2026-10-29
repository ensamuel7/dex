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
	// slot names the C variable holding this variable's index on the cleanup
	// stack, for a variable that is assigned again after being registered: the
	// assignment has to point the entry at the new value. Empty when the
	// variable is written once, which needs no slot and so declares none.
	slot string
	// fieldSlots is the same thing per heap field, for a struct local: each
	// field is registered separately, so each needs its own slot for the field
	// assignment to write through.
	fieldSlots map[string]string
}

// A scopeFrame is one C block's worth of cleanup: the variables it releases on
// the way out, and the name of the C variable holding the cleanup stack's
// height when it started. The height is what the scope restores on exit, and
// what an unwind releases down to; it is only emitted for a scope that actually
// registers something.
type scopeFrame struct {
	vars []scopeVar
	mark string
}

type Generator struct {
	usesBool          bool
	usesString        bool
	usesArray         bool
	usesAssert        bool
	usesConcurrency   bool
	usesSafety        bool
	usesRefcount      bool
	usesHandle        bool
	usesExternal      bool
	usesArena         bool
	usesDebugCycles   bool
	usesWeakRef       bool
	usesOptional      bool
	usesExceptions    bool
	usesStringMethods bool
	usesMap           bool
	usesEventLoop     bool
	usesStringBuilder bool
	usesOsArgs        bool

	// memCheck emits the debug memory checker: a widened object header, a live
	// object registry, and a DEX_AT marker ahead of every statement so an
	// allocation can be attributed to the .dx line that made it. Set from
	// DEX_MEM_CHECK=1, off in every normal build.
	memCheck bool

	// profile emits the memory profiler: the same DEX_AT statement markers,
	// with a per-statement allocation table and a timeline of live bytes in
	// place of the checker's registry and quarantine. Set by `dex profile` or
	// DEX_PROFILE=1. It and memCheck are never both on.
	profile bool

	tryCatchCounter int
	switchCounter   int

	importedModules map[string]*stdlib.Module
	userModules     map[string]bool
	structModules   map[string]string // structName -> moduleName

	funcs      map[string]*ast.Function
	strVars    map[string]bool     // variables known to be string type
	arrVars    map[string]ast.Type // variables known to be array type (name -> array type)
	structVars map[string]ast.Type // variables known to be struct type
	mapVars    map[string]ast.Type // variables known to be map type (name -> map type)
	sbVars     map[string]bool     // variables known to be StringBuilder type
	varTypes   map[string]ast.Type // all variable types for this function scope

	printCounter      int             // unique counter for print temp variables
	foreachCounter    int             // unique counter for foreach loop variables
	spawnCounter      int             // unique counter for spawn wrapper functions
	spawnWrappers     strings.Builder // collected wrapper functions emitted before main
	jsonEncoders      map[ast.Type]string
	jsonEncProtos     strings.Builder
	jsonEncBodies     strings.Builder
	routeWrapperCount int             // unique counter for route handler wrappers
	matchCounter      int             // unique counter for match expression temp vars
	lambdaCounter     int             // unique counter for lambda/closure functions
	lambdaWrappers    strings.Builder // collected lambda functions emitted before main

	funcTypedefs    map[ast.Type]string // function type → typedef name (e.g. DexFn_1)
	usesClosure     bool                // any function value appears, so closures are needed
	usesDefer       bool                // any defer appears, so the defer stack is needed
	closureThunks   map[string]string   // function name → uniform-convention thunk
	methodWrappers  map[string]string   // flattened method name → method-value wrapper
	methodEnvTypes  map[ast.Type]bool   // struct types with a receiver environment emitted
	routeAdapters   map[string]string   // handler shape → response adapter
	closureTypes    strings.Builder     // environment typedefs
	closureWrappers strings.Builder     // thunks, wrappers, and environment destructors
	funcTypedefCnt  int                 // counter for unique typedef names

	// Scope tracking for cleanup
	scopeStack []scopeFrame
	// cleanupMarks names the C variables that hold cleanup-stack heights.
	cleanupMarks int
	// stmtTempsRegistered records whether the statement being generated put any
	// temporary on the cleanup stack, which is what decides if it needs a mark.
	stmtTempsRegistered bool
	// stmtTempMark is the mark for a statement whose hoists are emitted without
	// a block around them — a `let`, whose variable has to stay in scope.
	stmtTempMark string
	// tryDepth is how many exception frames the statement being generated sits
	// inside, counting only try bodies: a catch or finally body runs after its
	// own frame has been popped. A return or a break leaving those frames has
	// to pop them, or the frame stack walks off its end.
	tryDepth int
	// loopTryDepth is tryDepth as it stood when the enclosing loop started, so
	// a break knows how many frames it is leaving.
	loopTryDepth   int
	currentFn      *ast.Function       // current function being generated
	isInLoop       bool                // whether we're inside a loop body
	loopDepth      int                 // scope depth at loop entry (for break/continue cleanup)
	varAnnotations map[string][]string // per-variable annotation tracking

	// Type narrowing for optional types in null checks
	narrowedVars  map[string]string   // varName -> narrowed C var name in current scope
	narrowedTypes map[string]ast.Type // varName -> narrowed (non-optional) type in current scope
	narrowStack   []map[string]string // stack of narrowedVars snapshots for scope management

	// Statement-level temp tracking for string concat intermediates
	pendingReleases []string // temp var names to release after current statement
	// stmtPrelude collects declarations hoisted out of the statement being
	// generated, and stmtTemps the temporaries they bind. Together they let an
	// expression that mints a heap value be handed to something that only
	// borrows it — a print, a function argument — and still be released once the
	// statement finishes, instead of leaking.
	stmtPrelude *strings.Builder
	stmtTemps   []scopeVar
	outRaws     map[ast.Expr]string
	outWrapped  map[*ast.CallExpr]bool
	tempCounter int // unique counter for temp names

	// Defer tracking
	// fnHasDefer records that the function being generated contains a defer, so
	// its entry records the defer stack's height and its exits run back down to
	// it. The calls themselves live on that stack at runtime: which ones to run
	// depends on what executed, not on what was written.
	fnHasDefer bool

	// Global variable tracking
	globalLets []ast.LetStmt // module-level let/const declarations
	// inlinedConsts names the globals whose value was written at their
	// declaration, so main() does not try to assign them again.
	inlinedConsts map[string]bool
	globalVars    map[string]ast.Type // global variable name -> type

	// String literals emitted once as immortal statics rather than allocated
	// at every evaluation. litPool maps the literal's value to its C symbol;
	// litOrder keeps the definitions in discovery order so output is stable.
	// See gen_litpool.go.
	litPool  map[string]string
	litOrder []string

	enumAppenders      map[string]bool
	enumAppenderProtos strings.Builder
	enumAppenderBodies strings.Builder
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
		closureThunks:   make(map[string]string),
		methodWrappers:  make(map[string]string),
		methodEnvTypes:  make(map[ast.Type]bool),
		routeAdapters:   make(map[string]string),
		varAnnotations:  make(map[string][]string),
		narrowedVars:    make(map[string]string),
		narrowedTypes:   make(map[string]ast.Type),
		globalVars:      make(map[string]ast.Type),
		inlinedConsts:   make(map[string]bool),
		litPool:         make(map[string]string),
	}
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
		// Signed overflow wraps instead of being undefined. Without this the
		// optimiser is entitled to assume it never happens, so an overflow
		// check written in .dx — x + 1 < x — folds to false at -O3 while the
		// addition itself wraps. Two answers for one expression.
		"-fwrapv",
		// Security hardening
		"-fstack-protector-strong",
		"-D_FORTIFY_SOURCE=2",
		// Format string security warnings
		"-Wformat", "-Wformat-security",
		// Suppress expected noise from static runtime functions
		"-Wno-unused-function",
		// Same, for the string literal pool: a literal whose only use is a
		// comparison compiles to raw bytes rather than an object, so its
		// pooled static is emitted and then never referenced. Both are
		// dead-stripped by the linker flags below.
		"-Wno-unused-variable",
	}
	if runtime.GOOS == "darwin" {
		flags = append(flags, "-Wl,-dead_strip")
	} else if runtime.GOOS == "linux" {
		flags = append(flags,
			"-Wl,--gc-sections",
			// Position-independent executable (default on macOS, explicit on Linux)
			"-fPIE", "-pie",
			// Relocations read-only and bound at load, so a write through a
			// stray pointer cannot redirect a later library call.
			"-Wl,-z,relro", "-Wl,-z,now",
		)
	}
	if g.usesConcurrency {
		flags = append(flags, "-pthread")
	}
	// A string crosses to C as char*, and plenty of byte-oriented C functions
	// are declared unsigned char* or Bytef* — zlib's crc32 is one. The
	// representation is identical and the conversion is well defined, so the
	// warning is noise on every such binding, and there is no unsigned-char
	// type to declare instead.
	//
	// Scoped to a program that declares an external, because that is the only
	// place it can fire: the rest of the generated C is internally consistent.
	// It also costs nothing real — a genuinely wrong declaration is an
	// incompatible-pointer-types error, which stays fatal.
	if g.usesExternal {
		flags = append(flags, "-Wno-pointer-sign", "-lm")
	}
	for _, mod := range g.importedModules {
		flags = append(flags, mod.CFlags...)
	}
	return flags
}

// isStringVar reports whether a receiver name holds a string. A variable
// declared string? is a string inside a null-guarded block, and narrowing
// records that — but strVars only holds what was declared string, so every
// site testing strVars directly missed a narrowed optional. `n.len()` inside
// the guard fell through to a user-function call and emitted a bare len(...),
// and the method's return type was unknown so fmt.println chose %d for a
// DexString*.
func (g *Generator) isStringVar(name string) bool {
	if g.strVars[name] {
		return true
	}
	t, ok := g.narrowedTypes[name]
	return ok && t == ast.TypeString
}

// lookupFunc resolves a qualified call against the modules this program
// imported or declared. It replaces the global stdlib.LookupFunc at every call
// site because an extern module is per-compilation state and is deliberately
// absent from that registry.
func (g *Generator) lookupFunc(module, name string) (*stdlib.FuncDef, bool) {
	mod, ok := g.importedModules[module]
	if !ok {
		return nil, false
	}
	fd, ok := mod.Funcs[name]
	if !ok {
		return nil, false
	}
	return &fd, true
}

// EnableMemCheck turns on the debug memory checker for this generation: the
// object header grows bookkeeping fields, every allocation is registered, and
// each statement is preceded by a marker naming its .dx source position so a
// leak or a double release can be reported against it.
func (g *Generator) EnableMemCheck() { g.memCheck = true }

// EnableProfile turns on the memory profiler for this generation: every
// statement is preceded by a marker naming its .dx position, every function
// restores its caller's marker on the way out, and the runtime tallies the
// bytes each statement allocates and still holds.
func (g *Generator) EnableProfile() { g.profile = true }

// siteMarkers reports whether statements carry DEX_AT markers in this build.
func (g *Generator) siteMarkers() bool { return (g.memCheck || g.profile) && g.usesRefcount }
