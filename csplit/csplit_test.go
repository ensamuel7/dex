package csplit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// sample has one of each construct the generated C uses at file scope.
const sample = `#include <stdio.h>
#include <string.h>

typedef struct { int n; } Box;
struct Pair { int a, b; };
enum { RED, GREEN };

#define TWICE(x) ((x) * 2)
#define DEFINE_GETTER(T, NAME) \
static T get_##NAME(T v) { return v; }

DEFINE_GETTER(int, int)

static int counter;
static int hits = 3, misses = 0;
static __thread int depth = 0;
static const char* const names[] = {"zero", "one", "two"};
static const int table[4] = {1, 2, 3, 4};
static void (*hook)(int) = NULL;
int exported = 7;

static int helper(int x);
static inline int square(int x) { return x * x; }
static inline int once(void) {
    static int calls = 0;
    return ++calls;
}

#ifdef NOT_DEFINED
static int never(void) { return 1; }
#else
static int always(void) { return 2; }
#endif

static int helper(int x) {
    counter += x; /* a brace in a comment: { */
    const char* s = "a brace in a string: {";
    return counter + (int)strlen(s) * 0;
}

static void on_hook(int v) { hits += v; }

int user_fn(Box b) {
    return helper(b.n) + square(b.n) + get_int(TWICE(1));
}

int user_other(struct Pair p) {
    return p.a + p.b + once() + always() + table[3] + (int)(sizeof(names) / sizeof(names[0]));
}

int main(void) {
    hook = on_hook;
    hook(5);
    Box b = {4};
    struct Pair p = {1, 2};
    depth++;
    printf("%d %d %d %d %d %d %s\n", user_fn(b), user_other(p), once(), hits, misses, exported, names[depth]);
    return 0;
}
`

func TestSplitCompilesAndRunsTheSame(t *testing.T) {
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("no C compiler")
	}
	dir := t.TempDir()
	run := func(name string, files ...string) string {
		bin := filepath.Join(dir, name)
		args := append([]string{"-O2", "-fno-common", "-o", bin}, files...)
		if out, err := exec.Command("cc", args...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", name, err, out)
		}
		out, err := exec.Command(bin).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", name, err, out)
		}
		return string(out)
	}
	one := filepath.Join(dir, "one.c")
	os.WriteFile(one, []byte(sample), 0644)
	want := run("one", one)

	units, err := Split(sample, 3, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) < 2 {
		t.Fatalf("got %d units, want at least 2", len(units))
	}
	var files []string
	for i, u := range units {
		f := filepath.Join(dir, fmt.Sprintf("u%d.c", i))
		os.WriteFile(f, []byte(u), 0644)
		files = append(files, f)
	}
	if got := run("split", files...); got != want {
		t.Errorf("split program printed %q, one unit printed %q", got, want)
	}
}

func TestSplitKeepsSmallProgramsWhole(t *testing.T) {
	units, err := Split(sample, 8, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 || units[0] != sample {
		t.Fatalf("a program below the size floor should come back unchanged, got %d units", len(units))
	}
}

func TestSplitLinkage(t *testing.T) {
	units, err := Split(sample, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	first, second := units[0], units[1]
	for _, c := range []struct {
		unit, text string
		want       bool
	}{
		// Variables are defined once, without static, and declared elsewhere.
		{first, "\nint counter;", true},
		{second, "extern int counter;", true},
		{second, "extern int hits, misses;", true},
		{second, "extern __thread int depth;", true},
		{second, "static int counter", false},
		// An array of unspecified size stays a private copy, so sizeof works.
		{second, `static const char* const names[] = {"zero", "one", "two"};`, true},
		// A stateless inline helper is copied; one with a static local is not.
		{second, "static inline int square(int x) { return x * x; }", true},
		{second, "static inline int once", false},
		// A macro instantiation is copied as written.
		{second, "DEFINE_GETTER(int, int)", true},
		// The prototype loses static along with the definition.
		{first, "static int helper(int x);", false},
	} {
		if got := strings.Contains(c.unit, c.text); got != c.want {
			t.Errorf("contains %q = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestSplitRefusesUnknownConstructs(t *testing.T) {
	for _, src := range []string{
		"static int a[] = {1, 2};\nint f(void) { return a[0]; }\n",
		"lowercase_macro(x)\n#include <stdio.h>\n",
	} {
		if _, err := Split(src, 2, 1); err == nil {
			t.Errorf("Split accepted %q", src)
		}
	}
}
