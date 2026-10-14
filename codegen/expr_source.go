package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

// exprSource renders an expression back to something close to what was written,
// for a diagnostic that has to name the thing that failed. It is a best effort
// and deliberately not a formatter: an unhandled node renders as "..." rather
// than growing this into a second code generator.
func exprSource(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.IntLit:
		return strconv.Itoa(v.Value)
	case *ast.FloatLit:
		return strconv.FormatFloat(v.Value, 'g', -1, 64)
	case *ast.BoolLit:
		if v.Value {
			return "true"
		}
		return "false"
	case *ast.StringLit:
		return strconv.Quote(v.Value)
	case *ast.CharLit:
		return "'" + string(v.Value) + "'"
	case *ast.NullLit:
		return "null"
	case *ast.BinaryExpr:
		return exprSource(v.Left) + " " + binOpSource(v.Op) + " " + exprSource(v.Right)
	case *ast.UnaryExpr:
		if v.Op == ast.UnaryNot {
			return "!" + exprSource(v.Operand)
		}
		return "-" + exprSource(v.Operand)
	case *ast.FieldAccessExpr:
		return exprSource(v.Object) + "." + v.Field
	case *ast.IndexExpr:
		return exprSource(v.Array) + "[" + exprSource(v.Index) + "]"
	case *ast.CallExpr:
		var b strings.Builder
		if v.Recv != nil {
			b.WriteString(exprSource(v.Recv) + ".")
		} else if v.Module != "" {
			b.WriteString(v.Module + ".")
		}
		b.WriteString(v.Name)
		b.WriteString("(")
		for i, a := range v.Args {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(exprSource(a))
		}
		b.WriteString(")")
		return b.String()
	default:
		return "..."
	}
}

// cEscape makes a string safe to paste inside a C string literal.
func cEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				b.WriteString(fmt.Sprintf("\\%03o", r))
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

func binOpSource(op ast.BinOp) string {
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
		return "==="
	case ast.BinStrictNeq:
		return "!=="
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
	}
	return "?"
}
