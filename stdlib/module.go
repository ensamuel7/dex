package stdlib

import (
	_ "embed"

	"github.com/ensamuel7/dex/ast"
)

// Base64, shared by the module runtimes that need it — the ws handshake and
// SMTP's AUTH. It lives here rather than beside either of them because it
// belongs to neither: both prepend it to their own runtime, and the guard
// inside it makes the second paste a no-op. See cruntime/base64.h.
//
//go:embed cruntime/base64.h
var base64Runtime string

type FuncDef struct {
	Params     []ast.Type
	ParamNames []string // human-readable parameter names
	ReturnType ast.Type
	CName      string // C function to call ("" = special handling in codegen)
	Doc        string // one-line description shown in editor

	// RawParams lists the parameter indices passed as DexString* rather than
	// const char*, and RawReturn says the C function already built the DexString
	// it returns.
	//
	// They exist because DexString is length-prefixed but the ordinary stdlib
	// boundary hands over `->data` alone, so every byte after the first NUL is
	// lost. That is invisible for text and fatal for a JPEG. A function handling
	// arbitrary bytes — reading a file, base64, a digest — marks the byte-valued
	// parameters and reads `->len` instead of calling strlen.
	//
	// It is per-parameter on purpose: a path is text and stays const char*, while
	// the content beside it is bytes. Marking a whole function would hand the
	// callee a DexString* where it expects a path.
	//
	// The generated C is a single translation unit, so a stdlib runtime marked
	// this way can use dex_string_new and the rest of the DexString API directly.
	RawParams []int
	RawReturn bool
}

// IsRawParam reports whether argument idx is handed over as a DexString*.
func (f FuncDef) IsRawParam(idx int) bool {
	for _, i := range f.RawParams {
		if i == idx {
			return true
		}
	}
	return false
}

type Module struct {
	Name      string
	Funcs     map[string]FuncDef
	Types     []ast.StructDef // module-provided struct types
	CRuntime  string          // C code to embed
	CIncludes string          // #include directives
	CFlags    []string        // compiler flags (e.g. "-pthread")
}

var registry = map[string]*Module{}

func Register(m *Module) {
	registry[m.Name] = m
}

func AllModules() map[string]*Module {
	return registry
}

func Lookup(name string) *Module {
	return registry[name]
}

func LookupFunc(moduleName, funcName string) (*FuncDef, bool) {
	m := registry[moduleName]
	if m == nil {
		return nil, false
	}
	f, ok := m.Funcs[funcName]
	if !ok {
		return nil, false
	}
	return &f, true
}

// RegisterAllModuleTypes registers struct types from all modules into the global ast registry.
func RegisterAllModuleTypes() {
	// Re-register map types used by module struct fields. Map type state is reset
	// between compilations, but module definitions persist from init(). Since type IDs
	// are assigned sequentially from TypeMapBase, re-calling MapTypeOf in the same
	// order as init() produces the same IDs.
	ast.MapTypeOf(ast.TypeString, ast.TypeString) // used by http.HttpRequest.params

	for _, mod := range registry {
		for _, td := range mod.Types {
			if _, exists := ast.LookupStructType(td.Name); !exists {
				ast.RegisterStructType(td)
			}
		}
	}
}

// ModuleTypesForImports returns the struct type names defined by the given imported modules.
func ModuleTypesForImports(importPaths []string) []string {
	var names []string
	for _, path := range importPaths {
		mod := registry[path]
		if mod == nil {
			continue
		}
		for _, td := range mod.Types {
			names = append(names, td.Name)
		}
	}
	return names
}
