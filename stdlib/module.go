package stdlib

import "github.com/ensamuel7/dex/ast"

type FuncDef struct {
	Params     []ast.Type
	ReturnType ast.Type
	CName      string // C function to call ("" = special handling in codegen)
}

type Module struct {
	Name      string
	Funcs     map[string]FuncDef
	CRuntime  string   // C code to embed
	CIncludes string   // #include directives
	CFlags    []string // compiler flags (e.g. "-pthread")
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
