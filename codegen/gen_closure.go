package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

// A function value is a DexClosure*: a code pointer plus its environment, passed
// as a hidden first argument. This file builds one from each of the three
// sources — a plain function, a lambda, and a method bound to its receiver.

// A top-level function carries no state, so its thunk ignores the environment.
func (g *Generator) closureThunk(fnName string, params []ast.Type, retType ast.Type) string {
	if name, ok := g.closureThunks[fnName]; ok {
		return name
	}
	name := fmt.Sprintf("_dex_thunk_%d", len(g.closureThunks))
	g.closureThunks[fnName] = name

	var w strings.Builder
	w.WriteString(fmt.Sprintf("static %s %s(void* _env", g.cType(retType), name))
	for i, p := range params {
		w.WriteString(fmt.Sprintf(", %s _a%d", g.cType(p), i))
	}
	w.WriteString(") {\n    (void)_env;\n    ")
	if retType != ast.TypeVoid {
		w.WriteString("return ")
	}
	w.WriteString(fnName + "(")
	for i := range params {
		if i > 0 {
			w.WriteString(", ")
		}
		w.WriteString(fmt.Sprintf("_a%d", i))
	}
	w.WriteString(");\n}\n")
	g.closureWrappers.WriteString(w.String())
	return name
}

func (g *Generator) genFuncValue(out *strings.Builder, fnName string, fnType ast.Type) {
	thunk := g.closureThunk(fnName, ast.FuncTypeParams(fnType), ast.FuncTypeReturn(fnType))
	out.WriteString(fmt.Sprintf("dex_closure_new((void*)%s, NULL)", thunk))
}

// The code pointer is cast to the signature its type describes.
func (g *Generator) genClosureCall(out *strings.Builder, recv string, fnType ast.Type, args []ast.Expr) {
	typedef := g.funcTypedef(fnType)
	params := ast.FuncTypeParams(fnType)
	out.WriteString(fmt.Sprintf("((%s)%s->fn)(%s->env", typedef, recv, recv))
	for i, arg := range args {
		out.WriteString(", ")
		if i < len(params) && ast.IsOptionalType(params[i]) {
			g.genOptionalArg(out, arg, params[i])
		} else {
			g.genBorrowed(out, arg)
		}
	}
	out.WriteString(")")
}

// The receiver is copied into the environment and its heap fields retained, so
// the value keeps working after the variable it came from is gone.
func (g *Generator) genMethodValue(out *strings.Builder, recvExpr ast.Expr, structType ast.Type, method *ast.Function, flatName string) {
	def := ast.GetStructDef(structType)
	if def == nil {
		out.WriteString("NULL")
		return
	}
	structC := g.cType(structType)
	envName := g.methodValueEnvType(structType)
	wrapper := g.methodValueWrapper(structType, method, flatName)

	tmp := g.nextTemp()
	out.WriteString(fmt.Sprintf("({ %s* %s = (%s*)dex_closure_env_alloc(sizeof(%s), %s); %s->self = ",
		envName, tmp, envName, envName, g.methodValueEnvDestroy(structType), tmp))
	g.genExpr(out, recvExpr)
	out.WriteString("; ")
	// The environment is now a second owner of every heap field the receiver
	// holds, and outlives the expression it was built from.
	for _, f := range def.Fields {
		if ast.IsHeapType(f.Type) {
			out.WriteString(fmt.Sprintf("dex_retain(%s->self.%s); ", tmp, f.Name))
		}
	}
	_ = structC
	out.WriteString(fmt.Sprintf("dex_closure_new((void*)%s, %s); })", wrapper, tmp))
}

// Emitted once per struct.
func (g *Generator) methodValueEnvType(structType ast.Type) string {
	name := "_DexRecvEnv_" + ast.StructName(structType)
	if g.methodEnvTypes[structType] {
		return name
	}
	g.methodEnvTypes[structType] = true

	def := ast.GetStructDef(structType)
	g.closureTypes.WriteString(fmt.Sprintf("typedef struct { DexObjHeader hdr; %s self; } %s;\n",
		g.cType(structType), name))

	g.closureWrappers.WriteString(fmt.Sprintf("static void %s_destroy(void* _p) {\n", name))
	g.closureWrappers.WriteString(fmt.Sprintf("    %s* _e = (%s*)_p;\n", name, name))
	g.closureWrappers.WriteString("    (void)_e;\n")
	if def != nil {
		for _, f := range def.Fields {
			if ast.IsHeapType(f.Type) {
				g.closureWrappers.WriteString(fmt.Sprintf("    dex_release(_e->self.%s);\n", f.Name))
			}
		}
	}
	g.closureWrappers.WriteString("}\n")
	return name
}

func (g *Generator) methodValueEnvDestroy(structType ast.Type) string {
	return g.methodValueEnvType(structType) + "_destroy"
}

// Unpacks the receiver from the environment and calls the flattened method.
func (g *Generator) methodValueWrapper(structType ast.Type, method *ast.Function, flatName string) string {
	key := flatName
	if name, ok := g.methodWrappers[key]; ok {
		return name
	}
	name := fmt.Sprintf("_dex_mv_%d", len(g.methodWrappers))
	g.methodWrappers[key] = name
	envName := g.methodValueEnvType(structType)

	var w strings.Builder
	w.WriteString(fmt.Sprintf("static %s %s(void* _env", g.cType(method.ReturnType), name))
	for i, p := range method.Params {
		w.WriteString(fmt.Sprintf(", %s _a%d", g.cType(p.Type), i))
	}
	w.WriteString(") {\n")
	w.WriteString(fmt.Sprintf("    %s* _e = (%s*)_env;\n    ", envName, envName))
	if method.ReturnType != ast.TypeVoid {
		w.WriteString("return ")
	}
	w.WriteString(fmt.Sprintf("%s(_e->self", flatName))
	for i := range method.Params {
		w.WriteString(fmt.Sprintf(", _a%d", i))
	}
	w.WriteString(");\n}\n")
	g.closureWrappers.WriteString(w.String())
	return name
}

// The signature comes from the flattened function, not the struct definition:
// the global struct registry records fields only.
func (g *Generator) genMethodValueExpr(out *strings.Builder, e *ast.FieldAccessExpr) {
	structType := e.StructType
	def := ast.GetStructDef(structType)
	if def == nil {
		out.WriteString("NULL")
		return
	}

	// Struct methods are flattened to `Struct_method`, and to
	// `module_Struct_method` when the struct comes from a user module.
	flatName := def.Name + "_" + e.Field
	if modName, ok := g.structModules[def.Name]; ok {
		flatName = modName + "_" + flatName
	}
	flat, ok := g.funcs[flatName]
	if !ok {
		out.WriteString("NULL")
		return
	}

	// Drop the receiver from the signature the caller sees.
	method := &ast.Function{Name: e.Field, ReturnType: flat.ReturnType}
	if len(flat.Params) > 0 {
		method.Params = flat.Params[1:]
	}
	g.genMethodValue(out, e.Object, structType, method, flatName)
}

// For somewhere that keeps the value: a borrowed one is retained, a fresh one
// simply moves.
func (g *Generator) genOwnedClosureArg(out *strings.Builder, expr ast.Expr) {
	if !g.borrowsHeapValue(expr) {
		g.genExpr(out, expr)
		return
	}
	tmp := g.nextTemp()
	out.WriteString(fmt.Sprintf("({ DexClosure* %s = ", tmp))
	g.genExpr(out, expr)
	out.WriteString(fmt.Sprintf("; dex_retain(%s); %s; })", tmp, tmp))
}

// The router always calls Dex_HttpResponse(void*, Dex_HttpRequest). A handler
// written in another shape is wrapped in an adapter closure over the original.
func (g *Generator) genRouteHandler(out *strings.Builder, arg ast.Expr) {
	fnType := g.typeOfExpr(arg)
	respType, hasResp := ast.LookupStructType("HttpResponse")
	reqType, _ := ast.LookupStructType("HttpRequest")

	params := ast.FuncTypeParams(fnType)
	retType := ast.FuncTypeReturn(fnType)
	takesReq := len(params) == 1 && params[0] == reqType

	if hasResp && retType == respType && takesReq {
		// Already the router's signature.
		g.genOwnedClosureArg(out, arg)
		return
	}

	adapter := g.routeAdapter(fnType, respType, reqType, takesReq, retType)
	tmp := g.nextTemp()
	out.WriteString(fmt.Sprintf("({ DexClosure* %s = ", tmp))
	g.genOwnedClosureArg(out, arg)
	out.WriteString(fmt.Sprintf("; dex_closure_new((void*)%s, %s); })", adapter, tmp))
}

// Emitted once per handler shape.
func (g *Generator) routeAdapter(fnType, respType, reqType ast.Type, takesReq bool, retType ast.Type) string {
	key := fmt.Sprintf("%d", int(fnType))
	if name, ok := g.routeAdapters[key]; ok {
		return name
	}
	name := fmt.Sprintf("_dex_route_adapt_%d", len(g.routeAdapters))
	g.routeAdapters[key] = name
	inner := g.funcTypedef(fnType)

	var w strings.Builder
	w.WriteString(fmt.Sprintf("static Dex_HttpResponse %s(void* _env, Dex_HttpRequest _req) {\n", name))
	w.WriteString("    DexClosure* _h = (DexClosure*)_env;\n")
	call := fmt.Sprintf("((%s)_h->fn)(_h->env)", inner)
	if takesReq {
		call = fmt.Sprintf("((%s)_h->fn)(_h->env, _req)", inner)
	} else {
		w.WriteString("    (void)_req;\n")
	}

	switch {
	case retType == respType:
		w.WriteString(fmt.Sprintf("    return %s;\n", call))
	case retType == ast.TypeString:
		w.WriteString(fmt.Sprintf("    DexString* _val = %s;\n", call))
		w.WriteString("    return (Dex_HttpResponse){200, _val, dex_string_from_lit(\"application/json\")};\n")
	default:
		spec := "%d"
		switch retType {
		case ast.TypeLong:
			spec = "%ld"
		case ast.TypeDouble:
			spec = "%f"
		}
		w.WriteString(fmt.Sprintf("    %s _val = %s;\n", g.cType(retType), call))
		w.WriteString("    char _buf[64];\n")
		if retType == ast.TypeBool {
			w.WriteString("    snprintf(_buf, sizeof(_buf), \"%s\", _val ? \"true\" : \"false\");\n")
		} else {
			w.WriteString(fmt.Sprintf("    snprintf(_buf, sizeof(_buf), \"%s\", _val);\n", spec))
		}
		w.WriteString("    return (Dex_HttpResponse){200, dex_string_from_lit(_buf), dex_string_from_lit(\"application/json\")};\n")
	}
	w.WriteString("}\n")
	g.closureWrappers.WriteString(w.String())
	return name
}
