package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

// genLambdaExpr generates a closure/lambda expression.
// For closures with captures, it generates an environment struct and wrapper function.
// For closures without captures, it generates a plain function pointer.
//
// The generated C representation uses function pointer typedefs.
// Since DexLang already has FuncType support, we can generate a plain function
// for lambdas without captures.
func (g *Generator) genLambdaExpr(out *strings.Builder, e *ast.LambdaExpr) {
	idx := g.lambdaCounter
	g.lambdaCounter++

	// Determine captured variables from outer scope
	captured := g.findCapturedVars(e.Body)

	fnName := fmt.Sprintf("_dex_lambda_%d", idx)

	if len(captured) == 0 {
		// No captures — generate a simple static function
		g.lambdaWrappers.WriteString(fmt.Sprintf("static %s %s(", g.cType(e.ReturnType), fnName))
		if len(e.Params) == 0 {
			g.lambdaWrappers.WriteString("void")
		} else {
			for i, p := range e.Params {
				if i > 0 {
					g.lambdaWrappers.WriteString(", ")
				}
				g.lambdaWrappers.WriteString(fmt.Sprintf("%s %s", g.cType(p.Type), p.Name))
			}
		}
		g.lambdaWrappers.WriteString(") {\n")

		// Save and restore generator state for the lambda body
		savedVarTypes := g.varTypes
		savedStrVars := g.strVars
		savedArrVars := g.arrVars
		savedStructVars := g.structVars
		g.varTypes = make(map[string]ast.Type)
		g.strVars = make(map[string]bool)
		g.arrVars = make(map[string]ast.Type)
		g.structVars = make(map[string]ast.Type)

		for _, p := range e.Params {
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

		// The body's `return` belongs to the lambda, not to the function the
		// lambda is written inside, so the current signature is swapped for the
		// duration.
		savedFn := g.currentFn
		g.currentFn = &ast.Function{Name: fnName, Params: e.Params, ReturnType: e.ReturnType}
		var bodyBuf strings.Builder
		for _, stmt := range e.Body {
			g.genStmt(&bodyBuf, stmt, 1)
		}
		g.currentFn = savedFn
		g.lambdaWrappers.WriteString(bodyBuf.String())
		g.lambdaWrappers.WriteString("}\n")

		// Restore state
		g.varTypes = savedVarTypes
		g.strVars = savedStrVars
		g.arrVars = savedArrVars
		g.structVars = savedStructVars

		// At call site, just reference the function name
		out.WriteString(fnName)
	} else {
		// Has captures — build an environment holding a copy of each captured
		// value, and a wrapper that unpacks it. This is the same shape as a
		// method value; only the contents of the environment differ.
		//
		// The environment takes a reference to every heap value it captures, so a
		// closure stays valid after the scope it was created in has gone.
		envName := fmt.Sprintf("_DexLambdaEnv_%d", idx)
		g.closureTypes.WriteString(fmt.Sprintf("typedef struct { DexObjHeader hdr;"))
		for _, cv := range captured {
			g.closureTypes.WriteString(fmt.Sprintf(" %s %s;", g.cType(cv.typ), cv.name))
		}
		g.closureTypes.WriteString(fmt.Sprintf(" } %s;\n", envName))

		destroyName := envName + "_destroy"
		g.closureWrappers.WriteString(fmt.Sprintf("static void %s(void* _p) {\n", destroyName))
		g.closureWrappers.WriteString(fmt.Sprintf("    %s* _e = (%s*)_p;\n    (void)_e;\n", envName, envName))
		for _, cv := range captured {
			if ast.IsHeapType(cv.typ) {
				g.closureWrappers.WriteString(fmt.Sprintf("    dex_release(_e->%s);\n", cv.name))
			}
		}
		g.closureWrappers.WriteString("}\n")

		// The wrapper reads the captures back into locals of the same names, so
		// the body generates exactly as it would have inline.
		var w strings.Builder
		w.WriteString(fmt.Sprintf("static %s %s(void* _env", g.cType(e.ReturnType), fnName))
		for _, p := range e.Params {
			w.WriteString(fmt.Sprintf(", %s %s", g.cType(p.Type), p.Name))
		}
		w.WriteString(") {\n")
		w.WriteString(fmt.Sprintf("    %s* _e = (%s*)_env;\n", envName, envName))
		for _, cv := range captured {
			w.WriteString(fmt.Sprintf("    %s %s = _e->%s;\n", g.cType(cv.typ), cv.name, cv.name))
		}

		savedVarTypes := g.varTypes
		savedStrVars := g.strVars
		savedArrVars := g.arrVars
		savedStructVars := g.structVars

		newVarTypes := make(map[string]ast.Type)
		newStrVars := make(map[string]bool)
		newArrVars := make(map[string]ast.Type)
		newStructVars := make(map[string]ast.Type)
		register := func(name string, typ ast.Type) {
			newVarTypes[name] = typ
			if typ == ast.TypeString {
				newStrVars[name] = true
			}
			if ast.IsArrayType(typ) {
				newArrVars[name] = typ
			}
			if ast.IsStructType(typ) {
				newStructVars[name] = typ
			}
		}
		for _, cv := range captured {
			register(cv.name, cv.typ)
		}
		for _, p := range e.Params {
			register(p.Name, p.Type)
		}
		g.varTypes = newVarTypes
		g.strVars = newStrVars
		g.arrVars = newArrVars
		g.structVars = newStructVars

		// Captured values belong to the environment, not to this invocation, so
		// the body must not release them on the way out.
		savedScopes := g.scopeStack
		savedFn := g.currentFn
		g.currentFn = &ast.Function{Name: fnName, Params: e.Params, ReturnType: e.ReturnType}
		g.scopeStack = nil
		g.pushScope()
		var bodyBuf strings.Builder
		for _, stmt := range e.Body {
			g.genStmt(&bodyBuf, stmt, 1)
		}
		g.scopeStack = savedScopes
		g.currentFn = savedFn

		w.WriteString(bodyBuf.String())
		w.WriteString("}\n")
		g.closureWrappers.WriteString(w.String())

		g.varTypes = savedVarTypes
		g.strVars = savedStrVars
		g.arrVars = savedArrVars
		g.structVars = savedStructVars

		// Build the environment at the point the lambda is written.
		tmp := g.nextTemp()
		out.WriteString(fmt.Sprintf("({ %s* %s = (%s*)dex_closure_env_alloc(sizeof(%s), %s); ",
			envName, tmp, envName, envName, destroyName))
		for _, cv := range captured {
			out.WriteString(fmt.Sprintf("%s->%s = %s; ", tmp, cv.name, cv.name))
			if ast.IsHeapType(cv.typ) {
				out.WriteString(fmt.Sprintf("dex_retain(%s->%s); ", tmp, cv.name))
			}
		}
		out.WriteString(fmt.Sprintf("dex_closure_new((void*)%s, %s); })", fnName, tmp))
	}
}
