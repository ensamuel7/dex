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
	usesBool          bool
	usesString        bool
	usesArray         bool
	usesAssert        bool
	usesConcurrency   bool
	usesSafety        bool
	usesRefcount      bool
	usesArena         bool
	usesDebugCycles   bool
	usesWeakRef       bool
	usesOptional      bool
	usesExceptions    bool
	usesStringMethods bool
	usesMap           bool
	usesEventLoop     bool
	usesStringBuilder bool

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
	routeWrapperCount int             // unique counter for route handler wrappers
	matchCounter      int             // unique counter for match expression temp vars
	lambdaCounter     int             // unique counter for lambda/closure functions
	lambdaWrappers    strings.Builder // collected lambda functions emitted before main

	funcTypedefs    map[ast.Type]string // function type → typedef name (e.g. DexFn_1)
	usesClosure     bool                // any function value appears, so closures are needed
	closureThunks   map[string]string   // function name → uniform-convention thunk
	methodWrappers  map[string]string   // flattened method name → method-value wrapper
	methodEnvTypes  map[ast.Type]bool   // struct types with a receiver environment emitted
	routeAdapters   map[string]string   // handler shape → response adapter
	closureTypes    strings.Builder     // environment typedefs
	closureWrappers strings.Builder     // thunks, wrappers, and environment destructors
	funcTypedefCnt  int                 // counter for unique typedef names

	// Scope tracking for cleanup
	scopeStack     [][]scopeVar
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
	stmtPrelude  *strings.Builder
	stmtTemps    []scopeVar
	stmtHoistOff int
	tempCounter  int // unique counter for temp names

	// Defer tracking
	deferExprs []ast.Expr // accumulated defer expressions for current function (LIFO order)

	// Global variable tracking
	globalLets []ast.LetStmt // module-level let/const declarations
	// inlinedConsts names the globals whose value was written at their
	// declaration, so main() does not try to assign them again.
	inlinedConsts map[string]bool
	globalVars    map[string]ast.Type // global variable name -> type
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
