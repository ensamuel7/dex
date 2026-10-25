package codegen

import (
	"strings"
	"testing"
)

// A throw releases, before its longjmp, what the throwing frame owns of the
// thrown struct, because the frame's own cleanup never runs. It used to release
// every field unconditionally, which freed the caller's string whenever the
// struct was built from something borrowed. These pin which shapes release, and
// that a literal retains its borrowed fields first; examples/throwBorrowed_test.dx
// runs the same shapes under the memory checker.

const throwShapes = `struct Oops(message: string, code: int) {}

struct Holder {
    err: string
}

fn fromParamField(h: Holder): void throws Oops {
    throw Oops(h.err, 1)
}

fn fromConcat(h: Holder): void throws Oops {
    throw Oops(h.err + "!", 2)
}

fn fromParamStruct(e: Oops): void throws Oops {
    throw e
}

fn fromLocal(h: Holder): void throws Oops {
    let e: Oops = Oops(h.err, 3)
    throw e
}

fn makeOops(m: string): Oops {
    return Oops(m, 4)
}

fn fromCall(h: Holder): void throws Oops {
    throw makeOops(h.err)
}

fn main(): void {
    let h: Holder = Holder { err: "bad " + "news" }
    try {
        fromParamField(h)
        fromConcat(h)
        fromParamStruct(Oops("x", 0))
        fromLocal(h)
        fromCall(h)
    } catch (e: Oops) {
        assert(e.message != "")
    }
}
`

// throwIn returns the throw statement emitted in the body of the named function.
func throwIn(t *testing.T, out, signature string) string {
	t.Helper()
	start := strings.Index(out, signature+" {")
	if start < 0 {
		t.Fatalf("no definition of %s in:\n%s", signature, out)
	}
	body := out[start:]
	if end := strings.Index(body, "\n}\n"); end >= 0 {
		body = body[:end]
	}
	line := lineContaining(t, body, "_dex_throw_staged(")
	if line == "" {
		t.Fatalf("no throw in %s:\n%s", signature, body)
	}
	return line
}

// A literal built from a borrowed field takes its own reference before the
// release, so the release frees only what the literal made.
func TestThrowOfALiteralRetainsItsBorrowedFields(t *testing.T) {
	line := throwIn(t, generate(t, throwShapes), "void fromParamField(Dex_Holder h)")
	retain := strings.Index(line, ".message);")
	if !strings.Contains(line, "dex_retain(") || !strings.Contains(line, "dex_release(") {
		t.Fatalf("want a retain then a release of the message:\n%s", line)
	}
	if retain < 0 || strings.Index(line, "dex_retain(") > strings.Index(line, "dex_release(") {
		t.Errorf("the retain must come before the release:\n%s", line)
	}
}

// A field the literal minted itself is released and not retained: the throw
// is its only owner.
func TestThrowOfALiteralReleasesWhatItMinted(t *testing.T) {
	line := throwIn(t, generate(t, throwShapes), "void fromConcat(Dex_Holder h)")
	if strings.Contains(line, "dex_retain(") || !strings.Contains(line, "dex_release(") {
		t.Errorf("a minted message is released and not retained:\n%s", line)
	}
}

// A parameter is the caller's. Throwing it releases nothing.
func TestThrowOfABorrowedStructReleasesNothing(t *testing.T) {
	line := throwIn(t, generate(t, throwShapes), "void fromParamStruct(Dex_Oops e)")
	if strings.Contains(line, "dex_release(") || strings.Contains(line, "dex_retain(") {
		t.Errorf("a borrowed struct was released or retained by its throw:\n%s", line)
	}
}

// A local is consumed: the throw's release replaces the scope exit the
// longjmp skips. A call result is minted, so the throw owns it too.
func TestThrowOfAnOwnedStructReleasesIt(t *testing.T) {
	out := generate(t, throwShapes)
	for _, sig := range []string{"void fromLocal(Dex_Holder h)", "void fromCall(Dex_Holder h)"} {
		line := throwIn(t, out, sig)
		if !strings.Contains(line, "dex_release(") || strings.Contains(line, "dex_retain(") {
			t.Errorf("%s: an owned struct is released, with nothing retained:\n%s", sig, line)
		}
	}
}
