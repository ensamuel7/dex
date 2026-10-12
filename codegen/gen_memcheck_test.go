package codegen

import (
	"strings"
	"testing"
)

// The program the memory checker was built to reason about: a struct with a
// nested struct field, put into an array literal while the original is still
// alive, plus a plain function used as a value.
const memCheckProgram = `
struct Inner {
    label: string
    n: int
}

struct User {
    email: string
    detail: Inner
}

fn twice(x: int): int { return x * 2 }

fn apply(f: fn(int): int, v: int): int { return f(v) }

fn makeUser(): User {
    return User { email: "fresh@example.com", detail: Inner { label: "made", n: 1 } }
}

fn main(): void {
    let unit: User = User { email: "ada@example.com", detail: Inner { label: "inner", n: 3 } }
    let borrowed: User[] = [unit]
    let owned: User[] = [makeUser()]
    assert(borrowed.len() == 1)
    assert(owned.len() == 1)

    let f: fn(int): int = twice
    assert(f(21) == 42)
    assert(apply(twice, 4) == 8)
}
`

func withMemCheck(g *Generator) { g.EnableMemCheck() }

func TestMemCheckEmitsRuntimeAndSiteMarkers(t *testing.T) {
	out := generateWith(t, memCheckProgram, withMemCheck)
	assertContains(t, out, "#define DEX_MEM_CHECK 1")
	assertContains(t, out, "static void dex_mc_on_alloc(void* ptr, size_t size);")
	assertContains(t, out, "#define DEX_AT(site) (dex_mc_site = (site))")
	// Registered before anything in main can allocate.
	mainIdx := strings.Index(out, "int main(")
	initIdx := strings.Index(out, "dex_mc_init();")
	firstMarker := strings.Index(out[mainIdx:], "DEX_AT(")
	if initIdx < mainIdx || firstMarker < 0 || mainIdx+firstMarker < initIdx {
		t.Fatalf("dex_mc_init() must precede the first statement marker in main")
	}
	// Every statement is attributed to its .dx line.
	if n := strings.Count(out, "DEX_AT(\""); n < 8 {
		t.Errorf("expected a site marker per statement, got %d", n)
	}
}

func TestMemCheckOffByDefault(t *testing.T) {
	out := generate(t, memCheckProgram)
	assertNotContains(t, out, "#define DEX_MEM_CHECK 1")
	assertNotContains(t, out, "DEX_AT(\"")
	assertNotContains(t, out, "dex_mc_init();")
	// The hooks still compile away to nothing.
	assertContains(t, out, "#define dex_mc_on_free(ptr) free(ptr)")
}

// A borrowed struct copied into an array literal: the array's destructor runs
// the element cleanup over every slot, so the array needs its own reference to
// each heap field or `unit` and `[unit]` both release the same strings.
func TestStructArrayLitRetainsBorrowedElement(t *testing.T) {
	out := generate(t, memCheckProgram)
	push := structArrayPushLine(t, out, "borrowed")
	for _, field := range []string{"dex_retain(_el.email)", "dex_retain(_el.detail.label)"} {
		if !strings.Contains(push, field) {
			t.Errorf("borrowed element push missing %s:\n%s", field, push)
		}
	}
}

// A struct the expression just produced already holds the only reference there
// is, and that ownership moves into the array.
func TestStructArrayLitMovesFreshElement(t *testing.T) {
	out := generate(t, memCheckProgram)
	push := structArrayPushLine(t, out, "owned")
	if strings.Contains(push, "dex_retain") {
		t.Errorf("fresh element push should transfer ownership, not retain:\n%s", push)
	}
}

// emitReleaseVar descends into a nested struct field, so the cleanup function a
// struct array uses has to as well — otherwise the array frees the top-level
// strings and leaves the nested ones alive.
func TestStructCleanupRecursesIntoNestedStruct(t *testing.T) {
	out := generate(t, memCheckProgram)
	start := strings.Index(out, "static void dex_cleanup_User(void* ptr) {")
	if start < 0 {
		t.Fatalf("no cleanup function for User:\n%s", out)
	}
	body := out[start : start+strings.Index(out[start:], "\n}")]
	assertContains(t, body, "dex_release(s->email);")
	assertContains(t, body, "dex_release(s->detail.label);")
}

// Naming a plain function mints a closure even though it is spelled as an
// identifier, so binding one must not retain on top of that.
func TestFuncValueIdentIsNotRetained(t *testing.T) {
	out := generate(t, memCheckProgram)
	assertContains(t, out, "DexClosure* f = dex_closure_new((void*)_dex_thunk_0, NULL);")
	assertNotContains(t, out, "dex_retain(f);")
}

// ...and passing one to a function that only borrows it has to hoist it into a
// statement-scoped temporary, or nothing ever releases it.
func TestFuncValueArgumentIsReleased(t *testing.T) {
	out := generate(t, memCheckProgram)
	line := lineContaining(t, out, "== 8)")
	if !strings.Contains(line, "DexClosure* _dex_tmp_") || !strings.Contains(line, "dex_release(_dex_tmp_") {
		t.Errorf("function value passed as a borrowed argument is never released:\n%s", line)
	}
}

// structArrayPushLine returns the generated line that pushes into the named
// struct array.
func structArrayPushLine(t *testing.T, out, arrVar string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "dex_array_struct_push("+arrVar+",") {
			return line
		}
	}
	t.Fatalf("no push into %q:\n%s", arrVar, out)
	return ""
}

func lineContaining(t *testing.T, out, substr string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, substr) {
			return line
		}
	}
	t.Fatalf("no line containing %q:\n%s", substr, out)
	return ""
}

// A returned struct literal whose nested field is itself a literal built from
// borrowed locals. The retain pass used to ask whether the nested value was
// borrowed — a literal never is — and so retained nothing inside it, while the
// release pass descended in and released everything.
func TestReturnedNestedStructLitRetainsItsOwnBorrows(t *testing.T) {
	out := generate(t, `
struct Cols {
    alias: string
    exprs: string[]
    at: map[string, int]
}

struct Sel {
    source: string
    cols: Cols
}

fn newSel(source: string, alias: string): Sel {
    let at: map[string, int] = {}
    let exprs: string[] = []
    return Sel { source: source, cols: Cols { alias: alias, exprs: exprs, at: at } }
}

fn main(): void {
    let q: Sel = newSel("invoices", "i")
    assert(q.cols.alias == "i")
}
`)
	start := strings.Index(out, "Dex_Sel newSel(DexString* source, DexString* alias) {")
	if start < 0 {
		t.Fatalf("no definition of newSel:\n%s", out)
	}
	body := out[start : start+strings.Index(out[start:], "\n}")]
	for _, retain := range []string{
		"dex_retain(_ret_tmp.source);",
		"dex_retain(_ret_tmp.cols.alias);",
		"dex_retain(_ret_tmp.cols.exprs);",
		"dex_retain(_ret_tmp.cols.at);",
	} {
		if !strings.Contains(body, retain) {
			t.Errorf("missing %s — the returned struct carries a reference it never took:\n%s", retain, body)
		}
	}
}

// A string method that only reads its argument has to release an argument that
// was *minted*, and which those are is isNewAlloc's question — not "is it a
// literal". Keying off literals leaked `s.indexOf(" " + x + " ")` and
// `s.startsWith(kind + "/")`: one reference per call, forever.
func TestStringMethodReleasesMintedArgument(t *testing.T) {
	out := generate(t, `import "fmt"
fn main(): void {
    let s: string = "a-b-c"
    let mid: string = "b"
    fmt.println(s.indexOf("-" + mid + "-"))
    fmt.println(s.startsWith(mid + "-"))
    fmt.println(s.contains(s.substring(0, 1)))
    fmt.println(s.endsWith("c"))
    fmt.println(s.indexOf(mid))
}
`)
	for _, want := range []string{"dex_str_indexOf", "dex_str_startsWith", "dex_str_contains", "dex_str_endsWith"} {
		assertContains(t, out, want)
	}
	// Every minted argument gets released; the three above plus the literal.
	if n := strings.Count(out, "dex_release(_dex_tmp_"); n < 4 {
		t.Errorf("expected a release per minted argument, found %d:\n%s", n, out)
	}
	// A plain variable is borrowed and must NOT be released — that would drop a
	// reference the caller still holds.
	line := lineContaining(t, out, "dex_str_indexOf(s, mid)")
	if strings.Contains(line, "dex_release") {
		t.Errorf("a borrowed argument must not be released:\n%s", line)
	}
}

// A message built for a throw is a string nothing can own across the unwind:
// longjmp skips every release the unwound frames would have run. The text is
// staged into the runtime first, the string released, and only then does the
// throw happen — so the order of those three is the whole fix.
func TestThrownMessageIsReleasedBeforeTheLongjmp(t *testing.T) {
	out := generate(t, `import "str"
fn boom(n: int): void { throw Exception("built " + str.fromInt(n)) }
fn main(): void {
    let seen: int = 0
    try {
        boom(1)
    } catch (e: Exception) {
        seen = 1
    }
}
`)
	line := lineContaining(t, out, "_dex_throw_staged(); }")
	stage := strings.Index(line, "_dex_exc_stage(")
	if stage < 0 {
		t.Fatalf("no staged message:\n%s", line)
	}
	// Measured from the staging call: the inner concat releases its own
	// operands earlier on the same line, and those are not what this is about.
	tail := line[stage:]
	release := strings.Index(tail, "dex_release(")
	throw := strings.Index(tail, "_dex_throw_staged()")
	if release < 0 || throw < 0 || release > throw {
		t.Errorf("the staged string must be released after staging and before the throw:\n%s", line)
	}
}

// A literal message is static storage, outlives the unwind on its own, and
// needs neither staging nor releasing.
func TestThrownLiteralNeedsNoStaging(t *testing.T) {
	out := generate(t, `fn main(): void {
    let seen: int = 0
    try {
        throw Exception("plain")
    } catch (e: Exception) {
        seen = 1
    }
}
`)
	assertContains(t, out, `_dex_throw("plain")`)
	// No staged throw anywhere — the runtime defines _dex_exc_stage either way,
	// so what matters is that nothing calls it.
	assertNotContains(t, out, "_dex_throw_staged(); }")
}

// A borrowed message belongs to the caller and must survive the throw.
func TestThrownBorrowedMessageIsNotReleased(t *testing.T) {
	out := generate(t, `fn boom(m: string): void { throw Exception(m) }
fn main(): void {
    let held: string = "kept"
    let seen: int = 0
    try {
        boom(held)
    } catch (e: Exception) {
        seen = 1
    }
}
`)
	line := lineContaining(t, out, "_dex_throw_staged(); }")
	if strings.Contains(line, "dex_release") {
		t.Errorf("a borrowed message must not be released:\n%s", line)
	}
	assertContains(t, out, "_dex_throw_staged()")
}
