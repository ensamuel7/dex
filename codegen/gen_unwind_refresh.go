package codegen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/resolve"
)

type writtenLocal struct {
	name   string
	fields map[string]bool
	all    bool
}

func (g *Generator) genCallRefreshingUnwind(out *strings.Builder, e *ast.CallExpr) {
	written := g.structLocalsWrittenByCall(e)
	if len(written) == 0 {
		g.genCallExpr(out, e)
		return
	}
	var clear, refresh strings.Builder
	for _, w := range written {
		sv := g.lookupScopeVar(w.name)
		keys := make([]string, 0, len(sv.fieldSlots))
		for key := range sv.fieldSlots {
			top := key
			if i := strings.IndexByte(key, '.'); i >= 0 {
				top = key[:i]
			}
			if w.all || w.fields[top] {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			held, ok := unwindHeld(w.name+"."+key, fieldPathType(sv.typ, key))
			if !ok {
				continue
			}
			clear.WriteString(fmt.Sprintf("_dex_cleanup_set(%s, 0); ", sv.fieldSlots[key]))
			refresh.WriteString(fmt.Sprintf("_dex_cleanup_set(%s, %s); ", sv.fieldSlots[key], held))
		}
	}
	if refresh.Len() == 0 {
		g.genCallExpr(out, e)
		return
	}
	typ := g.typeOfExpr(e)
	if typ == ast.TypeVoid {
		out.WriteString("({ " + clear.String())
		g.genCallExpr(out, e)
		out.WriteString("; " + refresh.String() + "})")
		return
	}
	tmp := g.nextTemp()
	out.WriteString(fmt.Sprintf("({ %s%s %s = ", clear.String(), g.cType(typ), tmp))
	g.genCallExpr(out, e)
	out.WriteString(fmt.Sprintf("; %s%s; })", refresh.String(), tmp))
}

func (g *Generator) structLocalsWrittenByCall(e *ast.CallExpr) []writtenLocal {
	if !g.usesExceptions {
		return nil
	}
	var written []writtenLocal
	add := func(name string, body []ast.Stmt, param string) {
		sv := g.lookupScopeVar(name)
		if sv == nil || len(sv.fieldSlots) == 0 {
			return
		}
		fields, all := map[string]bool{}, true
		if body != nil {
			fields, all = writesThrough(body, param)
		}
		if !all && len(fields) == 0 {
			return
		}
		for i := range written {
			if written[i].name == name {
				written[i].all = written[i].all || all
				for f := range fields {
					written[i].fields[f] = true
				}
				return
			}
		}
		written = append(written, writtenLocal{name: name, fields: fields, all: all})
	}
	structLocal := func(name string) bool {
		t, ok := g.varTypes[name]
		return ok && ast.IsStructType(t)
	}
	if e.IsMethodCall && e.Recv == nil && e.Module != "" {
		head, rest, dotted := strings.Cut(e.Module, ".")
		if structLocal(head) {
			if dotted && rest != "" {
				add(head, nil, "")
			} else if def := ast.GetStructDef(e.StructType); def != nil {
				for i := range def.Methods {
					if def.Methods[i].Name == e.Name {
						add(head, def.Methods[i].Body, "self")
					}
				}
			}
		}
	}
	if (e.Module == "reflect" && reflectSetterSuffix(e.Name) == "str") || (e.Module == "db" && e.Name == "fill") {
		if len(e.Args) > 0 {
			if head := headIdentName(e.Args[0]); head != "" && structLocal(head) {
				add(head, nil, "")
			}
		}
	}
	if fn, ok := g.funcs[e.Name]; ok && e.Module == "" {
		for i, arg := range e.Args {
			if i >= len(fn.Params) || !ast.IsRefType(fn.Params[i].Type) {
				continue
			}
			head := headIdentName(arg)
			if head == "" || !structLocal(head) {
				continue
			}
			if _, isIdent := arg.(*ast.Ident); isIdent {
				add(head, fn.Body, fn.Params[i].Name)
			} else {
				add(head, nil, "")
			}
		}
	}
	return written
}

func writesThrough(body []ast.Stmt, name string) (map[string]bool, bool) {
	fields := map[string]bool{}
	all := false
	resolve.VisitStmts(body, func(st ast.Stmt) {
		switch s := st.(type) {
		case *ast.FieldAssignStmt:
			if headIdentName(s.Object) != name {
				return
			}
			if field := topField(s.Object, name); field != "" {
				fields[field] = true
				return
			}
			fields[s.Field] = true
		case *ast.CompoundAssignStmt:
			if strings.HasPrefix(s.Name, name+".") {
				rest := strings.TrimPrefix(s.Name, name+".")
				if i := strings.IndexByte(rest, '.'); i >= 0 {
					rest = rest[:i]
				}
				fields[rest] = true
			}
		}
	}, func(ex ast.Expr) {
		call, ok := ex.(*ast.CallExpr)
		if !ok {
			return
		}
		if call.Module == name || strings.HasPrefix(call.Module, name+".") {
			all = true
		}
		for _, arg := range call.Args {
			if headIdentName(arg) == name {
				all = true
			}
		}
	})
	return fields, all
}

func topField(expr ast.Expr, name string) string {
	for {
		fa, ok := expr.(*ast.FieldAccessExpr)
		if !ok {
			return ""
		}
		if id, isIdent := fa.Object.(*ast.Ident); isIdent && id.Name == name {
			return fa.Field
		}
		expr = fa.Object
	}
}

func (g *Generator) structLitNeedsUnwindGuard(e *ast.StructLitExpr, def *ast.StructDef) bool {
	if !g.usesExceptions || def == nil {
		return false
	}
	owned := false
	for i, name := range e.FieldNames {
		if owned && containsCall(e.FieldValues[i]) {
			return true
		}
		if g.structLitFieldOwned(structFieldType(def, name), e.FieldValues[i]) {
			owned = true
		}
	}
	return false
}

func structFieldType(def *ast.StructDef, name string) ast.Type {
	for _, f := range def.Fields {
		if f.Name == name {
			return f.Type
		}
	}
	return 0
}

func containsCall(expr ast.Expr) bool {
	found := false
	resolve.VisitExpr(expr, func(ex ast.Expr) {
		if _, ok := ex.(*ast.CallExpr); ok {
			found = true
		}
	})
	return found
}

func (g *Generator) structLitFieldOwned(fieldType ast.Type, value ast.Expr) bool {
	valueType := g.typeOfExpr(value)
	switch {
	case ast.IsInterfaceType(fieldType):
		return !ast.IsInterfaceType(valueType) || !g.borrowsHeapValue(value)
	case ast.IsStructOptionalType(fieldType):
		return true
	case ast.IsHeapType(valueType), ast.IsHeapOptionalType(valueType):
		return g.isNewAlloc(value)
	case ast.IsStructType(fieldType) && ast.NeedsRelease(fieldType):
		return !g.borrowsHeapValue(value)
	}
	return false
}

func (g *Generator) genGuardedFieldValue(out *strings.Builder, fieldType ast.Type, value ast.Expr) {
	tmp := g.nextTemp()
	out.WriteString(fmt.Sprintf("({ %s %s = ", g.cType(fieldType), tmp))
	switch {
	case ast.IsInterfaceType(fieldType) && !ast.IsInterfaceType(g.typeOfExpr(value)):
		g.genInterfaceValue(out, value, fieldType)
	case ast.IsStructOptionalType(fieldType):
		g.genOptionalStructOwned(out, value, fieldType)
	default:
		g.genExpr(out, value)
	}
	out.WriteString("; ")
	var held []string
	g.collectUnwindHeld(tmp, fieldType, &held, 0)
	for _, h := range held {
		out.WriteString(fmt.Sprintf("_dex_cleanup_push(%s); ", h))
	}
	out.WriteString(fmt.Sprintf("%s; })", tmp))
}
