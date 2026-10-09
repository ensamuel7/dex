package lsp

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ensamuel7/dex/ast"
	"github.com/ensamuel7/dex/lexer"
	"github.com/ensamuel7/dex/parser"
)

var structHeaderRe = regexp.MustCompile(`^\s*struct\s+([A-Za-z_][A-Za-z0-9_]*)`)

// enclosingStructName reports the struct whose body contains the given line, or
// "" when the line is not inside one.
//
// Brace counting over the raw text, in keeping with the rest of this file's
// line-based approach: a brace inside a string literal or a comment can throw
// it off, and the cost of that is a completion list that is wrong rather than a
// compile that is. The alternative — resolving the cursor against the parsed
// AST — needs position spans the parser does not record for bodies.
func enclosingStructName(text string, line int) string {
	lines := strings.Split(text, "\n")
	if line >= len(lines) {
		line = len(lines) - 1
	}
	type frame struct {
		name  string
		depth int
	}
	var stack []frame
	depth := 0
	for i := 0; i <= line && i < len(lines); i++ {
		if m := structHeaderRe.FindStringSubmatch(lines[i]); m != nil {
			stack = append(stack, frame{name: m[1], depth: depth})
		}
		for _, ch := range lines[i] {
			switch ch {
			case '{':
				depth++
			case '}':
				depth--
				for len(stack) > 0 && depth <= stack[len(stack)-1].depth {
					stack = stack[:len(stack)-1]
				}
			}
		}
	}
	if len(stack) == 0 {
		return ""
	}
	return stack[len(stack)-1].name
}

// selfCompletions lists what `self.` can reach: the enclosing struct's fields
// and its other methods. Returns nil when the cursor is not inside a struct, so
// the caller can fall through to its other options.
func selfCompletions(program *ast.Program, text string, line int) []CompletionItem {
	name := enclosingStructName(text, line)
	if name == "" {
		return nil
	}
	var def *ast.StructDef
	if program != nil {
		for i := range program.Structs {
			if program.Structs[i].Name == name {
				def = &program.Structs[i]
				break
			}
		}
	}
	if def == nil {
		// The document may not parse while it is being typed; the registry still
		// holds the shape from the last good parse.
		if t, ok := ast.LookupStructType(name); ok {
			def = ast.GetStructDef(t)
		}
	}
	if def == nil {
		return nil
	}
	var items []CompletionItem
	for i := range def.Fields {
		f := &def.Fields[i]
		items = append(items, CompletionItem{
			Label:         f.Name,
			Kind:          CompletionKindField,
			Detail:        typeName(f.Type),
			Documentation: f.Doc,
		})
	}
	for i := range def.Methods {
		m := &def.Methods[i]
		items = append(items, CompletionItem{
			Label:  m.Name,
			Kind:   CompletionKindMethod,
			Detail: formatFuncSignature(m),
		})
	}
	return items
}

// selfItem offers `self` itself, but only inside a struct body where it means
// something.
func selfItem(text string, line int) []CompletionItem {
	name := enclosingStructName(text, line)
	if name == "" {
		return nil
	}
	return []CompletionItem{{
		Label:         "self",
		Kind:          CompletionKindVariable,
		Detail:        "&" + name,
		Documentation: "The instance this method was called on. Assign through it to change that instance, and return it to continue a chain.",
	}}
}

// selfMemberCompletions parses the document so `self.` can be answered from the
// struct the cursor sits in.
//
// The cursor line is blanked before parsing. A half-written `self.` is a syntax
// error, so parsing the document as it stands yields nothing to read the fields
// off — which is precisely the moment completion is asked for. Removing that one
// line leaves the rest of the struct intact and parseable.
func (s *Server) selfMemberCompletions(text string, line int, uri string) []CompletionItem {
	if enclosingStructName(text, line) == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	if line < len(lines) {
		lines[line] = ""
	}
	sanitized := strings.Join(lines, "\n")

	tokens, err := lexer.New(sanitized).Tokenize()
	if err != nil {
		return nil
	}
	p := parser.New(tokens)
	seedParserModuleTypes(p, tokens, filepath.Dir(uriToPath(uri)))
	program, errs := p.Parse()
	if len(errs) > 0 {
		program = nil
	}
	// enclosingStructName is given the original text: blanking a line cannot move
	// the cursor out of its struct, and the original is what the user sees.
	return selfCompletions(program, text, line)
}
