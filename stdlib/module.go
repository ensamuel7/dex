package stdlib

import "github.com/ensamuel7/dex/ast"

type FuncDef struct {
	Params     []ast.Type
	ParamNames []string // human-readable parameter names
	ReturnType ast.Type
	CName      string // C function to call ("" = special handling in codegen)
	Doc        string // one-line description shown in editor
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
