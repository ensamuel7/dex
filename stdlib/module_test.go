package stdlib

import (
	"testing"

	"github.com/ensamuel7/dex/ast"
)

func TestLookupExistingModules(t *testing.T) {
	modules := []string{"fmt", "json", "http", "db", "math", "time"}
	for _, name := range modules {
		mod := Lookup(name)
		if mod == nil {
			t.Errorf("Lookup(%q) = nil, want module", name)
			continue
		}
		if mod.Name != name {
			t.Errorf("Lookup(%q).Name = %q, want %q", name, mod.Name, name)
		}
	}
}

func TestLookupUnknownModule(t *testing.T) {
	mod := Lookup("nonexistent")
	if mod != nil {
		t.Errorf("Lookup(%q) = %v, want nil", "nonexistent", mod)
	}
}

func TestLookupFuncExisting(t *testing.T) {
	tests := []struct {
		module string
		fn     string
	}{
		{"fmt", "print"},
		{"json", "new"},
		{"json", "set"},
		{"json", "stringify"},
		{"json", "setArray"},
		{"http", "route"},
		{"http", "listen"},
		{"db", "open"},
		{"db", "exec"},
		{"db", "query"},
		{"db", "next"},
		{"db", "col"},
		{"db", "free"},
		{"db", "close"},
	}
	for _, tt := range tests {
		fd, ok := LookupFunc(tt.module, tt.fn)
		if !ok || fd == nil {
			t.Errorf("LookupFunc(%q, %q) = nil, want function", tt.module, tt.fn)
		}
	}
}

func TestLookupFuncUnknown(t *testing.T) {
	tests := []struct {
		module string
		fn     string
	}{
		{"fmt", "nonexistent"},
		{"nonexistent", "print"},
		{"json", "nonexistent"},
	}
	for _, tt := range tests {
		fd, ok := LookupFunc(tt.module, tt.fn)
		if ok || fd != nil {
			t.Errorf("LookupFunc(%q, %q) = (%v, true), want (nil, false)", tt.module, tt.fn, fd)
		}
	}
}

func TestFmtFunctionSignatures(t *testing.T) {
	fd, ok := LookupFunc("fmt", "print")
	if !ok {
		t.Fatal("LookupFunc(fmt, print) not found")
	}
	// print has nil params (polymorphic — checked by the checker)
	if fd.Params != nil {
		t.Errorf("fmt.print params = %v, want nil (polymorphic)", fd.Params)
	}
	if fd.ReturnType != ast.TypeVoid {
		t.Errorf("fmt.print return = %d, want TypeVoid", fd.ReturnType)
	}
}

func TestJsonFunctionSignatures(t *testing.T) {
	// json.new — concrete function
	fd, ok := LookupFunc("json", "new")
	if !ok {
		t.Fatal("LookupFunc(json, new) not found")
	}
	if len(fd.Params) != 0 {
		t.Errorf("json.new params count = %d, want 0", len(fd.Params))
	}
	if fd.ReturnType != ast.TypeString {
		t.Errorf("json.new return = %d, want TypeString", fd.ReturnType)
	}
	if fd.CName != "dex_json_new" {
		t.Errorf("json.new CName = %q, want %q", fd.CName, "dex_json_new")
	}

	// json.set — polymorphic (nil params, special-cased in checker/codegen)
	fd, ok = LookupFunc("json", "set")
	if !ok {
		t.Fatal("LookupFunc(json, set) not found")
	}
	if fd.Params != nil {
		t.Errorf("json.set params = %v, want nil (polymorphic)", fd.Params)
	}
	if fd.ReturnType != ast.TypeString {
		t.Errorf("json.set return = %d, want TypeString", fd.ReturnType)
	}
}

func TestHttpFunctionSignatures(t *testing.T) {
	tests := []struct {
		name       string
		params     []ast.Type
		returnType ast.Type
	}{
		{"route", []ast.Type{ast.TypeString, ast.TypeString, ast.TypeString}, ast.TypeVoid},
		{"listen", nil, ast.TypeVoid}, // special-cased in checker: 1-2 int args
	}
	for _, tt := range tests {
		fd, ok := LookupFunc("http", tt.name)
		if !ok {
			t.Fatalf("LookupFunc(http, %q) not found", tt.name)
		}
		if len(fd.Params) != len(tt.params) {
			t.Errorf("http.%s params count = %d, want %d", tt.name, len(fd.Params), len(tt.params))
			continue
		}
		for i, p := range tt.params {
			if fd.Params[i] != p {
				t.Errorf("http.%s param[%d] = %d, want %d", tt.name, i, fd.Params[i], p)
			}
		}
		if fd.ReturnType != tt.returnType {
			t.Errorf("http.%s return = %d, want %d", tt.name, fd.ReturnType, tt.returnType)
		}
	}
}

func TestDbFunctionSignatures(t *testing.T) {
	tests := []struct {
		name       string
		params     []ast.Type
		returnType ast.Type
		cname      string
	}{
		{"open", []ast.Type{ast.TypeString, ast.TypeString}, ast.TypeInt, "dex_db_open"},
		{"exec", []ast.Type{ast.TypeInt, ast.TypeString}, ast.TypeInt, "dex_db_exec"},
		{"query", []ast.Type{ast.TypeInt, ast.TypeString}, ast.TypeInt, "dex_db_query"},
		{"next", []ast.Type{ast.TypeInt}, ast.TypeBool, "dex_db_next"},
		{"col", nil, ast.TypeInt, ""},
		{"free", []ast.Type{ast.TypeInt}, ast.TypeVoid, "dex_db_free"},
		{"close", []ast.Type{ast.TypeInt}, ast.TypeVoid, "dex_db_close"},
	}
	for _, tt := range tests {
		fd, ok := LookupFunc("db", tt.name)
		if !ok {
			t.Fatalf("LookupFunc(db, %q) not found", tt.name)
		}
		if len(fd.Params) != len(tt.params) {
			t.Errorf("db.%s params count = %d, want %d", tt.name, len(fd.Params), len(tt.params))
			continue
		}
		for i, p := range tt.params {
			if fd.Params[i] != p {
				t.Errorf("db.%s param[%d] = %d, want %d", tt.name, i, fd.Params[i], p)
			}
		}
		if fd.ReturnType != tt.returnType {
			t.Errorf("db.%s return = %d, want %d", tt.name, fd.ReturnType, tt.returnType)
		}
		if fd.CName != tt.cname {
			t.Errorf("db.%s CName = %q, want %q", tt.name, fd.CName, tt.cname)
		}
	}
}

func TestMathFunctionSignatures(t *testing.T) {
	tests := []struct {
		name       string
		paramCount int
		returnType ast.Type
		cname      string
	}{
		{"pi", 0, ast.TypeDouble, "dex_math_pi"},
		{"e", 0, ast.TypeDouble, "dex_math_e"},
		{"sin", 1, ast.TypeDouble, "dex_math_sin"},
		{"cos", 1, ast.TypeDouble, "dex_math_cos"},
		{"tan", 1, ast.TypeDouble, "dex_math_tan"},
		{"asin", 1, ast.TypeDouble, "dex_math_asin"},
		{"acos", 1, ast.TypeDouble, "dex_math_acos"},
		{"atan", 1, ast.TypeDouble, "dex_math_atan"},
		{"sqrt", 1, ast.TypeDouble, "dex_math_sqrt"},
		{"pow", 2, ast.TypeDouble, "dex_math_pow"},
		{"exp", 1, ast.TypeDouble, "dex_math_exp"},
		{"floor", 1, ast.TypeDouble, "dex_math_floor"},
		{"ceil", 1, ast.TypeDouble, "dex_math_ceil"},
		{"round", 1, ast.TypeDouble, "dex_math_round"},
		{"abs", 1, ast.TypeDouble, "dex_math_abs"},
		{"log", 1, ast.TypeDouble, "dex_math_log"},
		{"log2", 1, ast.TypeDouble, "dex_math_log2"},
		{"log10", 1, ast.TypeDouble, "dex_math_log10"},
		{"min", 2, ast.TypeDouble, "dex_math_min"},
		{"max", 2, ast.TypeDouble, "dex_math_max"},
	}
	for _, tt := range tests {
		fd, ok := LookupFunc("math", tt.name)
		if !ok {
			t.Fatalf("LookupFunc(math, %q) not found", tt.name)
		}
		if len(fd.Params) != tt.paramCount {
			t.Errorf("math.%s params count = %d, want %d", tt.name, len(fd.Params), tt.paramCount)
			continue
		}
		for i := 0; i < tt.paramCount; i++ {
			if fd.Params[i] != ast.TypeDouble {
				t.Errorf("math.%s param[%d] = %d, want TypeDouble", tt.name, i, fd.Params[i])
			}
		}
		if fd.ReturnType != tt.returnType {
			t.Errorf("math.%s return = %d, want %d", tt.name, fd.ReturnType, tt.returnType)
		}
		if fd.CName != tt.cname {
			t.Errorf("math.%s CName = %q, want %q", tt.name, fd.CName, tt.cname)
		}
	}
}

func TestAllModules(t *testing.T) {
	all := AllModules()
	expected := []string{"fmt", "json", "http", "db", "math", "time"}
	for _, name := range expected {
		if _, ok := all[name]; !ok {
			t.Errorf("AllModules() missing module %q", name)
		}
	}
}
