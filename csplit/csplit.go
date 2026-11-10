// Package csplit cuts one generated C file into several translation units that
// can be compiled in parallel and linked into the same program. See README.md.
package csplit

import (
	"fmt"
	"strings"
)

type kind int

const (
	kindOther kind = iota // directive, typedef, struct, prototype, extern: same text in every unit
	kindFunc              // function definition
	kindVar               // file-scope variable definition
)

// item is one top-level construct of the source, with the comments and blank
// lines before it attached.
type item struct {
	text string
	kind kind

	// kindFunc
	name   string
	inline bool // declared inline; kept whole in every unit when stateless
	sig    int  // offset of the body's opening brace in text

	// kindFunc or kindVar: offset just past a leading "static" keyword, or -1
	staticEnd int
	// kindVar: read-only array of unspecified size, which another unit could
	// not take sizeof through an extern declaration, so every unit keeps its
	// own copy
	dupConst bool
}

// Split returns the units for src: at most maxUnits of them, each with at
// least minBytes of function bodies to compile, or src alone when that leaves
// fewer than two. The first unit holds every variable definition; the others
// hold only function bodies. It errors on a construct it does not recognise,
// and the caller then compiles src as one unit as before.
func Split(src string, maxUnits, minBytes int) ([]string, error) {
	items, err := scan(src)
	if err != nil {
		return nil, err
	}

	// A static prototype must lose its storage class when its definition does,
	// or the definition inherits internal linkage from it.
	protoStatic := map[string]bool{}
	var movable []int
	total := 0
	for i := range items {
		it := &items[i]
		if it.kind != kindFunc {
			continue
		}
		if it.inline && !hasLocalStatic(it.text[it.sig:]) {
			// Stays a static inline copy in every unit, as it is now, so the
			// hot runtime helpers inline without link-time help.
			continue
		}
		protoStatic[it.name] = true
		movable = append(movable, i)
		total += len(it.text)
	}
	n := maxUnits
	if minBytes > 0 && total/minBytes < n {
		n = total / minBytes
	}
	if n < 2 {
		return []string{src}, nil
	}

	// Contiguous runs of roughly equal size. Source order keeps a module's
	// functions together and keeps the cut points stable when code is added
	// near the end.
	owner := make(map[int]int, len(movable))
	per := total/n + 1
	unit, acc := 0, 0
	for _, i := range movable {
		if acc >= per && unit < n-1 {
			unit++
			acc = 0
		}
		owner[i] = unit
		acc += len(items[i].text)
	}
	units := unit + 1

	out := make([]strings.Builder, units)
	for i := range items {
		it := &items[i]
		for u := 0; u < units; u++ {
			b := &out[u]
			switch it.kind {
			case kindOther:
				b.WriteString(fixProto(it.text, protoStatic))
			case kindVar:
				switch {
				case it.dupConst || it.staticEnd < 0 && u == 0:
					b.WriteString(it.text)
				case u == 0:
					b.WriteString(dropStatic(it.text, it.staticEnd))
				default:
					b.WriteString(externDecl(it.text, it.staticEnd))
				}
			case kindFunc:
				o, moved := owner[i]
				switch {
				case !moved:
					b.WriteString(it.text)
				case o == u:
					b.WriteString(dropInline(dropStatic(it.text, it.staticEnd)))
				default:
					b.WriteString(dropInline(dropStatic(it.text[:it.sig], it.staticEnd)))
					b.WriteString(";\n")
				}
			}
		}
	}
	res := make([]string, units)
	for u := range out {
		res[u] = out[u].String()
	}
	return res, nil
}

// scan cuts src into top-level items. It understands comments, string and
// character literals, preprocessor lines and bracket nesting, which is all the
// structure the generator's output has at file scope.
func scan(src string) ([]item, error) {
	var items []item
	start := 0
	depth := 0     // { }
	paren := 0     // ( ) and [ ]
	bodyAt := -1   // offset of a function body's opening brace
	sawEq := false // '=' at depth 0: an initializer, not a body
	lastSig := byte(0)
	lineStart := true

	emit := func(end int, k kind) {
		items = append(items, item{text: src[start:end], kind: k, sig: bodyAt - start})
		start = end
		bodyAt, sawEq, lastSig = -1, false, 0
	}

	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case c == '\n':
			lineStart = true
			if depth == 0 && paren == 0 && lastSig == ')' && isMacroCall(textAfterComments(src[start:i])) {
				// A macro instantiation with no terminator, such as
				// DEX_MAP_DEFINE(...): what it expands to is not visible
				// here, so every unit gets it as written.
				emit(i+1, kindOther)
			}
			continue
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
			continue
		case c == '#' && lineStart:
			// A directive runs to an unescaped newline.
			if depth == 0 && paren == 0 && strings.TrimSpace(textAfterComments(src[start:i])) != "" {
				// Code before the directive with no terminator, such as a
				// macro invocation: a construct this scanner does not know.
				return nil, fmt.Errorf("csplit: unterminated construct before directive at offset %d", i)
			}
			j := i
			for j < len(src) && !(src[j] == '\n' && src[j-1] != '\\') {
				j++
			}
			if j < len(src) {
				j++
			}
			i = j - 1
			if depth == 0 && paren == 0 {
				emit(j, kindOther)
			}
			lineStart = true
			continue
		}
		lineStart = false
		switch c {
		case '/':
			if i+1 < len(src) && src[i+1] == '/' {
				for i < len(src) && src[i] != '\n' {
					i++
				}
				i--
				continue
			}
			if i+1 < len(src) && src[i+1] == '*' {
				k := strings.Index(src[i+2:], "*/")
				if k < 0 {
					return nil, fmt.Errorf("csplit: unterminated comment")
				}
				i += k + 3
				continue
			}
		case '"', '\'':
			j := i + 1
			for j < len(src) && src[j] != c {
				if src[j] == '\\' {
					j++
				} else if src[j] == '\n' {
					return nil, fmt.Errorf("csplit: newline in literal at offset %d", i)
				}
				j++
			}
			i = j
			lastSig = c
			continue
		case '(', '[':
			paren++
		case ')', ']':
			paren--
		case '{':
			if depth == 0 && paren == 0 && lastSig == ')' && !sawEq {
				bodyAt = i
			}
			depth++
		case '}':
			depth--
			if depth < 0 {
				return nil, fmt.Errorf("csplit: unbalanced brace at offset %d", i)
			}
			if depth == 0 && paren == 0 && bodyAt >= 0 {
				end := i + 1
				if end < len(src) && src[end] == '\n' {
					end++
				}
				emit(end, kindFunc)
				continue
			}
		case '=':
			if depth == 0 && paren == 0 {
				sawEq = true
			}
		case ';':
			if depth == 0 && paren == 0 {
				end := i + 1
				if end < len(src) && src[end] == '\n' {
					end++
				}
				emit(end, kindOther)
				continue
			}
		}
		lastSig = c
	}
	if depth != 0 || paren != 0 {
		return nil, fmt.Errorf("csplit: unbalanced brackets at end of file")
	}
	if strings.TrimSpace(textAfterComments(src[start:])) != "" {
		return nil, fmt.Errorf("csplit: trailing text with no terminator")
	}
	if start < len(src) {
		items = append(items, item{text: src[start:], kind: kindOther})
	}
	for k := range items {
		if err := classify(&items[k]); err != nil {
			return nil, err
		}
	}
	return items, nil
}

// classify sets kind-specific fields from the item's leading keywords.
func classify(it *item) error {
	it.staticEnd = -1
	code := textAfterComments(it.text)
	off := len(it.text) - len(code)
	toks := words(code)
	if len(toks) == 0 || code[0] == '#' {
		it.kind = kindOther
		return nil
	}
	// Leading storage class and function specifiers.
	isStatic, isExtern, isTypedef := false, false, false
	pos := 0
	for _, t := range toks {
		switch t.s {
		case "static":
			isStatic = true
			it.staticEnd = off + t.end
		case "inline", "__inline", "__inline__":
			it.inline = true
		case "extern":
			isExtern = true
		case "typedef":
			isTypedef = true
		case "_Thread_local", "__thread", "const", "volatile":
		default:
			goto done
		}
		pos++
	}
done:
	_ = pos
	if it.kind == kindFunc {
		it.name = funcName(code[:it.sig-off])
		if it.name == "" {
			return fmt.Errorf("csplit: cannot name function %q", firstLine(code))
		}
		if !isStatic {
			it.staticEnd = -1
		}
		return nil
	}
	if isTypedef || isExtern {
		it.kind = kindOther
		return nil
	}
	decl := strings.TrimSuffix(strings.TrimSpace(code), ";")
	// A struct, union or enum definition that declares nothing.
	if open := strings.IndexByte(decl, '{'); open >= 0 && !hasTopLevel(decl, '=') && strings.HasSuffix(decl, "}") {
		lead := strings.TrimSpace(decl[:open])
		if strings.HasPrefix(lead, "struct") || strings.HasPrefix(lead, "union") || strings.HasPrefix(lead, "enum") {
			it.kind = kindOther
			return nil
		}
	}
	if isPrototype(decl) {
		it.kind = kindOther
		return nil
	}
	it.kind = kindVar
	if !isStatic {
		it.staticEnd = -1
	}
	if strings.HasPrefix(decl, "struct") || strings.HasPrefix(decl, "union") || strings.HasPrefix(decl, "enum") {
		if open := strings.IndexByte(decl, '{'); open >= 0 && open < firstTopLevel(decl, '=') {
			return fmt.Errorf("csplit: variable declared with its type inline: %q", firstLine(code))
		}
	}
	before := decl
	if k := firstTopLevel(decl, '='); k >= 0 {
		before = decl[:k]
	}
	if strings.Contains(strings.ReplaceAll(before, " ", ""), "[]") {
		if !readOnly(before) {
			return fmt.Errorf("csplit: writable array of unspecified size: %q", firstLine(code))
		}
		if !isStatic {
			return fmt.Errorf("csplit: external array of unspecified size: %q", firstLine(code))
		}
		it.dupConst = true
	}
	return nil
}

// isPrototype reports whether a declaration (without its ';') declares a
// function rather than an object: the first parenthesis at top level, past any
// attribute, opens a parameter list rather than "(*name)".
func isPrototype(decl string) bool {
	if hasTopLevel(decl, '=') {
		return false
	}
	for i := 0; i < len(decl); i++ {
		if decl[i] != '(' {
			continue
		}
		head := strings.TrimSpace(decl[:i])
		if strings.HasSuffix(head, "__attribute__") || strings.HasSuffix(head, "__attribute") ||
			strings.HasSuffix(head, "_Alignas") || strings.HasSuffix(head, "__typeof__") || strings.HasSuffix(head, "typeof") {
			i = matchParen(decl, i)
			continue
		}
		rest := strings.TrimSpace(decl[i+1:])
		return !strings.HasPrefix(rest, "*") && !strings.HasPrefix(rest, "^")
	}
	return false
}

// isMacroCall reports whether code is exactly NAME(...) with an upper-case
// name, the convention every runtime macro follows.
func isMacroCall(code string) bool {
	code = strings.TrimSpace(code)
	k := 0
	for k < len(code) && (code[k] == '_' || code[k] >= 'A' && code[k] <= 'Z' || code[k] >= '0' && code[k] <= '9') {
		k++
	}
	if k == 0 || k == len(code) || code[k] != '(' {
		return false
	}
	return matchParen(code, k) == len(code)-1
}

func matchParen(s string, i int) int {
	d := 0
	for ; i < len(s); i++ {
		switch s[i] {
		case '"', '\'':
			q := s[i]
			for i++; i < len(s) && s[i] != q; i++ {
				if s[i] == '\\' {
					i++
				}
			}
		case '(':
			d++
		case ')':
			d--
			if d == 0 {
				return i
			}
		}
	}
	return len(s) - 1
}

// readOnly reports whether the object named last in a declarator is const:
// "const T x[]" and "const T* const x[]" are, "const T* x[]" is not.
func readOnly(before string) bool {
	b := before
	if k := strings.IndexByte(b, '['); k >= 0 {
		b = b[:k]
	}
	ws := words(b)
	if len(ws) < 2 {
		return false
	}
	ws = ws[:len(ws)-1] // the name
	if !strings.Contains(b, "*") {
		for _, w := range ws {
			if w.s == "const" {
				return true
			}
		}
		return false
	}
	star := strings.LastIndexByte(b, '*')
	tail := words(b[star+1:])
	return len(tail) >= 2 && tail[len(tail)-2].s == "const"
}

// funcName returns the identifier that names the function a signature
// declares: the one directly before the first parameter list.
func funcName(sig string) string {
	for i := 0; i < len(sig); i++ {
		if sig[i] != '(' {
			continue
		}
		j := i
		for j > 0 && (sig[j-1] == ' ' || sig[j-1] == '\t' || sig[j-1] == '\n') {
			j--
		}
		k := j
		for k > 0 && isIdent(sig[k-1]) {
			k--
		}
		name := sig[k:j]
		switch name {
		case "__attribute__", "__attribute", "__declspec", "_Alignas":
			i = matchParen(sig, i)
			continue
		case "":
			// "(*name(...))(...)": a function returning a function pointer.
			continue
		}
		return name
	}
	return ""
}

type word struct {
	s   string
	end int
}

func words(s string) []word {
	var out []word
	for i := 0; i < len(s); {
		if isIdent(s[i]) {
			j := i
			for j < len(s) && isIdent(s[j]) {
				j++
			}
			out = append(out, word{s[i:j], j})
			i = j
			continue
		}
		if s[i] == '(' || s[i] == '{' || s[i] == '=' || s[i] == ';' {
			// Specifiers only come before these.
			if len(out) == 0 || s[i] != '(' {
				out = append(out, word{string(s[i]), i + 1})
			}
		}
		if s[i] == '*' {
			out = append(out, word{"*", i + 1})
		}
		i++
	}
	return out
}

func isIdent(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// textAfterComments strips leading whitespace and comments.
func textAfterComments(s string) string {
	for {
		t := strings.TrimLeft(s, " \t\r\n\f\v")
		switch {
		case strings.HasPrefix(t, "//"):
			k := strings.IndexByte(t, '\n')
			if k < 0 {
				return ""
			}
			s = t[k+1:]
		case strings.HasPrefix(t, "/*"):
			k := strings.Index(t, "*/")
			if k < 0 {
				return ""
			}
			s = t[k+2:]
		default:
			return t
		}
	}
}

func firstLine(s string) string {
	if k := strings.IndexByte(s, '\n'); k >= 0 {
		s = s[:k]
	}
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

// firstTopLevel finds c outside any brackets and literals, or -1.
func firstTopLevel(s string, c byte) int {
	d := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[', '{':
			d++
		case ')', ']', '}':
			d--
		case '"', '\'':
			q := s[i]
			for i++; i < len(s) && s[i] != q; i++ {
				if s[i] == '\\' {
					i++
				}
			}
		default:
			if s[i] == c && d == 0 {
				return i
			}
		}
	}
	return -1
}

func hasTopLevel(s string, c byte) bool { return firstTopLevel(s, c) >= 0 }

// hasLocalStatic reports whether a function body declares a static local,
// which must exist once, so the function cannot be copied into every unit.
func hasLocalStatic(body string) bool {
	for i := 0; i+6 <= len(body); i++ {
		switch body[i] {
		case '"', '\'':
			q := body[i]
			for i++; i < len(body) && body[i] != q; i++ {
				if body[i] == '\\' {
					i++
				}
			}
			continue
		}
		if strings.HasPrefix(body[i:], "static") && (i == 0 || !isIdent(body[i-1])) &&
			(i+6 == len(body) || !isIdent(body[i+6])) {
			return true
		}
	}
	return false
}

// dropStatic removes the leading "static" that ends at end (-1: none).
func dropStatic(text string, end int) string {
	if end < 0 {
		return text
	}
	start := end - len("static")
	if end < len(text) && text[end] == ' ' {
		end++
	}
	return text[:start] + text[end:]
}

// dropInline removes an inline specifier from a definition's leading
// keywords: a plain C99 inline definition emits no symbol, so a function made
// external must not keep it.
func dropInline(text string) string {
	code := textAfterComments(text)
	off := len(text) - len(code)
	for _, w := range words(code) {
		switch w.s {
		case "inline", "__inline", "__inline__":
			s := off + w.end - len(w.s)
			return text[:s] + text[off+w.end:]
		case "static", "extern", "const", "volatile", "_Thread_local", "__thread", "__attribute__":
			continue
		}
		break
	}
	return text
}

// fixProto drops "static" from a prototype of a function that is now
// external.
func fixProto(text string, external map[string]bool) string {
	code := textAfterComments(text)
	if !strings.HasPrefix(code, "static") || !strings.HasSuffix(strings.TrimSpace(code), ";") {
		return text
	}
	decl := strings.TrimSuffix(strings.TrimSpace(code), ";")
	if !isPrototype(decl) || !external[funcName(decl)] {
		return text
	}
	off := len(text) - len(code)
	return dropInline(text[:off] + code[len("static"):])
}

// externDecl turns a definition into a declaration of the same object:
// "static T x = v, y;" becomes "extern T x, y;".
func externDecl(text string, staticEnd int) string {
	text = dropStatic(text, staticEnd)
	code := textAfterComments(text)
	decl := strings.TrimSuffix(strings.TrimSpace(code), ";")
	var parts []string
	for _, p := range splitTopLevel(decl, ',') {
		if k := firstTopLevel(p, '='); k >= 0 {
			p = p[:k]
		}
		parts = append(parts, strings.TrimSpace(p))
	}
	return "extern " + strings.Join(parts, ", ") + ";\n"
}

func splitTopLevel(s string, sep byte) []string {
	var out []string
	for {
		k := firstTopLevel(s, sep)
		if k < 0 {
			return append(out, s)
		}
		out = append(out, s[:k])
		s = s[k+1:]
	}
}
