package parser

import (
	"strings"
	"testing"

	"github.com/ensamuel7/dex/lexer"
)

// `self` names the receiver inside a method and must not be introducible
// anywhere else: resolve.FlattenStructMethods gives a method's receiver
// parameter that name, so a user-written `self` reads as a receiver where there
// is none, and inside a method it would shadow the real one.
//
// It is reserved at the declaration sites rather than in the lexer, because it
// has to stay an ordinary expression — `self.cols` and `return self` are the
// whole point of it.

func parseErrs(t *testing.T, src string) []error {
	t.Helper()
	tokens, err := lexer.New(src).Tokenize()
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	_, errs := New(tokens).Parse()
	return errs
}

func mustRejectSelf(t *testing.T, src string) {
	t.Helper()
	errs := parseErrs(t, src)
	if len(errs) == 0 {
		t.Fatal("expected 'self' to be rejected, got no error")
	}
	var joined []string
	for _, e := range errs {
		joined = append(joined, e.Error())
	}
	all := strings.Join(joined, "; ")
	if !strings.Contains(all, "'self' is reserved") {
		t.Errorf("error does not say self is reserved: %s", all)
	}
}

func TestSelfRejectedAsDeclarationName(t *testing.T) {
	for name, src := range map[string]string{
		"parameter":     "fn f(self: int): int { return 1 }",
		"variable":      "fn main(): void {\n\tlet self: int = 1\n}",
		"constant":      "fn main(): void {\n\tconst self: int = 1\n}",
		"field":         "struct S {\n\tself: int\n}",
		"function":      "fn self(): int { return 1 }",
		"struct":        "struct self { a: int }",
		"enum":          "enum self { A, B }",
		"enum variant":  "enum E { self, B }",
		"interface":     "interface self { fn a(): int }",
		"method":        "struct S {\n\tfn self(): int { return 1 }\n}",
		"loop variable": "fn main(): void {\n\tlet xs: int[] = [1]\n\tforeach (xs as self) {\n\t}\n}",
		"catch binding": "fn main(): void {\n\ttry {\n\t} catch (self: Exception) {\n\t}\n}",
	} {
		t.Run(name, func(t *testing.T) { mustRejectSelf(t, src) })
	}
}

// The half that must keep working: inside a method self is an ordinary
// expression, so it parses as a receiver, a field base and a return value.
func TestSelfStillParsesInsideAMethod(t *testing.T) {
	src := "struct Q {\n" +
		"\tn: int\n" +
		"\tfn bump(): &Q {\n" +
		"\t\tself.n = self.n + 1\n" +
		"\t\treturn self\n" +
		"\t}\n" +
		"\tfn chained(): &Q {\n" +
		"\t\treturn self.bump().bump()\n" +
		"\t}\n" +
		"}"
	if errs := parseErrs(t, src); len(errs) > 0 {
		t.Fatalf("self inside a method should parse: %v", errs)
	}
}
