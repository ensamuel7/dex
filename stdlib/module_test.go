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
		{"fmt", "println"},
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

// --- HttpRequest struct type tests ---

func TestHttpRequestStructFields(t *testing.T) {
	mod := Lookup("http")
	if mod == nil {
		t.Fatal("http module not found")
	}

	var httpReq *ast.StructDef
	for i := range mod.Types {
		if mod.Types[i].Name == "HttpRequest" {
			httpReq = &mod.Types[i]
			break
		}
	}
	if httpReq == nil {
		t.Fatal("HttpRequest struct not found in http module")
	}

	expectedFields := []string{"method", "path", "body", "query", "params"}
	if len(httpReq.Fields) != len(expectedFields) {
		t.Fatalf("HttpRequest field count = %d, want %d", len(httpReq.Fields), len(expectedFields))
	}
	for i, name := range expectedFields {
		if httpReq.Fields[i].Name != name {
			t.Errorf("HttpRequest.Fields[%d].Name = %q, want %q", i, httpReq.Fields[i].Name, name)
		}
	}
}

func TestHttpRequestParamsFieldType(t *testing.T) {
	mod := Lookup("http")
	if mod == nil {
		t.Fatal("http module not found")
	}

	var httpReq *ast.StructDef
	for i := range mod.Types {
		if mod.Types[i].Name == "HttpRequest" {
			httpReq = &mod.Types[i]
			break
		}
	}
	if httpReq == nil {
		t.Fatal("HttpRequest struct not found in http module")
	}

	// Find the params field
	var paramsField *ast.StructField
	for i := range httpReq.Fields {
		if httpReq.Fields[i].Name == "params" {
			paramsField = &httpReq.Fields[i]
			break
		}
	}
	if paramsField == nil {
		t.Fatal("HttpRequest.params field not found")
	}

	if !ast.IsMapType(paramsField.Type) {
		t.Fatalf("HttpRequest.params type is not a map type")
	}

	// Verify it's map[string,string]
	ast.ResetMapTypes()
	expectedType := ast.MapTypeOf(ast.TypeString, ast.TypeString)
	if paramsField.Type != expectedType {
		t.Errorf("HttpRequest.params type = %d, want map[string,string] (%d)", paramsField.Type, expectedType)
	}
}

func TestRegisterAllModuleTypes(t *testing.T) {
	ast.ResetStructTypes()
	ast.ResetMapTypes()

	// Before registration, HttpRequest/HttpResponse should not be in the struct registry
	_, ok := ast.LookupStructType("HttpRequest")
	if ok {
		t.Error("HttpRequest should not be registered before RegisterAllModuleTypes()")
	}

	RegisterAllModuleTypes()

	// After registration, module types should be registered
	_, ok = ast.LookupStructType("HttpRequest")
	if !ok {
		t.Error("HttpRequest should be registered after RegisterAllModuleTypes()")
	}
	_, ok = ast.LookupStructType("HttpResponse")
	if !ok {
		t.Error("HttpResponse should be registered after RegisterAllModuleTypes()")
	}

	// Calling again should not panic or duplicate
	RegisterAllModuleTypes()
	_, ok = ast.LookupStructType("HttpRequest")
	if !ok {
		t.Error("HttpRequest should still be registered after second RegisterAllModuleTypes()")
	}
}

func TestRegisterAllModuleTypesReregistersMapTypes(t *testing.T) {
	ast.ResetMapTypes()
	RegisterAllModuleTypes()

	// After RegisterAllModuleTypes, map[string,string] should be registered
	mapStrStr := ast.MapTypeOf(ast.TypeString, ast.TypeString)
	if !ast.IsMapType(mapStrStr) {
		t.Error("map[string,string] should be a map type after RegisterAllModuleTypes()")
	}
	if ast.MapKeyType(mapStrStr) != ast.TypeString {
		t.Error("map[string,string] key type should be TypeString")
	}
	if ast.MapValueType(mapStrStr) != ast.TypeString {
		t.Error("map[string,string] value type should be TypeString")
	}
}

func TestModuleTypesForImportsHttp(t *testing.T) {
	names := ModuleTypesForImports([]string{"http"})
	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	if !found["HttpRequest"] {
		t.Error("ModuleTypesForImports([http]) missing HttpRequest")
	}
	if !found["HttpResponse"] {
		t.Error("ModuleTypesForImports([http]) missing HttpResponse")
	}
}

func TestModuleTypesForImportsEmpty(t *testing.T) {
	names := ModuleTypesForImports([]string{})
	if len(names) != 0 {
		t.Errorf("ModuleTypesForImports([]) = %v, want empty", names)
	}
}

func TestModuleTypesForImportsUnknown(t *testing.T) {
	names := ModuleTypesForImports([]string{"nonexistent"})
	if len(names) != 0 {
		t.Errorf("ModuleTypesForImports([nonexistent]) = %v, want empty", names)
	}
}

func TestModuleTypesForImportsNoTypes(t *testing.T) {
	names := ModuleTypesForImports([]string{"fmt"})
	if len(names) != 0 {
		t.Errorf("ModuleTypesForImports([fmt]) = %v, want empty (fmt has no types)", names)
	}
}

func TestTimeFunctionSignatures(t *testing.T) {
	mod := Lookup("time")
	if mod == nil {
		t.Fatal("time module not found")
	}
	if len(mod.Funcs) == 0 {
		t.Fatal("time module has no functions")
	}
	// Verify some time functions exist
	for _, name := range []string{"now", "nowNs", "sleep"} {
		fd, ok := LookupFunc("time", name)
		if !ok || fd == nil {
			t.Errorf("LookupFunc(time, %q) not found", name)
		}
	}
}

func TestModuleCRuntimeNotEmpty(t *testing.T) {
	// Modules with embedded C runtime should have non-empty CRuntime
	// Note: fmt uses inline codegen rather than embedded CRuntime
	for _, name := range []string{"json", "http", "db", "math", "time"} {
		mod := Lookup(name)
		if mod == nil {
			t.Errorf("module %q not found", name)
			continue
		}
		if mod.CRuntime == "" {
			t.Errorf("module %q has empty CRuntime", name)
		}
	}
}

func TestModuleDocStrings(t *testing.T) {
	// Functions should have doc strings for editor support
	for _, modName := range []string{"fmt", "json", "http", "db", "math", "time"} {
		mod := Lookup(modName)
		if mod == nil {
			continue
		}
		for fnName, fd := range mod.Funcs {
			if fd.Doc == "" {
				t.Errorf("%s.%s has empty Doc string", modName, fnName)
			}
		}
	}
}

func TestHttpResponseStructUnchanged(t *testing.T) {
	mod := Lookup("http")
	if mod == nil {
		t.Fatal("http module not found")
	}

	var httpResp *ast.StructDef
	for i := range mod.Types {
		if mod.Types[i].Name == "HttpResponse" {
			httpResp = &mod.Types[i]
			break
		}
	}
	if httpResp == nil {
		t.Fatal("HttpResponse struct not found in http module")
	}

	// HttpResponse should still have exactly 3 fields
	expectedFields := []string{"statusCode", "body", "contentType"}
	if len(httpResp.Fields) != len(expectedFields) {
		t.Fatalf("HttpResponse field count = %d, want %d", len(httpResp.Fields), len(expectedFields))
	}
	for i, name := range expectedFields {
		if httpResp.Fields[i].Name != name {
			t.Errorf("HttpResponse.Fields[%d].Name = %q, want %q", i, httpResp.Fields[i].Name, name)
		}
	}
}
