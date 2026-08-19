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

		var bodyBuf strings.Builder
		for _, stmt := range e.Body {
			g.genStmt(&bodyBuf, stmt, 1)
		}
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
		// Has captures — generate a closure with environment struct
		// For simplicity, we use the same approach as spawn:
		// but since closures need to be called synchronously, we use a struct with env + function.
		// However, the existing FuncType system in DexLang uses plain function pointers.
		// For now, we'll generate a static function that takes the captures as extra first params,
		// and use a wrapper approach.

		// Actually, let's keep it simple: generate a static function that reads from a global
		// or use a GCC nested function (which captures variables from enclosing scope).
		// GCC nested functions are the simplest approach for captured variables.

		// Use GCC statement expression with nested function:
		out.WriteString("({ ")

		// Declare the nested function
		out.WriteString(fmt.Sprintf("%s %s(", g.cType(e.ReturnType), fnName))
		if len(e.Params) == 0 {
			out.WriteString("void")
		} else {
			for i, p := range e.Params {
				if i > 0 {
					out.WriteString(", ")
				}
				out.WriteString(fmt.Sprintf("%s %s", g.cType(p.Type), p.Name))
			}
		}
		out.WriteString(") { ")

		// Generate body inline
		savedVarTypes := g.varTypes
		savedStrVars := g.strVars
		savedArrVars := g.arrVars
		savedStructVars := g.structVars

		// Create new maps with captured variables + params
		newVarTypes := make(map[string]ast.Type)
		newStrVars := make(map[string]bool)
		newArrVars := make(map[string]ast.Type)
		newStructVars := make(map[string]ast.Type)

		// Copy captured vars
		for _, cv := range captured {
			newVarTypes[cv.name] = cv.typ
			if cv.typ == ast.TypeString {
				newStrVars[cv.name] = true
			}
			if ast.IsArrayType(cv.typ) {
				newArrVars[cv.name] = cv.typ
			}
			if ast.IsStructType(cv.typ) {
				newStructVars[cv.name] = cv.typ
			}
		}

		// Add params
		for _, p := range e.Params {
			newVarTypes[p.Name] = p.Type
			if p.Type == ast.TypeString {
				newStrVars[p.Name] = true
			}
			if ast.IsArrayType(p.Type) {
				newArrVars[p.Name] = p.Type
			}
			if ast.IsStructType(p.Type) {
				newStructVars[p.Name] = p.Type
			}
		}

		g.varTypes = newVarTypes
		g.strVars = newStrVars
		g.arrVars = newArrVars
		g.structVars = newStructVars

		var bodyBuf strings.Builder
		for _, stmt := range e.Body {
			g.genStmt(&bodyBuf, stmt, 0)
		}
		out.WriteString(bodyBuf.String())
		out.WriteString("} ")

		// Restore state
		g.varTypes = savedVarTypes
		g.strVars = savedStrVars
		g.arrVars = savedArrVars
		g.structVars = savedStructVars

		out.WriteString(fmt.Sprintf("%s; })", fnName))
	}
}
