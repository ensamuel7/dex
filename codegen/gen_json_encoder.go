package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

func (g *Generator) jsonStructEncodable(t ast.Type, visiting map[ast.Type]bool) bool {
	if visiting[t] {
		return true
	}
	def := ast.GetStructDef(t)
	if def == nil {
		return false
	}
	visiting[t] = true
	defer delete(visiting, t)
	for _, f := range def.Fields {
		switch {
		case f.Type == ast.TypeInt, f.Type == ast.TypeLong, f.Type == ast.TypeBool,
			f.Type == ast.TypeDouble, f.Type == ast.TypeString:
		case ast.IsStructType(f.Type):
			if !g.jsonStructEncodable(f.Type, visiting) {
				return false
			}
		case isJSONStructArray(f.Type):
			if !g.jsonStructEncodable(ast.ElementType(f.Type), visiting) {
				return false
			}
		case ast.IsArrayType(f.Type):
		default:
			return false
		}
	}
	return true
}

func (g *Generator) jsonEncoderFor(t ast.Type) (string, bool) {
	if g.jsonEncoders == nil {
		g.jsonEncoders = map[ast.Type]string{}
	}
	if name, ok := g.jsonEncoders[t]; ok {
		return name, name != ""
	}
	if !g.jsonStructEncodable(t, map[ast.Type]bool{}) {
		g.jsonEncoders[t] = ""
		return "", false
	}
	cType := g.cType(t)
	name := "dex_jenc_" + cType
	g.jsonEncoders[t] = name
	g.jsonEncProtos.WriteString(fmt.Sprintf("static void %s(DexJB* b, const %s* v);\n", name, cType))

	def := ast.GetStructDef(t)
	var body strings.Builder
	body.WriteString(fmt.Sprintf("static void %s(DexJB* b, const %s* v) {\n", name, cType))
	first := true
	for _, f := range def.Fields {
		isPrimArray := false
		if _, _, ok := jsonArrayCodecNames(f.Type); ok {
			isPrimArray = true
		}
		if ast.IsArrayType(f.Type) && !isJSONStructArray(f.Type) && !isPrimArray {
			continue
		}
		sep := ","
		if first {
			sep = "{"
		}
		first = false
		key := sep + `"` + f.Name + `":`
		body.WriteString(fmt.Sprintf("    dex_jb_raw(b, %s, %d);\n", strconv.Quote(key), len(key)))
		field := "v->" + f.Name
		switch {
		case f.Type == ast.TypeInt, f.Type == ast.TypeLong:
			body.WriteString(fmt.Sprintf("    dex_jb_long(b, (long)%s);\n", field))
		case f.Type == ast.TypeBool:
			body.WriteString(fmt.Sprintf("    dex_jb_bool(b, %s);\n", field))
		case f.Type == ast.TypeDouble:
			body.WriteString(fmt.Sprintf("    dex_jb_double(b, %s);\n", field))
		case f.Type == ast.TypeString:
			body.WriteString(fmt.Sprintf("    dex_jb_str(b, %s);\n", field))
		case ast.IsStructType(f.Type):
			sub, _ := g.jsonEncoderFor(f.Type)
			body.WriteString(fmt.Sprintf("    %s(b, &%s);\n", sub, field))
		case isJSONStructArray(f.Type):
			elem := ast.ElementType(f.Type)
			sub, _ := g.jsonEncoderFor(elem)
			body.WriteString(fmt.Sprintf("    { DexArrayStruct* _a = %s; dex_jb_raw(b, \"[\", 1);\n", field))
			body.WriteString(fmt.Sprintf("      if (_a) for (int _i = 0; _i < _a->len; _i++) { if (_i) dex_jb_raw(b, \",\", 1); %s(b, (const %s*)((char*)_a->data + (size_t)_i * _a->elem_size)); }\n", sub, g.cType(elem)))
			body.WriteString("      dex_jb_raw(b, \"]\", 1); }\n")
		default:
			enc, _, _ := jsonArrayCodecNames(f.Type)
			body.WriteString(fmt.Sprintf("    dex_jb_codec(b, %s((void*)&%s));\n", enc, field))
		}
	}
	if first {
		body.WriteString("    dex_jb_raw(b, \"{}\", 2);\n")
	} else {
		body.WriteString("    dex_jb_raw(b, \"}\", 1);\n")
	}
	body.WriteString("}\n")
	g.jsonEncBodies.WriteString(body.String())
	return name, true
}
