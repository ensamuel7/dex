package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ensamuel7/dex/lint"
)

// Hovering a keyword answers from its own table, separate from completion's.
// A keyword added to the lexer and the grammar but not here hovers to nothing,
// which is how `throws` shipped before this test existed.
const keywordHoverSource = `struct ParseError(message: string, line: int) {}

fn load(path: string): int throws ParseError {
    defer cleanup()
    if (path == "") {
        throw ParseError("no path", 0)
    }
    return 1
}

fn cleanup(): void {
}

fn main(): void {
    try {
        let n = load("a")
    } catch (e: ParseError) {
        let m = e.message
    } finally {
        let done = 1
    }
}
`

func keywordHoverServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "k.dx")
	if err := os.WriteFile(path, []byte(keywordHoverSource), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToURI(path)
	return &Server{documents: map[string]string{uri: keywordHoverSource}}, uri
}

// hoverOnKeyword points at the first character of word on the first line that
// contains needle.
func hoverOnKeyword(t *testing.T, s *Server, uri, needle, word string) string {
	t.Helper()
	for i, line := range strings.Split(keywordHoverSource, "\n") {
		if !strings.Contains(line, needle) {
			continue
		}
		c := strings.Index(line, word)
		if c < 0 {
			t.Fatalf("%q not on the line holding %q", word, needle)
		}
		return s.hoverAt(uri, keywordHoverSource, Position{Line: i, Character: c + 1})
	}
	t.Fatalf("no line holds %q", needle)
	return ""
}

func TestHoverThrowsKeyword(t *testing.T) {
	s, uri := keywordHoverServer(t)
	got := hoverOnKeyword(t, s, uri, "fn load", "throws")

	for _, want := range []string{
		"`throws`",
		"may escape this function",
		"throws ParseError, IOError",
		"dex lint",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("hover over throws missing %q:\n%s", want, got)
		}
	}
	// `throws` and `throw` are different keywords and must not share an entry.
	if strings.Contains(got, "Throws an exception. Syntax") {
		t.Errorf("hover over throws shows the throw entry:\n%s", got)
	}
}

func TestHoverThrowKeywordMentionsAnyThrowableStruct(t *testing.T) {
	s, uri := keywordHoverServer(t)
	got := hoverOnKeyword(t, s, uri, "throw ParseError", "throw")

	if !strings.Contains(got, "`message: string`") {
		t.Errorf("hover over throw should say what makes a struct throwable:\n%s", got)
	}
}

func TestHoverCatchKeywordMentionsTypedClauses(t *testing.T) {
	s, uri := keywordHoverServer(t)
	got := hoverOnKeyword(t, s, uri, "} catch (e: ParseError)", "catch")

	for _, want := range []string{"a clause per type", "catch-all"} {
		if !strings.Contains(got, want) {
			t.Errorf("hover over catch missing %q:\n%s", want, got)
		}
	}
}

func TestHoverDeferKeywordSaysWhenItRuns(t *testing.T) {
	s, uri := keywordHoverServer(t)
	got := hoverOnKeyword(t, s, uri, "defer cleanup()", "defer")

	for _, want := range []string{"unwinds past the frame", "per iteration"} {
		if !strings.Contains(got, want) {
			t.Errorf("hover over defer missing %q:\n%s", want, got)
		}
	}
	// It is the function's exit, not the enclosing scope's.
	if strings.Contains(got, "enclosing scope exits") {
		t.Errorf("hover over defer still says enclosing scope:\n%s", got)
	}
}

// `Exception` is registered in the global type registry rather than declared in
// any file, so a hover that only walked the program's own structs found nothing
// to say about the one type every catch clause names. The stdlib modules'
// struct types arrive the same way.
const builtinHoverSource = `fn risky(): void throws {
    throw Exception("x")
}

fn main(): void {
    try {
        risky()
    } catch (err: Exception) {
        let m = err.message
    }
}
`

func builtinHoverServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "b.dx")
	if err := os.WriteFile(path, []byte(builtinHoverSource), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToURI(path)
	return &Server{documents: map[string]string{uri: builtinHoverSource}}, uri
}

func hoverIn(t *testing.T, s *Server, uri, source, needle, word string) string {
	t.Helper()
	for i, line := range strings.Split(source, "\n") {
		if !strings.Contains(line, needle) {
			continue
		}
		c := strings.Index(line, word)
		if c < 0 {
			t.Fatalf("%q not on the line holding %q", word, needle)
		}
		return s.hoverAt(uri, source, Position{Line: i, Character: c + 1})
	}
	t.Fatalf("no line holds %q", needle)
	return ""
}

func TestHoverBuiltinExceptionType(t *testing.T) {
	s, uri := builtinHoverServer(t)

	for _, where := range []struct{ label, needle string }{
		{"a catch clause", "} catch (err: Exception)"},
		{"a throw", "throw Exception"},
	} {
		got := hoverIn(t, s, uri, builtinHoverSource, where.needle, "Exception")
		if got == "" {
			t.Fatalf("hover over Exception in %s shows nothing", where.label)
		}
		for _, want := range []string{"struct Exception", "message: string", "Built-in exception type"} {
			if !strings.Contains(got, want) {
				t.Errorf("hover over Exception in %s missing %q:\n%s", where.label, want, got)
			}
		}
	}
}

func TestHoverFieldOfABuiltinType(t *testing.T) {
	s, uri := builtinHoverServer(t)
	got := hoverIn(t, s, uri, builtinHoverSource, "let m = err.message", "message")
	if !strings.Contains(got, "Exception.message: string") {
		t.Errorf("hover over a built-in type's field:\n%s", got)
	}
}

// A signature is where a caller looks for what escapes, so hovering a call has
// to show the throws clause — it was dropped, which made the declaration
// invisible exactly where it is meant to be read.
const throwsSigSource = `struct ParseError(message: string, line: int) {}
struct IOError(message: string) {}

fn load(path: string): int throws ParseError, IOError {
    throw IOError("x")
}

fn bare(): void throws {
    throw Exception("x")
}

fn plain(): int {
    return 1
}

fn main(): void {
    try {
        let n = load("a")
        bare()
        let p = plain()
    } catch (e: Exception) {
        let m = e.message
    }
}
`

func TestHoverShowsTheThrowsClause(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.dx")
	if err := os.WriteFile(path, []byte(throwsSigSource), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToURI(path)
	s := &Server{documents: map[string]string{uri: throwsSigSource}}

	typed := hoverIn(t, s, uri, throwsSigSource, "let n = load", "load")
	if !strings.Contains(typed, "fn load(path: string): int throws ParseError, IOError") {
		t.Errorf("a typed throws clause belongs in the signature:\n%s", typed)
	}

	bare := hoverIn(t, s, uri, throwsSigSource, "        bare()", "bare")
	if !strings.Contains(bare, "fn bare(): void throws") {
		t.Errorf("a bare throws belongs in the signature:\n%s", bare)
	}

	plain := hoverIn(t, s, uri, throwsSigSource, "let p = plain", "plain")
	if strings.Contains(plain, "throws") {
		t.Errorf("a function that cannot throw must not claim it does:\n%s", plain)
	}
}

// How a finding is graded decides whether an editor squiggles it. Lint is
// opt-in on the command line but unconditional in an editor, so a rule that
// fires on correct code has to stay a hint: `throws` is optional, and every
// `throw` in a codebase that has not adopted it is an uncaught-throw finding.
func TestLintSeverityKeepsOptionalAdviceQuiet(t *testing.T) {
	const warning, hint = 2, 4

	for _, rule := range []string{
		lint.RuleUndeclaredThrow,  // a throws list the body contradicts
		lint.RuleUnneededThrows,   // a throws naming what cannot be thrown
		lint.RuleUnreachableCatch, // a clause that can never run
		lint.RuleUnreachable,      // code nothing reaches
		lint.RuleUnknownAllow,     // a suppression that suppresses nothing
	} {
		if got := lintSeverity(rule); got != warning {
			t.Errorf("%s should be a warning, got severity %d", rule, got)
		}
	}

	for _, rule := range []string{
		lint.RuleUncaughtThrow, // advice about adopting an optional declaration
		lint.RuleUnusedVar,
		lint.RuleUnusedParam,
		lint.RuleUnusedImport,
		lint.RuleUnusedFunc,
		lint.RuleShadow,
	} {
		if got := lintSeverity(rule); got != hint {
			t.Errorf("%s should be a hint, got severity %d", rule, got)
		}
	}
}
