package codegen

import (
	"fmt"
	"strings"

	"github.com/ensamuel7/dex/ast"
)

// genMatchExpr generates a match expression using a GCC statement expression.
// match(tag) { pat1 => body1, pat2, pat3 => body2, _ => default }
// becomes:
// ({ Type _match_N; Type _tag_N = tag;
//    if (_tag_N == pat1) { _match_N = body1; }
//    else if (_tag_N == pat2 || _tag_N == pat3) { _match_N = body2; }
//    else { _match_N = default; }
//    _match_N; })
func (g *Generator) genMatchExpr(out *strings.Builder, e *ast.MatchExpr) {
	idx := g.matchCounter
	g.matchCounter++

	resultType := g.cType(e.Type)
	tagType := g.cType(g.typeOfExpr(e.Tag))

	out.WriteString(fmt.Sprintf("({ %s _match_%d; %s _tag_%d = ", resultType, idx, tagType, idx))
	g.genExpr(out, e.Tag)
	out.WriteString("; ")

	isFirst := true
	for _, arm := range e.Arms {
		if arm.IsWildcard {
			if !isFirst {
				out.WriteString("else ")
			}
			out.WriteString(fmt.Sprintf("{ _match_%d = ", idx))
			g.genExpr(out, arm.Body)
			out.WriteString("; } ")
		} else {
			if !isFirst {
				out.WriteString("else ")
			}
			out.WriteString("if (")
			for i, pat := range arm.Patterns {
				if i > 0 {
					out.WriteString(" || ")
				}
				// Check if comparing strings
				patType := g.typeOfExpr(pat)
				if patType == ast.TypeString {
					out.WriteString(fmt.Sprintf("strcmp(dex_string_data(_tag_%d), ", idx))
					g.genStringData(out, pat)
					out.WriteString(") == 0")
				} else {
					out.WriteString(fmt.Sprintf("_tag_%d == ", idx))
					g.genExpr(out, pat)
				}
			}
			out.WriteString(fmt.Sprintf(") { _match_%d = ", idx))
			g.genExpr(out, arm.Body)
			out.WriteString("; } ")
		}
		isFirst = false
	}

	out.WriteString(fmt.Sprintf("_match_%d; })", idx))
}
