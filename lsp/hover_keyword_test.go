package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
