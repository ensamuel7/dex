package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/stdlib"
)

type Generator struct {
	usesBool        bool
	usesString      bool
	usesArray       bool
	usesAssert      bool
	usesConcurrency bool
	usesSafety      bool

	importedModules map[string]*stdlib.Module

	funcs          map[string]*ast.Function
	strVars        map[string]bool     // variables known to be string type
	strLenVars     map[string]bool     // string vars with a shadow _dex_slen_ length variable
	arrVars        map[string]ast.Type  // variables known to be array type (name -> array type)
	structVars     map[string]ast.Type  // variables known to be struct type
	varTypes       map[string]ast.Type  // all variable types for this function scope
	foreachCounter int                  // unique counter for foreach loop variables
	spawnCounter   int                  // unique counter for spawn wrapper functions
	spawnWrappers  strings.Builder      // collected wrapper functions emitted before main

	funcTypedefs   map[ast.Type]string  // function type → typedef name (e.g. DexFn_1)
	funcTypedefCnt int                  // counter for unique typedef names
}

func New() *Generator {
	return &Generator{
		importedModules: make(map[string]*stdlib.Module),
		funcs:           make(map[string]*ast.Function),
		strVars:         make(map[string]bool),
		strLenVars:      make(map[string]bool),
		arrVars:         make(map[string]ast.Type),
		structVars:      make(map[string]ast.Type),
		varTypes:        make(map[string]ast.Type),
		funcTypedefs:    make(map[ast.Type]string),
	}
}

// CompilerFlags returns extra flags needed for the C compiler based on features used.
// Must be called after Generate().
func (g *Generator) CompilerFlags() []string {
	flags := []string{"-O2"}
	if g.usesConcurrency {
		flags = append(flags, "-pthread")
	}
	for _, mod := range g.importedModules {
		flags = append(flags, mod.CFlags...)
	}
	return flags
}

func (g *Generator) Generate(program *ast.Program) string {
	// Register imported modules
	for _, imp := range program.Imports {
		mod := stdlib.Lookup(imp.Path)
		if mod != nil {
			g.importedModules[imp.Path] = mod
		}
	}

	// Index functions
	for i := range program.Functions {
		g.funcs[program.Functions[i].Name] = &program.Functions[i]
	}

	// Pre-scan to determine needed language-level features
	g.scan(program)

	var out strings.Builder

	// Collect and deduplicate includes from imported modules
	emittedIncludes := map[string]bool{}
	for _, mod := range g.importedModules {
		if mod.CIncludes != "" {
			for _, line := range strings.Split(mod.CIncludes, "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !emittedIncludes[line] {
					emittedIncludes[line] = true
					out.WriteString(line + "\n")
				}
			}
		}
	}

	if g.usesBool {
		inc := "#include <stdbool.h>"
		if !emittedIncludes[inc] {
			emittedIncludes[inc] = true
			out.WriteString(inc + "\n")
		}
	}

	if g.usesAssert {
		for _, inc := range []string{"#include <stdio.h>", "#include <stdlib.h>"} {
			if !emittedIncludes[inc] {
				emittedIncludes[inc] = true
				out.WriteString(inc + "\n")
			}
		}
	}

	if g.usesConcurrency {
		for _, inc := range []string{"#include <pthread.h>", "#include <stdlib.h>", "#include <string.h>"} {
			if !emittedIncludes[inc] {
				emittedIncludes[inc] = true
				out.WriteString(inc + "\n")
			}
		}
	}

	// Emit safety runtime (bounds checks, div-zero checks, panic helper)
	if g.usesSafety {
		for _, inc := range []string{"#include <stdio.h>", "#include <stdlib.h>"} {
			if !emittedIncludes[inc] {
				emittedIncludes[inc] = true
				out.WriteString(inc + "\n")
			}
		}
		out.WriteString(SafetyRuntime)
	}

	// Emit string runtime (language-level feature for + on strings)
	if g.usesString {
		out.WriteString(StringRuntime)
	}

	// Emit array runtime (must come before module runtimes that reference array types)
	if g.usesArray {
		out.WriteString(ArrayRuntime)
	}

	// Emit concurrency runtime
	if g.usesConcurrency {
		out.WriteString(ConcurrencyRuntime)
	}

	// Emit C runtime from imported modules
	for _, mod := range g.importedModules {
		if mod.CRuntime != "" {
			out.WriteString(mod.CRuntime)
		}
	}

	// Emit struct typedefs
	for _, sd := range program.Structs {
		out.WriteString(fmt.Sprintf("typedef struct {\n"))
		for _, f := range sd.Fields {
			out.WriteString(fmt.Sprintf("    %s %s;\n", g.cType(f.Type), f.Name))
		}
		out.WriteString(fmt.Sprintf("} Dex_%s;\n", sd.Name))
	}

	// Emit function pointer typedefs
	for t, name := range g.funcTypedefs {
		params := ast.FuncTypeParams(t)
		retType := ast.FuncTypeReturn(t)
		out.WriteString(fmt.Sprintf("typedef %s (*%s)(", g.cType(retType), name))
		if len(params) == 0 {
			out.WriteString("void")
		} else {
			for i, p := range params {
				if i > 0 {
					out.WriteString(", ")
				}
				out.WriteString(g.cType(p))
			}
		}
		out.WriteString(");\n")
	}

	if out.Len() > 0 {
		out.WriteString("\n")
	}

	// Forward declarations for handler functions used by HTTP
	if _, ok := g.importedModules["http"]; ok {
		for _, fn := range program.Functions {
			if fn.ReturnType == ast.TypeString && len(fn.Params) == 0 && fn.Name != "main" {
				out.WriteString(fmt.Sprintf("const char* %s(void);\n", fn.Name))
			}
		}
		out.WriteString("\n")
	}

	// Emit forward declarations for all user-defined functions
	if g.usesConcurrency {
		for _, fn := range program.Functions {
			if fn.Name == "main" {
				continue
			}
			retType := g.cType(fn.ReturnType)
			out.WriteString(fmt.Sprintf("%s %s(", retType, fn.Name))
			for i, p := range fn.Params {
				if i > 0 {
					out.WriteString(", ")
				}
				out.WriteString(fmt.Sprintf("%s %s", g.cType(p.Type), p.Name))
			}
			if len(fn.Params) == 0 {
				out.WriteString("void")
			}
			out.WriteString(");\n")
		}
		out.WriteString("\n")
	}

	// First pass: generate functions to collect spawn wrappers
	var funcBuf strings.Builder
	for i, fn := range program.Functions {
		if i > 0 {
			funcBuf.WriteString("\n")
		}
		g.genFunction(&funcBuf, &fn)
	}

	// Emit spawn wrappers (before function bodies)
	if g.spawnWrappers.Len() > 0 {
		out.WriteString(g.spawnWrappers.String())
		out.WriteString("\n")
	}

	out.WriteString(funcBuf.String())

	return out.String()
}

func (g *Generator) scan(program *ast.Program) {
	for _, fn := range program.Functions {
		g.scanType(fn.ReturnType)
		for _, p := range fn.Params {
			g.scanType(p.Type)
		}
		for _, stmt := range fn.Body {
			g.scanStmt(stmt)
		}
	}
}

func (g *Generator) scanType(t ast.Type) {
	if t == ast.TypeBool {
		g.usesBool = true
	}
	if t == ast.TypeString {
		g.usesString = true
	}
	if ast.IsArrayType(t) {
		g.usesArray = true
		g.usesSafety = true
	}
	if ast.IsFuncType(t) {
		g.funcTypedef(t) // register the typedef
	}
}

func (g *Generator) scanStmt(stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		g.scanType(s.Type)
		g.scanExpr(s.Value)
	case *ast.ReturnStmt:
		g.scanExpr(s.Value)
	case *ast.ExprStmt:
		g.scanExpr(s.Expr)
	case *ast.AssignStmt:
		g.scanExpr(s.Value)
	case *ast.IndexAssignStmt:
		g.scanExpr(s.Array)
		g.scanExpr(s.Index)
		g.scanExpr(s.Value)
	case *ast.IfStmt:
		g.scanExpr(s.Cond)
		for _, stmt := range s.Then {
			g.scanStmt(stmt)
		}
		for _, stmt := range s.Else {
			g.scanStmt(stmt)
		}
	case *ast.WhileStmt:
		g.scanExpr(s.Cond)
		for _, stmt := range s.Body {
			g.scanStmt(stmt)
		}
	case *ast.ForStmt:
		g.scanStmt(s.Init)
		g.scanExpr(s.Cond)
		g.scanStmt(s.Post)
		for _, stmt := range s.Body {
			g.scanStmt(stmt)
		}
	case *ast.ForeachStmt:
		g.scanExpr(s.Iterable)
		for _, stmt := range s.Body {
			g.scanStmt(stmt)
		}
	case *ast.BlockStmt:
		for _, stmt := range s.Stmts {
			g.scanStmt(stmt)
		}
	case *ast.FieldAssignStmt:
		g.scanExpr(s.Object)
		g.scanExpr(s.Value)
	case *ast.CompoundAssignStmt:
		g.scanExpr(s.Value)
	case *ast.SendStmt:
		if s.Target != nil {
			g.scanExpr(s.Target)
		}
		g.scanExpr(s.Value)
		g.usesConcurrency = true
	}
}

func (g *Generator) scanExpr(expr ast.Expr) {
	switch e := expr.(type) {
	case *ast.BoolLit:
		g.usesBool = true
	case *ast.StringLit:
		g.usesString = true
	case *ast.BinaryExpr:
		if e.Op == ast.BinDiv || e.Op == ast.BinMod {
			g.usesSafety = true
		}
		g.scanExpr(e.Left)
		g.scanExpr(e.Right)
	case *ast.UnaryExpr:
		g.scanExpr(e.Operand)
	case *ast.CallExpr:
		// Scan for bool usage from json module
		if e.Module == "json" {
			g.usesBool = true
		}
		// Scan for assert usage
		if e.Module == "" && e.Name == "assert" {
			g.usesAssert = true
			g.usesBool = true
		}
		for _, arg := range e.Args {
			g.scanExpr(arg)
		}
	case *ast.ArrayLitExpr:
		g.usesArray = true
		for _, elem := range e.Elems {
			g.scanExpr(elem)
		}
	case *ast.IndexExpr:
		g.scanExpr(e.Array)
		g.scanExpr(e.Index)
	case *ast.StructLitExpr:
		for _, v := range e.FieldValues {
			g.scanExpr(v)
		}
	case *ast.FieldAccessExpr:
		g.scanExpr(e.Object)
	case *ast.SpawnExpr:
		g.usesConcurrency = true
		if e.Body != nil {
			for _, stmt := range e.Body {
				g.scanStmt(stmt)
			}
		}
		if e.Call != nil {
			g.scanExpr(e.Call)
		}
	case *ast.ChannelExpr:
		g.usesConcurrency = true
	case *ast.ReceiveExpr:
		g.usesConcurrency = true
		g.scanExpr(e.Source)
	}
}

func (g *Generator) genFunction(out *strings.Builder, fn *ast.Function) {
	// Reset var tracking for this function scope
	g.strVars = make(map[string]bool)
	g.strLenVars = make(map[string]bool)
	g.arrVars = make(map[string]ast.Type)
	g.structVars = make(map[string]ast.Type)
	g.varTypes = make(map[string]ast.Type)
	g.foreachCounter = 0

	// Register params
	for _, p := range fn.Params {
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

	// For main(), always emit "int main" in C regardless of Dex return type
	retType := g.cType(fn.ReturnType)
	if fn.Name == "main" {
		retType = "int"
	}
	name := fn.Name

	if fn.ReturnType == ast.TypeString && len(fn.Params) == 0 {
		out.WriteString(fmt.Sprintf("%s %s(void) {\n", retType, name))
	} else {
		out.WriteString(fmt.Sprintf("%s %s(", retType, name))
		for i, p := range fn.Params {
			if i > 0 {
				out.WriteString(", ")
			}
			out.WriteString(fmt.Sprintf("%s %s", g.cType(p.Type), p.Name))
		}
		out.WriteString(") {\n")
	}

	for _, stmt := range fn.Body {
		g.genStmt(out, stmt, 1)
	}

	// For void main(), insert implicit return 0
	if fn.Name == "main" && fn.ReturnType == ast.TypeVoid {
		out.WriteString("    return 0;\n")
	}

	out.WriteString("}\n")
}

func (g *Generator) genStmt(out *strings.Builder, stmt ast.Stmt, indent int) {
	prefix := strings.Repeat("    ", indent)

	switch s := stmt.(type) {
	case *ast.LetStmt:
		g.varTypes[s.Name] = s.Type
		if s.Type == ast.TypeString {
			g.strVars[s.Name] = true
		}
		if ast.IsArrayType(s.Type) {
			g.arrVars[s.Name] = s.Type
		}
		if ast.IsStructType(s.Type) {
			g.structVars[s.Name] = s.Type
		}
		// Special case for channel/task types: let ch = channel(int), let t = spawn { ... }
		if ast.IsChanType(s.Type) || ast.IsTaskType(s.Type) {
			// Already handled by varTypes above; ensure cType works
		}
		// Special case for receive expression: let val = receive(task)
		if recvExpr, ok := s.Value.(*ast.ReceiveExpr); ok {
			ctyp := g.cType(s.Type)
			out.WriteString(fmt.Sprintf("%s%s %s; dex_chan_recv(", prefix, ctyp, s.Name))
			g.genExpr(out, recvExpr.Source)
			out.WriteString(fmt.Sprintf(", &%s);\n", s.Name))
			break
		}
		// Special case for array literal value
		if arrLit, ok := s.Value.(*ast.ArrayLitExpr); ok {
			cNewFn := g.arrayNewFunc(s.Type)
			out.WriteString(fmt.Sprintf("%s%s %s = %s();\n", prefix, g.cType(s.Type), s.Name, cNewFn))
			if len(arrLit.Elems) > 0 {
				// Inline initialize data
				for i, elem := range arrLit.Elems {
					out.WriteString(fmt.Sprintf("%s%s.data[%d] = ", prefix, s.Name, i))
					g.genExpr(out, elem)
					out.WriteString(";\n")
				}
				out.WriteString(fmt.Sprintf("%s%s.len = %d;\n", prefix, s.Name, len(arrLit.Elems)))
			}
			break
		}
		// Special case for string declarations: heap-owned + shadow length
		if s.Type == ast.TypeString {
			if strLit, ok := s.Value.(*ast.StringLit); ok {
				out.WriteString(fmt.Sprintf("%sconst char* %s = strdup(%q);\n", prefix, s.Name, strLit.Value))
				out.WriteString(fmt.Sprintf("%ssize_t _dex_slen_%s = %d;\n", prefix, s.Name, len(strLit.Value)))
			} else {
				out.WriteString(fmt.Sprintf("%sconst char* %s = ", prefix, s.Name))
				g.genExpr(out, s.Value)
				out.WriteString(";\n")
				out.WriteString(fmt.Sprintf("%ssize_t _dex_slen_%s = strlen(%s);\n", prefix, s.Name, s.Name))
			}
			g.strLenVars[s.Name] = true
			break
		}
		constPrefix := ""
		if s.IsConst {
			constPrefix = "const "
		}
		out.WriteString(fmt.Sprintf("%s%s%s %s = ", prefix, constPrefix, g.cType(s.Type), s.Name))
		g.genExpr(out, s.Value)
		out.WriteString(";\n")

	case *ast.ReturnStmt:
		out.WriteString(fmt.Sprintf("%sreturn ", prefix))
		g.genExpr(out, s.Value)
		out.WriteString(";\n")

	case *ast.ExprStmt:
		// Fire-and-forget spawn: suppress unused value warning by casting to void
		if _, ok := s.Expr.(*ast.SpawnExpr); ok {
			out.WriteString(prefix + "(void)")
			g.genExpr(out, s.Expr)
			out.WriteString(";\n")
			break
		}
		out.WriteString(prefix)
		g.genExpr(out, s.Expr)
		out.WriteString(";\n")

	case *ast.AssignStmt:
		// Optimized string concat: s = s + expr → use dex_str_concat_len + free old
		if g.strLenVars[s.Name] {
			if binExpr, ok := s.Value.(*ast.BinaryExpr); ok && binExpr.Op == ast.BinAdd {
				if ident, ok := binExpr.Left.(*ast.Ident); ok && ident.Name == s.Name {
					out.WriteString(fmt.Sprintf("%s{ const char* _dex_old = %s; ", prefix, s.Name))
					out.WriteString(fmt.Sprintf("%s = dex_str_concat_len(%s, _dex_slen_%s, ", s.Name, "_dex_old", s.Name))
					g.genExpr(out, binExpr.Right)
					out.WriteString(fmt.Sprintf(", &_dex_slen_%s); free((char*)_dex_old); }\n", s.Name))
					break
				}
			}
			// Non-concat reassignment: free old, update shadow length
			out.WriteString(fmt.Sprintf("%s{ const char* _dex_old = %s; ", prefix, s.Name))
			out.WriteString(fmt.Sprintf("%s = ", s.Name))
			g.genExpr(out, s.Value)
			out.WriteString(fmt.Sprintf("; _dex_slen_%s = strlen(%s); free((char*)_dex_old); }\n", s.Name, s.Name))
			break
		}
		out.WriteString(fmt.Sprintf("%s%s = ", prefix, s.Name))
		g.genExpr(out, s.Value)
		out.WriteString(";\n")

	case *ast.IfStmt:
		out.WriteString(fmt.Sprintf("%sif (", prefix))
		g.genExprNoParen(out, s.Cond)
		out.WriteString(") {\n")
		for _, stmt := range s.Then {
			g.genStmt(out, stmt, indent+1)
		}
		if s.Else != nil {
			out.WriteString(fmt.Sprintf("%s} else {\n", prefix))
			for _, stmt := range s.Else {
				g.genStmt(out, stmt, indent+1)
			}
		}
		out.WriteString(fmt.Sprintf("%s}\n", prefix))

	case *ast.WhileStmt:
		out.WriteString(fmt.Sprintf("%swhile (", prefix))
		g.genExprNoParen(out, s.Cond)
		out.WriteString(") {\n")
		for _, stmt := range s.Body {
			g.genStmt(out, stmt, indent+1)
		}
		out.WriteString(fmt.Sprintf("%s}\n", prefix))

	case *ast.ForStmt:
		out.WriteString(fmt.Sprintf("%sfor (", prefix))
		g.genForInit(out, s.Init)
		out.WriteString("; ")
		g.genExprNoParen(out, s.Cond)
		out.WriteString("; ")
		g.genForPost(out, s.Post)
		out.WriteString(") {\n")
		for _, stmt := range s.Body {
			g.genStmt(out, stmt, indent+1)
		}
		out.WriteString(fmt.Sprintf("%s}\n", prefix))

	case *ast.ForeachStmt:
		idx := g.foreachCounter
		g.foreachCounter++
		idxVar := fmt.Sprintf("_foreach_idx_%d", idx)
		// Determine the array expression name for .len and .data access
		arrExpr := g.exprToString(s.Iterable)
		// Get element type from the iterable
		arrType := g.typeOfExpr(s.Iterable)
		elemType := ast.ElementType(arrType)
		elemCType := g.cType(elemType)

		out.WriteString(fmt.Sprintf("%sfor (int %s = 0; %s < %s.len; %s++) {\n",
			prefix, idxVar, idxVar, arrExpr, idxVar))
		// Declare value variable
		innerPrefix := strings.Repeat("    ", indent+1)
		out.WriteString(fmt.Sprintf("%s%s %s = %s.data[%s];\n",
			innerPrefix, elemCType, s.ValueVar, arrExpr, idxVar))
		// Register the value variable type
		g.varTypes[s.ValueVar] = elemType
		if elemType == ast.TypeString {
			g.strVars[s.ValueVar] = true
		}
		// Declare index variable if used
		if s.IndexVar != "" {
			out.WriteString(fmt.Sprintf("%sint %s = %s;\n", innerPrefix, s.IndexVar, idxVar))
			g.varTypes[s.IndexVar] = ast.TypeInt
		}
		for _, stmt := range s.Body {
			g.genStmt(out, stmt, indent+1)
		}
		out.WriteString(fmt.Sprintf("%s}\n", prefix))

	case *ast.BreakStmt:
		out.WriteString(fmt.Sprintf("%sbreak;\n", prefix))

	case *ast.ContinueStmt:
		out.WriteString(fmt.Sprintf("%scontinue;\n", prefix))

	case *ast.IncrementStmt:
		out.WriteString(fmt.Sprintf("%s%s++;\n", prefix, s.Name))

	case *ast.DecrementStmt:
		out.WriteString(fmt.Sprintf("%s%s--;\n", prefix, s.Name))

	case *ast.CompoundAssignStmt:
		out.WriteString(fmt.Sprintf("%s%s %s= ", prefix, s.Name, g.cBinOp(s.Op)))
		g.genExpr(out, s.Value)
		out.WriteString(";\n")

	case *ast.IndexAssignStmt:
		out.WriteString(prefix)
		out.WriteString("dex_bounds_check(")
		g.genExpr(out, s.Index)
		out.WriteString(", ")
		g.genExpr(out, s.Array)
		out.WriteString(".len);\n")
		out.WriteString(prefix)
		g.genExpr(out, s.Array)
		out.WriteString(".data[")
		g.genExpr(out, s.Index)
		out.WriteString("] = ")
		g.genExpr(out, s.Value)
		out.WriteString(";\n")

	case *ast.FieldAssignStmt:
		out.WriteString(prefix)
		g.genExpr(out, s.Object)
		out.WriteString(fmt.Sprintf(".%s = ", s.Field))
		g.genExpr(out, s.Value)
		out.WriteString(";\n")

	case *ast.BlockStmt:
		out.WriteString(fmt.Sprintf("%s{\n", prefix))
		for _, stmt := range s.Stmts {
			g.genStmt(out, stmt, indent+1)
		}
		out.WriteString(fmt.Sprintf("%s}\n", prefix))

	case *ast.SendStmt:
		valType := g.typeOfExpr(s.Value)
		ctyp := g.cType(valType)
		out.WriteString(fmt.Sprintf("%s{ %s _send_val = ", prefix, ctyp))
		g.genExpr(out, s.Value)
		out.WriteString("; dex_chan_send(")
		if s.Target != nil {
			g.genExpr(out, s.Target)
		} else {
			out.WriteString("_ch")
		}
		out.WriteString(", &_send_val); }\n")
	}
}

// genForInit generates the init part of a for loop (no trailing semicolon).
func (g *Generator) genForInit(out *strings.Builder, stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		g.varTypes[s.Name] = s.Type
		if s.Type == ast.TypeString {
			g.strVars[s.Name] = true
		}
		if ast.IsArrayType(s.Type) {
			g.arrVars[s.Name] = s.Type
		}
		out.WriteString(fmt.Sprintf("%s %s = ", g.cType(s.Type), s.Name))
		g.genExpr(out, s.Value)
	case *ast.AssignStmt:
		out.WriteString(fmt.Sprintf("%s = ", s.Name))
		g.genExpr(out, s.Value)
	}
}

// genForPost generates the post part of a for loop (no trailing semicolon).
func (g *Generator) genForPost(out *strings.Builder, stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.IncrementStmt:
		out.WriteString(fmt.Sprintf("%s++", s.Name))
	case *ast.DecrementStmt:
		out.WriteString(fmt.Sprintf("%s--", s.Name))
	case *ast.CompoundAssignStmt:
		out.WriteString(fmt.Sprintf("%s %s= ", s.Name, g.cBinOp(s.Op)))
		g.genExpr(out, s.Value)
	case *ast.AssignStmt:
		out.WriteString(fmt.Sprintf("%s = ", s.Name))
		g.genExpr(out, s.Value)
	}
}

// exprToString renders an expression to a string (for use in foreach).
func (g *Generator) exprToString(expr ast.Expr) string {
	var buf strings.Builder
	g.genExpr(&buf, expr)
	return buf.String()
}

func (g *Generator) genExpr(out *strings.Builder, expr ast.Expr) {
	switch e := expr.(type) {
	case *ast.IntLit:
		out.WriteString(fmt.Sprintf("%d", e.Value))

	case *ast.CharLit:
		switch e.Value {
		case '\'':
			out.WriteString("'\\''")
		case '\\':
			out.WriteString("'\\\\'")
		case '\n':
			out.WriteString("'\\n'")
		case '\t':
			out.WriteString("'\\t'")
		default:
			out.WriteString(fmt.Sprintf("'%c'", e.Value))
		}

	case *ast.FloatLit:
		out.WriteString(fmt.Sprintf("%g", e.Value))

	case *ast.BoolLit:
		if e.Value {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}

	case *ast.StringLit:
		out.WriteString(fmt.Sprintf("%q", e.Value))

	case *ast.Ident:
		out.WriteString(e.Name)

	case *ast.BinaryExpr:
		// Check if this is a string operation
		if g.isStringExpr(e.Left) || g.isStringExpr(e.Right) {
			switch e.Op {
			case ast.BinAdd:
				out.WriteString("dex_str_concat(")
				g.genExpr(out, e.Left)
				out.WriteString(", ")
				g.genExpr(out, e.Right)
				out.WriteString(")")
				return
			case ast.BinEq, ast.BinStrictEq:
				out.WriteString("(strcmp(")
				g.genExpr(out, e.Left)
				out.WriteString(", ")
				g.genExpr(out, e.Right)
				out.WriteString(") == 0)")
				return
			case ast.BinNeq, ast.BinStrictNeq:
				out.WriteString("(strcmp(")
				g.genExpr(out, e.Left)
				out.WriteString(", ")
				g.genExpr(out, e.Right)
				out.WriteString(") != 0)")
				return
			}
		}

		// Cross-numeric operations: cast narrower operand to wider type
		if e.HasMixedTypes {
			widerType := g.widerNumericType(e.LeftType, e.RightType)
			castType := g.cType(widerType)
			out.WriteString("(")
			if e.LeftType != widerType {
				out.WriteString(fmt.Sprintf("(%s)", castType))
			}
			g.genExpr(out, e.Left)
			out.WriteString(fmt.Sprintf(" %s ", g.cBinOp(e.Op)))
			if e.Op == ast.BinDiv || e.Op == ast.BinMod {
				out.WriteString(g.nonzeroCheckFunc(widerType) + "(")
			}
			if e.RightType != widerType {
				out.WriteString(fmt.Sprintf("(%s)", castType))
			}
			g.genExpr(out, e.Right)
			if e.Op == ast.BinDiv || e.Op == ast.BinMod {
				out.WriteString(")")
			}
			out.WriteString(")")
			return
		}

		out.WriteString("(")
		g.genExpr(out, e.Left)
		out.WriteString(fmt.Sprintf(" %s ", g.cBinOp(e.Op)))
		if e.Op == ast.BinDiv || e.Op == ast.BinMod {
			rightType := g.typeOfExpr(e.Right)
			out.WriteString(g.nonzeroCheckFunc(rightType) + "(")
			g.genExpr(out, e.Right)
			out.WriteString(")")
		} else {
			g.genExpr(out, e.Right)
		}
		out.WriteString(")")

	case *ast.UnaryExpr:
		out.WriteString("(")
		switch e.Op {
		case ast.UnaryNeg:
			out.WriteString("-")
		case ast.UnaryNot:
			out.WriteString("!")
		}
		g.genExpr(out, e.Operand)
		out.WriteString(")")

	case *ast.IndexExpr:
		out.WriteString("(dex_bounds_check(")
		g.genExpr(out, e.Index)
		out.WriteString(", ")
		g.genExpr(out, e.Array)
		out.WriteString(".len), ")
		g.genExpr(out, e.Array)
		out.WriteString(".data[")
		g.genExpr(out, e.Index)
		out.WriteString("])")

	case *ast.ArrayLitExpr:
		// Non-let context — shouldn't normally happen since checker ensures array literals
		// are used in let statements, but handle defensively
		out.WriteString("/* array literal */")

	case *ast.StructLitExpr:
		cName := "Dex_" + e.Name
		out.WriteString(fmt.Sprintf("(%s){ ", cName))
		for i, fn := range e.FieldNames {
			if i > 0 {
				out.WriteString(", ")
			}
			out.WriteString(fmt.Sprintf(".%s = ", fn))
			g.genExpr(out, e.FieldValues[i])
		}
		out.WriteString(" }")

	case *ast.FieldAccessExpr:
		g.genExpr(out, e.Object)
		out.WriteString(fmt.Sprintf(".%s", e.Field))

	case *ast.CallExpr:
		g.genCallExpr(out, e)

	case *ast.SpawnExpr:
		g.genSpawnExpr(out, e)

	case *ast.ChannelExpr:
		ctyp := g.cType(e.ElemType)
		out.WriteString(fmt.Sprintf("dex_chan_new(sizeof(%s), 64)", ctyp))

	case *ast.ReceiveExpr:
		// receive in expression context — should typically be handled by LetStmt
		// Generate a temp variable
		srcType := g.typeOfExpr(e.Source)
		var elemType ast.Type
		if ast.IsChanType(srcType) {
			elemType = ast.ChanElemType(srcType)
		} else if ast.IsTaskType(srcType) {
			elemType = ast.TaskReturnType(srcType)
		} else {
			elemType = ast.TypeInt
		}
		ctyp := g.cType(elemType)
		// This is for nested expressions. For let statements, genStmt handles it specially.
		out.WriteString(fmt.Sprintf("({ %s _recv_tmp; dex_chan_recv(", ctyp))
		g.genExpr(out, e.Source)
		out.WriteString(", &_recv_tmp); _recv_tmp; })")
	}
}

func (g *Generator) genCallExpr(out *strings.Builder, e *ast.CallExpr) {
	// Special case: fmt.print — polymorphic print for any primitive type
	if e.Module == "fmt" && e.Name == "print" {
		argType := g.typeOfExpr(e.Args[0])
		var fmtStr string
		switch argType {
		case ast.TypeChar:
			fmtStr = "%c"
		case ast.TypeInt:
			fmtStr = "%d"
		case ast.TypeLong:
			fmtStr = "%ld"
		case ast.TypeDouble:
			fmtStr = "%f"
		case ast.TypeString:
			fmtStr = "%s"
		case ast.TypeBool:
			// Print bools as "true"/"false"
			out.WriteString("printf(\"%s\\n\", ")
			out.WriteString("(")
			g.genExpr(out, e.Args[0])
			out.WriteString(") ? \"true\" : \"false\")")
			return
		default:
			fmtStr = "%d"
		}
		out.WriteString(fmt.Sprintf("printf(\"%s\\n\", ", fmtStr))
		g.genExpr(out, e.Args[0])
		out.WriteString(")")
		return
	}
	// json.stringify(array) — special codegen
	if e.Module == "json" && e.Name == "stringify" {
		// Determine array type from the argument
		argIdent, ok := e.Args[0].(*ast.Ident)
		if ok {
			arrType := g.arrVars[argIdent.Name]
			switch arrType {
			case ast.TypeArrayInt:
				out.WriteString(fmt.Sprintf("dex_json_stringify_int(&%s)", argIdent.Name))
			case ast.TypeArrayBool:
				out.WriteString(fmt.Sprintf("dex_json_stringify_bool(&%s)", argIdent.Name))
			case ast.TypeArrayString:
				out.WriteString(fmt.Sprintf("dex_json_stringify_str(&%s)", argIdent.Name))
			case ast.TypeArrayLong:
				out.WriteString(fmt.Sprintf("dex_json_stringify_long(&%s)", argIdent.Name))
			case ast.TypeArrayDouble:
				out.WriteString(fmt.Sprintf("dex_json_stringify_double(&%s)", argIdent.Name))
			case ast.TypeArrayChar:
				out.WriteString(fmt.Sprintf("dex_json_stringify_char(&%s)", argIdent.Name))
			}
		}
		return
	}

	// json.set_arr(obj, key, array) — special codegen
	if e.Module == "json" && e.Name == "set_arr" {
		argIdent, ok := e.Args[2].(*ast.Ident)
		if ok {
			arrType := g.arrVars[argIdent.Name]
			var fn string
			switch arrType {
			case ast.TypeArrayInt:
				fn = "dex_json_set_arr_int"
			case ast.TypeArrayBool:
				fn = "dex_json_set_arr_bool"
			case ast.TypeArrayString:
				fn = "dex_json_set_arr_str"
			case ast.TypeArrayLong:
				fn = "dex_json_set_arr_long"
			case ast.TypeArrayDouble:
				fn = "dex_json_set_arr_double"
			case ast.TypeArrayChar:
				fn = "dex_json_set_arr_char"
			}
			out.WriteString(fmt.Sprintf("%s(", fn))
			g.genExpr(out, e.Args[0])
			out.WriteString(", ")
			g.genExpr(out, e.Args[1])
			out.WriteString(fmt.Sprintf(", &%s)", argIdent.Name))
		}
		return
	}

	// json.set(obj, key, value) — polymorphic: dispatch by value type
	if e.Module == "json" && e.Name == "set" {
		valType := g.typeOfExpr(e.Args[2])
		var fn string
		switch valType {
		case ast.TypeInt:
			fn = "dex_json_set_int"
		case ast.TypeBool:
			fn = "dex_json_set_bool"
		case ast.TypeLong:
			fn = "dex_json_set_long"
		case ast.TypeDouble:
			fn = "dex_json_set_double"
		default:
			fn = "dex_json_set"
		}
		out.WriteString(fn + "(")
		g.genExpr(out, e.Args[0])
		out.WriteString(", ")
		g.genExpr(out, e.Args[1])
		out.WriteString(", ")
		g.genExpr(out, e.Args[2])
		out.WriteString(")")
		return
	}

	// db.col(rows, col) — polymorphic: dispatch by resolved return type
	if e.Module == "db" && e.Name == "col" {
		var fn string
		switch e.ResolvedType {
		case ast.TypeString:
			fn = "dex_db_col_str"
		case ast.TypeBool:
			fn = "dex_db_col_bool"
		case ast.TypeDouble:
			fn = "dex_db_col_double"
		default:
			fn = "dex_db_col_int"
		}
		out.WriteString(fn + "(")
		g.genExpr(out, e.Args[0])
		out.WriteString(", ")
		g.genExpr(out, e.Args[1])
		out.WriteString(")")
		return
	}

	if e.Module == "http" && e.Name == "route" {
		// route("GET", "/path", handler) -> dex_route("GET", "/path", handler_name)
		out.WriteString("dex_route(")
		g.genExpr(out, e.Args[0])
		out.WriteString(", ")
		g.genExpr(out, e.Args[1])
		out.WriteString(", ")
		// Resolve handler name to function pointer
		switch h := e.Args[2].(type) {
		case *ast.StringLit:
			out.WriteString(h.Value)
		case *ast.Ident:
			out.WriteString(h.Name)
		}
		out.WriteString(")")
		return
	}

	// Built-in: close(channel)
	if e.Module == "" && e.Name == "close" {
		out.WriteString("dex_chan_close(")
		g.genExpr(out, e.Args[0])
		out.WriteString(")")
		return
	}

	// Built-in: assert(condition)
	if e.Module == "" && e.Name == "assert" {
		out.WriteString("if (!(")
		g.genExpr(out, e.Args[0])
		out.WriteString(")) { fprintf(stderr, \"FAIL: assert failed\\n\"); exit(1); }")
		return
	}

	// Check if this is an array method call (e.Module is a variable name)
	if e.Module != "" {
		if arrType, ok := g.arrVars[e.Module]; ok {
			switch e.Name {
			case "push":
				pushFn := g.arrayPushFunc(arrType)
				out.WriteString(fmt.Sprintf("%s(&%s, ", pushFn, e.Module))
				g.genExpr(out, e.Args[0])
				out.WriteString(")")
				return
			case "len":
				out.WriteString(fmt.Sprintf("%s.len", e.Module))
				return
			case "pop":
				popFn := g.arrayPopFunc(arrType)
				out.WriteString(fmt.Sprintf("%s(&%s)", popFn, e.Module))
				return
			case "remove":
				removeFn := g.arrayRemoveFunc(arrType)
				out.WriteString(fmt.Sprintf("%s(&%s, ", removeFn, e.Module))
				g.genExpr(out, e.Args[0])
				out.WriteString(")")
				return
			case "contains":
				containsFn := g.arrayContainsFunc(arrType)
				out.WriteString(fmt.Sprintf("%s(&%s, ", containsFn, e.Module))
				g.genExpr(out, e.Args[0])
				out.WriteString(")")
				return
			case "indexOf":
				indexOfFn := g.arrayIndexOfFunc(arrType)
				out.WriteString(fmt.Sprintf("%s(&%s, ", indexOfFn, e.Module))
				g.genExpr(out, e.Args[0])
				out.WriteString(")")
				return
			case "reverse":
				reverseFn := g.arrayReverseFunc(arrType)
				out.WriteString(fmt.Sprintf("%s(&%s)", reverseFn, e.Module))
				return
			case "sort":
				sortArg := e.Args[0].(*ast.StringLit).Value
				var sortFn string
				if sortArg == "asc" {
					sortFn = g.arraySortAscFunc(arrType)
				} else {
					sortFn = g.arraySortDescFunc(arrType)
				}
				out.WriteString(fmt.Sprintf("%s(&%s)", sortFn, e.Module))
				return
			}
		}
	}

	// Qualified call with CName — look up from stdlib
	if e.Module != "" {
		funcDef, ok := stdlib.LookupFunc(e.Module, e.Name)
		if ok && funcDef.CName != "" {
			out.WriteString(funcDef.CName)
			out.WriteString("(")
			for i, arg := range e.Args {
				if i > 0 {
					out.WriteString(", ")
				}
				g.genExpr(out, arg)
			}
			out.WriteString(")")
			return
		}
	}

	// User-defined function call
	out.WriteString(e.Name)
	out.WriteString("(")
	for i, arg := range e.Args {
		if i > 0 {
			out.WriteString(", ")
		}
		g.genExpr(out, arg)
	}
	out.WriteString(")")
}

func (g *Generator) genSpawnExpr(out *strings.Builder, e *ast.SpawnExpr) {
	idx := g.spawnCounter
	g.spawnCounter++
	wrapperName := fmt.Sprintf("_dex_spawn_%d", idx)
	ctxType := fmt.Sprintf("_dex_spawn_%d_ctx", idx)

	if e.Body != nil {
		// Spawn block: spawn { body }
		// Determine captured variables from outer scope used in body
		captured := g.findCapturedVars(e.Body)

		// Build context struct
		g.spawnWrappers.WriteString(fmt.Sprintf("typedef struct { DexChan* _ch;"))
		for _, cv := range captured {
			g.spawnWrappers.WriteString(fmt.Sprintf(" %s %s;", g.cType(cv.typ), cv.name))
		}
		g.spawnWrappers.WriteString(fmt.Sprintf(" } %s;\n", ctxType))

		// Build wrapper function
		g.spawnWrappers.WriteString(fmt.Sprintf("void* %s(void* _raw) {\n", wrapperName))
		g.spawnWrappers.WriteString(fmt.Sprintf("    %s* _ctx = (%s*)_raw;\n", ctxType, ctxType))
		g.spawnWrappers.WriteString("    DexChan* _ch = _ctx->_ch;\n")
		for _, cv := range captured {
			g.spawnWrappers.WriteString(fmt.Sprintf("    %s %s = _ctx->%s;\n", g.cType(cv.typ), cv.name, cv.name))
		}
		g.spawnWrappers.WriteString("    free(_raw);\n")

		// Generate body statements into wrapper
		var bodyBuf strings.Builder
		for _, stmt := range e.Body {
			g.genStmt(&bodyBuf, stmt, 1)
		}
		g.spawnWrappers.WriteString(bodyBuf.String())
		g.spawnWrappers.WriteString("    return NULL;\n")
		g.spawnWrappers.WriteString("}\n")

		// Generate call site inline (using GCC statement expression)
		retCType := g.cType(e.ReturnType)
		if retCType == "void" {
			retCType = "int" // placeholder for sizeof in fire-and-forget
		}
		out.WriteString(fmt.Sprintf("({ DexChan* _spawn_ch = dex_chan_new(sizeof(%s), 64); ", retCType))
		out.WriteString(fmt.Sprintf("%s* _spawn_ctx = (%s*)malloc(sizeof(%s)); ", ctxType, ctxType, ctxType))
		out.WriteString("_spawn_ctx->_ch = _spawn_ch; ")
		for _, cv := range captured {
			out.WriteString(fmt.Sprintf("_spawn_ctx->%s = %s; ", cv.name, cv.name))
		}
		out.WriteString(fmt.Sprintf("pthread_t _spawn_t_%d; ", idx))
		out.WriteString(fmt.Sprintf("pthread_create(&_spawn_t_%d, NULL, %s, _spawn_ctx); ", idx, wrapperName))
		out.WriteString(fmt.Sprintf("pthread_detach(_spawn_t_%d); ", idx))
		out.WriteString("_spawn_ch; })")
	} else if e.Call != nil {
		// Spawn function call: spawn fn(args)
		call := e.Call.(*ast.CallExpr)

		// Build context struct with channel + args
		g.spawnWrappers.WriteString(fmt.Sprintf("typedef struct { DexChan* _ch;"))
		for i, arg := range call.Args {
			argType := g.typeOfExpr(arg)
			g.spawnWrappers.WriteString(fmt.Sprintf(" %s _a%d;", g.cType(argType), i))
		}
		g.spawnWrappers.WriteString(fmt.Sprintf(" } %s;\n", ctxType))

		// Build wrapper function
		g.spawnWrappers.WriteString(fmt.Sprintf("void* %s(void* _raw) {\n", wrapperName))
		g.spawnWrappers.WriteString(fmt.Sprintf("    %s* _ctx = (%s*)_raw;\n", ctxType, ctxType))
		g.spawnWrappers.WriteString("    DexChan* _ch = _ctx->_ch;\n")
		for i, arg := range call.Args {
			argType := g.typeOfExpr(arg)
			g.spawnWrappers.WriteString(fmt.Sprintf("    %s _a%d = _ctx->_a%d;\n", g.cType(argType), i, i))
		}
		g.spawnWrappers.WriteString("    free(_raw);\n")

		// Call the function
		retType := e.ReturnType
		if retType != ast.TypeVoid {
			g.spawnWrappers.WriteString(fmt.Sprintf("    %s _ret = %s(", g.cType(retType), call.Name))
		} else {
			g.spawnWrappers.WriteString(fmt.Sprintf("    %s(", call.Name))
		}
		for i := range call.Args {
			if i > 0 {
				g.spawnWrappers.WriteString(", ")
			}
			g.spawnWrappers.WriteString(fmt.Sprintf("_a%d", i))
		}
		g.spawnWrappers.WriteString(");\n")
		if retType != ast.TypeVoid {
			g.spawnWrappers.WriteString("    dex_chan_send(_ch, &_ret);\n")
		}
		g.spawnWrappers.WriteString("    return NULL;\n")
		g.spawnWrappers.WriteString("}\n")

		// Generate call site
		retCType := g.cType(e.ReturnType)
		if retCType == "void" {
			retCType = "int" // use int for void tasks to have a valid sizeof
		}
		out.WriteString(fmt.Sprintf("({ DexChan* _spawn_ch = dex_chan_new(sizeof(%s), 1); ", retCType))
		out.WriteString(fmt.Sprintf("%s* _spawn_ctx = (%s*)malloc(sizeof(%s)); ", ctxType, ctxType, ctxType))
		out.WriteString("_spawn_ctx->_ch = _spawn_ch; ")
		for i, arg := range call.Args {
			out.WriteString(fmt.Sprintf("_spawn_ctx->_a%d = ", i))
			g.genExpr(out, arg)
			out.WriteString("; ")
		}
		out.WriteString(fmt.Sprintf("pthread_t _spawn_t_%d; ", idx))
		out.WriteString(fmt.Sprintf("pthread_create(&_spawn_t_%d, NULL, %s, _spawn_ctx); ", idx, wrapperName))
		out.WriteString(fmt.Sprintf("pthread_detach(_spawn_t_%d); ", idx))
		out.WriteString("_spawn_ch; })")
	}
}

type capturedVar struct {
	name string
	typ  ast.Type
}

// findCapturedVars identifies variables referenced in spawn body that are defined in the outer scope.
func (g *Generator) findCapturedVars(body []ast.Stmt) []capturedVar {
	used := make(map[string]bool)
	defined := make(map[string]bool) // vars defined within the spawn body
	g.collectUsedVars(body, used, defined)

	var result []capturedVar
	for name := range used {
		if defined[name] {
			continue
		}
		if typ, ok := g.varTypes[name]; ok {
			result = append(result, capturedVar{name: name, typ: typ})
		}
	}
	return result
}

func (g *Generator) collectUsedVars(stmts []ast.Stmt, used, defined map[string]bool) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.LetStmt:
			g.collectUsedVarsExpr(s.Value, used)
			defined[s.Name] = true
		case *ast.ExprStmt:
			g.collectUsedVarsExpr(s.Expr, used)
		case *ast.ReturnStmt:
			g.collectUsedVarsExpr(s.Value, used)
		case *ast.AssignStmt:
			g.collectUsedVarsExpr(s.Value, used)
			used[s.Name] = true
		case *ast.SendStmt:
			if s.Target != nil {
				g.collectUsedVarsExpr(s.Target, used)
			}
			g.collectUsedVarsExpr(s.Value, used)
		case *ast.IfStmt:
			g.collectUsedVarsExpr(s.Cond, used)
			g.collectUsedVars(s.Then, used, defined)
			g.collectUsedVars(s.Else, used, defined)
		case *ast.WhileStmt:
			g.collectUsedVarsExpr(s.Cond, used)
			g.collectUsedVars(s.Body, used, defined)
		case *ast.ForStmt:
			g.collectUsedVars([]ast.Stmt{s.Init}, used, defined)
			g.collectUsedVarsExpr(s.Cond, used)
			g.collectUsedVars([]ast.Stmt{s.Post}, used, defined)
			g.collectUsedVars(s.Body, used, defined)
		case *ast.ForeachStmt:
			g.collectUsedVarsExpr(s.Iterable, used)
			defined[s.ValueVar] = true
			if s.IndexVar != "" {
				defined[s.IndexVar] = true
			}
			g.collectUsedVars(s.Body, used, defined)
		case *ast.BlockStmt:
			g.collectUsedVars(s.Stmts, used, defined)
		case *ast.IncrementStmt:
			used[s.Name] = true
		case *ast.DecrementStmt:
			used[s.Name] = true
		case *ast.CompoundAssignStmt:
			used[s.Name] = true
			g.collectUsedVarsExpr(s.Value, used)
		case *ast.IndexAssignStmt:
			g.collectUsedVarsExpr(s.Array, used)
			g.collectUsedVarsExpr(s.Index, used)
			g.collectUsedVarsExpr(s.Value, used)
		case *ast.FieldAssignStmt:
			g.collectUsedVarsExpr(s.Object, used)
			g.collectUsedVarsExpr(s.Value, used)
		}
	}
}

func (g *Generator) collectUsedVarsExpr(expr ast.Expr, used map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.Ident:
		used[e.Name] = true
	case *ast.BinaryExpr:
		g.collectUsedVarsExpr(e.Left, used)
		g.collectUsedVarsExpr(e.Right, used)
	case *ast.UnaryExpr:
		g.collectUsedVarsExpr(e.Operand, used)
	case *ast.CallExpr:
		for _, arg := range e.Args {
			g.collectUsedVarsExpr(arg, used)
		}
	case *ast.IndexExpr:
		g.collectUsedVarsExpr(e.Array, used)
		g.collectUsedVarsExpr(e.Index, used)
	case *ast.FieldAccessExpr:
		g.collectUsedVarsExpr(e.Object, used)
	case *ast.ArrayLitExpr:
		for _, elem := range e.Elems {
			g.collectUsedVarsExpr(elem, used)
		}
	case *ast.StructLitExpr:
		for _, v := range e.FieldValues {
			g.collectUsedVarsExpr(v, used)
		}
	case *ast.SpawnExpr:
		if e.Body != nil {
			defined := make(map[string]bool)
			g.collectUsedVars(e.Body, used, defined)
		}
		if e.Call != nil {
			g.collectUsedVarsExpr(e.Call, used)
		}
	case *ast.ReceiveExpr:
		g.collectUsedVarsExpr(e.Source, used)
	case *ast.ChannelExpr:
		// no vars
	}
}

// genExprNoParen generates an expression without wrapping outer parens,
// used for if/while conditions which already provide parens.
func (g *Generator) genExprNoParen(out *strings.Builder, expr ast.Expr) {
	switch e := expr.(type) {
	case *ast.BinaryExpr:
		// Check string comparison in no-paren context too
		if g.isStringExpr(e.Left) || g.isStringExpr(e.Right) {
			switch e.Op {
			case ast.BinEq, ast.BinStrictEq:
				out.WriteString("strcmp(")
				g.genExpr(out, e.Left)
				out.WriteString(", ")
				g.genExpr(out, e.Right)
				out.WriteString(") == 0")
				return
			case ast.BinNeq, ast.BinStrictNeq:
				out.WriteString("strcmp(")
				g.genExpr(out, e.Left)
				out.WriteString(", ")
				g.genExpr(out, e.Right)
				out.WriteString(") != 0")
				return
			}
		}
		// Cross-numeric operations in no-paren context
		if e.HasMixedTypes {
			widerType := g.widerNumericType(e.LeftType, e.RightType)
			castType := g.cType(widerType)
			if e.LeftType != widerType {
				out.WriteString(fmt.Sprintf("(%s)", castType))
			}
			g.genExpr(out, e.Left)
			out.WriteString(fmt.Sprintf(" %s ", g.cBinOp(e.Op)))
			if e.Op == ast.BinDiv || e.Op == ast.BinMod {
				out.WriteString(g.nonzeroCheckFunc(widerType) + "(")
			}
			if e.RightType != widerType {
				out.WriteString(fmt.Sprintf("(%s)", castType))
			}
			g.genExpr(out, e.Right)
			if e.Op == ast.BinDiv || e.Op == ast.BinMod {
				out.WriteString(")")
			}
			return
		}
		g.genExpr(out, e.Left)
		out.WriteString(fmt.Sprintf(" %s ", g.cBinOp(e.Op)))
		if e.Op == ast.BinDiv || e.Op == ast.BinMod {
			rightType := g.typeOfExpr(e.Right)
			out.WriteString(g.nonzeroCheckFunc(rightType) + "(")
			g.genExpr(out, e.Right)
			out.WriteString(")")
		} else {
			g.genExpr(out, e.Right)
		}
	default:
		g.genExpr(out, expr)
	}
}

// isStringExpr checks if an expression is known to produce a string type.
func (g *Generator) isStringExpr(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.StringLit:
		return true
	case *ast.CallExpr:
		// Polymorphic return type: db.col uses ResolvedType
		if e.Module == "db" && e.Name == "col" {
			return e.ResolvedType == ast.TypeString
		}
		// Check stdlib functions
		if e.Module != "" {
			funcDef, ok := stdlib.LookupFunc(e.Module, e.Name)
			if ok {
				return funcDef.ReturnType == ast.TypeString
			}
			return false
		}
		// User-defined functions
		if fn, ok := g.funcs[e.Name]; ok {
			return fn.ReturnType == ast.TypeString
		}
	case *ast.Ident:
		return g.strVars[e.Name]
	case *ast.IndexExpr:
		// Check if indexing a string array
		if ident, ok := e.Array.(*ast.Ident); ok {
			if arrType, ok := g.arrVars[ident.Name]; ok {
				return arrType == ast.TypeArrayString
			}
		}
		return false
	case *ast.BinaryExpr:
		if e.Op == ast.BinAdd {
			return g.isStringExpr(e.Left) || g.isStringExpr(e.Right)
		}
	case *ast.FieldAccessExpr:
		return g.typeOfExpr(e) == ast.TypeString
	}
	return false
}

func (g *Generator) cType(t ast.Type) string {
	switch t {
	case ast.TypeInt:
		return "int"
	case ast.TypeBool:
		return "_Bool"
	case ast.TypeString:
		return "const char*"
	case ast.TypeLong:
		return "long"
	case ast.TypeDouble:
		return "double"
	case ast.TypeArrayInt:
		return "DexArrayInt"
	case ast.TypeArrayBool:
		return "DexArrayBool"
	case ast.TypeArrayString:
		return "DexArrayString"
	case ast.TypeArrayLong:
		return "DexArrayLong"
	case ast.TypeArrayDouble:
		return "DexArrayDouble"
	case ast.TypeChar:
		return "unsigned char"
	case ast.TypeArrayChar:
		return "DexArrayChar"
	default:
		if ast.IsStructType(t) {
			return "Dex_" + ast.StructName(t)
		}
		if ast.IsChanType(t) || ast.IsTaskType(t) {
			return "DexChan*"
		}
		if ast.IsFuncType(t) {
			return g.funcTypedef(t)
		}
		return "void"
	}
}

// funcTypedef returns (and lazily registers) a typedef name for a function pointer type.
func (g *Generator) funcTypedef(t ast.Type) string {
	if name, ok := g.funcTypedefs[t]; ok {
		return name
	}
	g.funcTypedefCnt++
	name := fmt.Sprintf("DexFn_%d", g.funcTypedefCnt)
	g.funcTypedefs[t] = name
	return name
}

func (g *Generator) arrayNewFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_new"
	case ast.TypeArrayBool:
		return "dex_array_bool_new"
	case ast.TypeArrayString:
		return "dex_array_string_new"
	case ast.TypeArrayLong:
		return "dex_array_long_new"
	case ast.TypeArrayDouble:
		return "dex_array_double_new"
	case ast.TypeArrayChar:
		return "dex_array_char_new"
	default:
		return ""
	}
}

func (g *Generator) arrayPushFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_push"
	case ast.TypeArrayBool:
		return "dex_array_bool_push"
	case ast.TypeArrayString:
		return "dex_array_string_push"
	case ast.TypeArrayLong:
		return "dex_array_long_push"
	case ast.TypeArrayDouble:
		return "dex_array_double_push"
	case ast.TypeArrayChar:
		return "dex_array_char_push"
	default:
		return ""
	}
}

func (g *Generator) arrayPopFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_pop"
	case ast.TypeArrayBool:
		return "dex_array_bool_pop"
	case ast.TypeArrayString:
		return "dex_array_string_pop"
	case ast.TypeArrayLong:
		return "dex_array_long_pop"
	case ast.TypeArrayDouble:
		return "dex_array_double_pop"
	case ast.TypeArrayChar:
		return "dex_array_char_pop"
	default:
		return ""
	}
}

func (g *Generator) arrayRemoveFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_remove"
	case ast.TypeArrayBool:
		return "dex_array_bool_remove"
	case ast.TypeArrayString:
		return "dex_array_string_remove"
	case ast.TypeArrayLong:
		return "dex_array_long_remove"
	case ast.TypeArrayDouble:
		return "dex_array_double_remove"
	case ast.TypeArrayChar:
		return "dex_array_char_remove"
	default:
		return ""
	}
}

func (g *Generator) arrayContainsFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_contains"
	case ast.TypeArrayBool:
		return "dex_array_bool_contains"
	case ast.TypeArrayString:
		return "dex_array_string_contains"
	case ast.TypeArrayLong:
		return "dex_array_long_contains"
	case ast.TypeArrayDouble:
		return "dex_array_double_contains"
	case ast.TypeArrayChar:
		return "dex_array_char_contains"
	default:
		return ""
	}
}

func (g *Generator) arrayIndexOfFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_indexOf"
	case ast.TypeArrayBool:
		return "dex_array_bool_indexOf"
	case ast.TypeArrayString:
		return "dex_array_string_indexOf"
	case ast.TypeArrayLong:
		return "dex_array_long_indexOf"
	case ast.TypeArrayDouble:
		return "dex_array_double_indexOf"
	case ast.TypeArrayChar:
		return "dex_array_char_indexOf"
	default:
		return ""
	}
}

func (g *Generator) arrayReverseFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_reverse"
	case ast.TypeArrayBool:
		return "dex_array_bool_reverse"
	case ast.TypeArrayString:
		return "dex_array_string_reverse"
	case ast.TypeArrayLong:
		return "dex_array_long_reverse"
	case ast.TypeArrayDouble:
		return "dex_array_double_reverse"
	case ast.TypeArrayChar:
		return "dex_array_char_reverse"
	default:
		return ""
	}
}

func (g *Generator) arraySortAscFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_sort_asc"
	case ast.TypeArrayString:
		return "dex_array_string_sort_asc"
	case ast.TypeArrayLong:
		return "dex_array_long_sort_asc"
	case ast.TypeArrayDouble:
		return "dex_array_double_sort_asc"
	case ast.TypeArrayChar:
		return "dex_array_char_sort_asc"
	default:
		return ""
	}
}

func (g *Generator) arraySortDescFunc(t ast.Type) string {
	switch t {
	case ast.TypeArrayInt:
		return "dex_array_int_sort_desc"
	case ast.TypeArrayString:
		return "dex_array_string_sort_desc"
	case ast.TypeArrayLong:
		return "dex_array_long_sort_desc"
	case ast.TypeArrayDouble:
		return "dex_array_double_sort_desc"
	case ast.TypeArrayChar:
		return "dex_array_char_sort_desc"
	default:
		return ""
	}
}

// typeOfExpr returns the type of an expression based on available information.
func (g *Generator) typeOfExpr(expr ast.Expr) ast.Type {
	switch e := expr.(type) {
	case *ast.CharLit:
		return ast.TypeChar
	case *ast.IntLit:
		return ast.TypeInt
	case *ast.FloatLit:
		return ast.TypeDouble
	case *ast.BoolLit:
		return ast.TypeBool
	case *ast.StringLit:
		return ast.TypeString
	case *ast.Ident:
		if t, ok := g.varTypes[e.Name]; ok {
			return t
		}
		// Check if it's a function reference
		if fn, ok := g.funcs[e.Name]; ok {
			var paramTypes []ast.Type
			for _, p := range fn.Params {
				paramTypes = append(paramTypes, p.Type)
			}
			return ast.FuncTypeOf(paramTypes, fn.ReturnType)
		}
	case *ast.CallExpr:
		// Polymorphic return type: db.col uses ResolvedType
		if e.Module == "db" && e.Name == "col" {
			return e.ResolvedType
		}
		if e.Module != "" {
			funcDef, ok := stdlib.LookupFunc(e.Module, e.Name)
			if ok {
				return funcDef.ReturnType
			}
		}
		if fn, ok := g.funcs[e.Name]; ok {
			return fn.ReturnType
		}
		// Check if calling through a function-typed variable
		if t, ok := g.varTypes[e.Name]; ok && ast.IsFuncType(t) {
			return ast.FuncTypeReturn(t)
		}
	case *ast.BinaryExpr:
		if e.Op == ast.BinAdd && g.isStringExpr(e.Left) {
			return ast.TypeString
		}
		return g.typeOfExpr(e.Left)
	case *ast.UnaryExpr:
		return g.typeOfExpr(e.Operand)
	case *ast.IndexExpr:
		if ident, ok := e.Array.(*ast.Ident); ok {
			if arrType, ok := g.arrVars[ident.Name]; ok {
				return ast.ElementType(arrType)
			}
		}
	case *ast.StructLitExpr:
		if t, ok := ast.LookupStructType(e.Name); ok {
			return t
		}
	case *ast.FieldAccessExpr:
		objType := g.typeOfExpr(e.Object)
		if ast.IsStructType(objType) {
			def := ast.GetStructDef(objType)
			if def != nil {
				for _, f := range def.Fields {
					if f.Name == e.Field {
						return f.Type
					}
				}
			}
		}
	case *ast.SpawnExpr:
		return ast.TaskTypeOf(e.ReturnType)
	case *ast.ChannelExpr:
		return ast.ChanTypeOf(e.ElemType)
	case *ast.ReceiveExpr:
		srcType := g.typeOfExpr(e.Source)
		if ast.IsChanType(srcType) {
			return ast.ChanElemType(srcType)
		}
		if ast.IsTaskType(srcType) {
			return ast.TaskReturnType(srcType)
		}
	}
	return ast.TypeVoid
}

// widerNumericType returns the wider of two numeric types.
// Widening order: int → long → double
func (g *Generator) widerNumericType(a, b ast.Type) ast.Type {
	rank := map[ast.Type]int{
		ast.TypeChar:   0,
		ast.TypeInt:    1,
		ast.TypeLong:   2,
		ast.TypeDouble: 3,
	}
	if rank[a] >= rank[b] {
		return a
	}
	return b
}

func (g *Generator) nonzeroCheckFunc(t ast.Type) string {
	switch t {
	case ast.TypeLong:
		return "dex_check_nonzero_long"
	case ast.TypeDouble:
		return "dex_check_nonzero_double"
	default:
		return "dex_check_nonzero_int"
	}
}

func (g *Generator) cBinOp(op ast.BinOp) string {
	switch op {
	case ast.BinAdd:
		return "+"
	case ast.BinSub:
		return "-"
	case ast.BinMul:
		return "*"
	case ast.BinDiv:
		return "/"
	case ast.BinMod:
		return "%"
	case ast.BinEq:
		return "=="
	case ast.BinNeq:
		return "!="
	case ast.BinStrictEq:
		return "=="
	case ast.BinStrictNeq:
		return "!="
	case ast.BinLt:
		return "<"
	case ast.BinGt:
		return ">"
	case ast.BinLte:
		return "<="
	case ast.BinGte:
		return ">="
	case ast.BinAnd:
		return "&&"
	case ast.BinOr:
		return "||"
	default:
		return "?"
	}
}
