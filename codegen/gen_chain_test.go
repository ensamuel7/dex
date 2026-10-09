package codegen

import (
	"strings"
	"testing"
)

// A call is no longer the end of an expression: its result can take a method.
func TestChainMethodOnCallResult(t *testing.T) {
	c := generate(t, `import "fmt"
	fn main(): void {
		let sb = StringBuilder()
		sb.append("hi")
		fmt.println(sb.toString().len())
	}`)
	if !strings.Contains(c, "dex_str_len(") {
		t.Errorf("chained len() did not reach the string-method runtime:\n%s", c)
	}
	// The receiver is bound once and released: it was minted by toString() and
	// nothing else holds it.
	if !strings.Contains(c, "dex_sb_toString(sb)") {
		t.Errorf("receiver was not evaluated:\n%s", c)
	}
	if !strings.Contains(c, "dex_release(") {
		t.Errorf("minted receiver was never released:\n%s", c)
	}
}

// The receiver must be evaluated once even though some methods mention it twice.
func TestChainEvaluatesReceiverOnce(t *testing.T) {
	c := generate(t, `import "fmt"
	fn main(): void {
		let csv: string = "a,b,c"
		fmt.println(csv.split(",")[1].len())
	}`)
	if n := strings.Count(c, "dex_str_split(csv"); n != 1 {
		t.Errorf("receiver evaluated %d times, want 1:\n%s", n, c)
	}
}

// An indexed container is written out more than once by the bounds check and the
// access; a container the expression minted must still be released exactly once.
func TestChainReleasesIndexedTemporaryArray(t *testing.T) {
	c := generate(t, `import "fmt"
	fn main(): void {
		let csv: string = "a,b,c"
		let one: string = csv.split(",")[1]
		fmt.println(one)
	}`)
	if n := strings.Count(c, "dex_str_split(csv"); n != 1 {
		t.Errorf("indexed array evaluated %d times, want 1:\n%s", n, c)
	}
	if !strings.Contains(c, "dex_release(") {
		t.Errorf("the temporary array was never released:\n%s", c)
	}
}

// A plain variable receiver must emit exactly what it did before — no temporary,
// no release — so chaining costs nothing where it is not used.
func TestChainLeavesPlainReceiverAlone(t *testing.T) {
	c := generate(t, `import "fmt"
	fn main(): void {
		let s: string = "abc"
		fmt.println(s.len())
	}`)
	if !strings.Contains(c, "dex_str_len(s)") {
		t.Errorf("a named receiver should be used directly:\n%s", c)
	}
}

// Searching an array only reads the needle, so a minted argument is released.
func TestArrayContainsReleasesMintedArgument(t *testing.T) {
	c := generate(t, `import "fmt"
	fn main(): void {
		let parts: string[] = ["a", "b"]
		fmt.println(parts.contains("b"))
	}`)
	if !strings.Contains(c, "dex_release(") {
		t.Errorf("contains() leaked its literal argument:\n%s", c)
	}
}
