package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

// genJsonValue emits an expression producing a `DexJsonValue*` with a +1
// reference the caller owns. Literals are built node by node; anything else is
// converted from its static type by genJsonValueFrom.
func (g *Generator) genJsonValue(out *strings.Builder, e ast.Expr) {
	switch lit := e.(type) {
	case *ast.ObjectLitExpr:
		// Built inside a statement expression so the partially-built object has a
		// name to hang the puts off, and so nesting composes.
		out.WriteString("({ DexJsonValue* _jo = dex_jv_object(); ")
		for i, k := range lit.Keys {
			out.WriteString(fmt.Sprintf("dex_jv_put_cstr(_jo, %s, ", cQuote(k)))
			g.genJsonValue(out, lit.Values[i])
			out.WriteString("); ")
		}
		out.WriteString("_jo; })")
		return

	case *ast.ArrayLitExpr:
		out.WriteString("({ DexJsonValue* _ja = dex_jv_array(); ")
		for _, elem := range lit.Elems {
			out.WriteString("dex_jv_push(_ja, ")
			g.genJsonValue(out, elem)
			out.WriteString("); ")
		}
		out.WriteString("_ja; })")
		return

	case *ast.MapLitExpr:
		// `{}` in a json.Value position is the empty object.
		out.WriteString("dex_jv_object()")
		return

	case *ast.NullLit:
		out.WriteString("dex_jv_null()")
		return
	}

	g.genJsonValueFrom(out, e, g.typeOfExpr(e))
}

// genJsonValueFrom converts an already-typed expression into a json.Value. A
// value that is already a json.Value is retained rather than copied, so nesting
// one document inside another shares structure instead of duplicating it.
func (g *Generator) genJsonValueFrom(out *strings.Builder, e ast.Expr, t ast.Type) {
	switch t {
	case ast.TypeJsonValue:
		// An expression that already minted a reference is handed straight on;
		// only a borrowed one needs retaining, or the document would never reach
		// a refcount of zero.
		if g.jsonValueOwned(e) {
			g.genExpr(out, e)
			return
		}
		out.WriteString("({ DexJsonValue* _jv = ")
		g.genExpr(out, e)
		out.WriteString("; dex_retain(_jv); _jv; })")
		return
	case ast.TypeInt, ast.TypeLong:
		out.WriteString("dex_jv_int(")
		g.genExpr(out, e)
		out.WriteString(")")
		return
	case ast.TypeChar:
		out.WriteString("dex_jv_int((long)")
		g.genExpr(out, e)
		out.WriteString(")")
		return
	case ast.TypeDouble:
		out.WriteString("dex_jv_double(")
		g.genExpr(out, e)
		out.WriteString(")")
		return
	case ast.TypeBool:
		out.WriteString("dex_jv_bool(")
		g.genExpr(out, e)
		out.WriteString(")")
		return
	case ast.TypeString:
		// A string the expression just built is handed over rather than retained,
		// so the caller's reference does not outlive its last user.
		if g.isNewAlloc(e) {
			out.WriteString("dex_jv_string_owned(")
		} else {
			out.WriteString("dex_jv_string(")
		}
		g.genExpr(out, e)
		out.WriteString(")")
		return
	}

	// Structs, arrays and maps already have encoders. Routing them through their
	// JSON text and re-parsing keeps one definition of how each shape serializes,
	// rather than a second tree-building path that could disagree with encode.
	if ast.IsStructType(t) || ast.IsArrayType(t) || ast.IsStructArrayType(t) || ast.IsMapType(t) {
		out.WriteString("({ DexString* _jt = ")
		g.genJsonEncodeToString(out, e, t)
		out.WriteString("; DexJsonValue* _jp = dex_jv_parse(_jt->data); dex_release(_jt); _jp; })")
		return
	}

	// The checker rejects everything else, so reaching here means a type slipped
	// through; null keeps the output well-formed rather than emitting bad C.
	out.WriteString("dex_jv_null()")
}

// jsonValueOwned reports whether an expression of type json.Value hands back a
// reference the consumer must release. Indexing and calls mint a new reference;
// a variable or struct field only lends one. Getting this wrong either leaks or
// frees a live document, so it is kept next to the code that acts on it.
func (g *Generator) jsonValueOwned(e ast.Expr) bool {
	switch e.(type) {
	case *ast.Ident, *ast.FieldAccessExpr:
		return false
	case *ast.IndexExpr, *ast.CallExpr, *ast.ObjectLitExpr, *ast.ArrayLitExpr, *ast.MapLitExpr:
		return true
	default:
		// Conservative: assume borrowed, which leaks at worst rather than
		// freeing something still in use.
		return false
	}
}

// jsonValueMethodFns maps each no-argument json.Value method to its runtime
// function. `has` takes an argument and is handled separately.
var jsonValueMethodFns = map[string]string{
	"len":      "dex_jv_len",
	"asInt":    "dex_jv_as_int",
	"asLong":   "dex_jv_as_long",
	"asDouble": "dex_jv_as_double",
	"asBool":   "dex_jv_as_bool",
	"asString": "dex_jv_as_string",
	"isNull":   "dex_jv_is_null",
	"isBool":   "dex_jv_is_bool",
	"isNumber": "dex_jv_is_number",
	"isString": "dex_jv_is_string",
	"isArray":  "dex_jv_is_array",
	"isObject": "dex_jv_is_object",
	"keys":     "dex_jv_keys",
}

// genJsonValueMethodExpr emits a method call whose receiver is an arbitrary
// expression. When the receiver owns its reference — `parsed[0].asInt()` — the
// call is wrapped so the intermediate is released once the result is in hand.
func (g *Generator) genJsonValueMethodExpr(out *strings.Builder, recv ast.Expr, method string, args []ast.Expr, retType ast.Type) bool {
	fn, ok := jsonValueMethodFns[method]
	if !ok && method != "has" {
		return false
	}
	emitCall := func(recvC string) {
		if method == "has" {
			// The runtime borrows the key, so an allocating key expression is
			// released here rather than being left to the caller.
			if g.isNewAlloc(args[0]) {
				key := g.nextTemp()
				res := g.nextTemp()
				out.WriteString(fmt.Sprintf("({ DexString* %s = ", key))
				g.genExpr(out, args[0])
				out.WriteString(fmt.Sprintf("; _Bool %s = dex_jv_has(%s, %s); dex_release(%s); %s; })", res, recvC, key, key, res))
				return
			}
			out.WriteString(fmt.Sprintf("dex_jv_has(%s, ", recvC))
			g.genExpr(out, args[0])
			out.WriteString(")")
			return
		}
		out.WriteString(fmt.Sprintf("%s(%s)", fn, recvC))
	}

	if !g.jsonValueOwned(recv) {
		var rb strings.Builder
		g.genExpr(&rb, recv)
		emitCall(rb.String())
		return true
	}

	tmp := g.nextTemp()
	res := g.nextTemp()
	out.WriteString("({ DexJsonValue* " + tmp + " = ")
	g.genExpr(out, recv)
	out.WriteString("; ")
	out.WriteString(fmt.Sprintf("%s %s = ", g.cType(retType), res))
	emitCall(tmp)
	out.WriteString(fmt.Sprintf("; dex_release(%s); %s; })", tmp, res))
	return true
}

// cQuote renders a Go string as a C string literal.
func cQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b.WriteString("\\\"")
		case '\\':
			b.WriteString("\\\\")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\t':
			b.WriteString("\\t")
		default:
			if c < 0x20 {
				b.WriteString(fmt.Sprintf("\\%03o", c))
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}


// genJsonValueIndex emits v[i] or v["key"]. Both runtime lookups return a new
// reference and borrow their operands, so an operand that is itself a fresh
// reference — the inner step of a chain like v["a"]["b"] — is bound to a
// temporary and released once the lookup is done. Without that, every
// intermediate in a chained path would leak.
func (g *Generator) genJsonValueIndex(out *strings.Builder, e *ast.IndexExpr) {
	fn := "dex_jv_index"
	idxType := g.typeOfExpr(e.Index)
	if idxType == ast.TypeString {
		fn = "dex_jv_get"
	}
	// Only a heap index — a string key — can need releasing; an int index is a
	// plain value even when the expression producing it looks like an allocation.
	keyOwned := ast.IsHeapType(idxType) && g.isNewAlloc(e.Index)
	arrOwned := g.jsonValueOwned(e.Array)

	if !keyOwned && !arrOwned {
		out.WriteString(fn + "(")
		g.genExpr(out, e.Array)
		out.WriteString(", ")
		g.genExpr(out, e.Index)
		out.WriteString(")")
		return
	}

	out.WriteString("({ ")
	arr := "_recv"
	if arrOwned {
		arr = g.nextTemp()
		out.WriteString(fmt.Sprintf("DexJsonValue* %s = ", arr))
		g.genExpr(out, e.Array)
		out.WriteString("; ")
	}
	key := ""
	if keyOwned {
		key = g.nextTemp()
		out.WriteString(fmt.Sprintf("%s %s = ", g.cType(idxType), key))
		g.genExpr(out, e.Index)
		out.WriteString("; ")
	}
	res := g.nextTemp()
	out.WriteString(fmt.Sprintf("DexJsonValue* %s = %s(", res, fn))
	if arrOwned {
		out.WriteString(arr)
	} else {
		g.genExpr(out, e.Array)
	}
	out.WriteString(", ")
	if keyOwned {
		out.WriteString(key)
	} else {
		g.genExpr(out, e.Index)
	}
	out.WriteString("); ")
	if keyOwned {
		out.WriteString(fmt.Sprintf("dex_release(%s); ", key))
	}
	if arrOwned {
		out.WriteString(fmt.Sprintf("dex_release(%s); ", arr))
	}
	out.WriteString(fmt.Sprintf("%s; })", res))
}
