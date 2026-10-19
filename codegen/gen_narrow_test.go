package codegen

import (
	"strings"
	"testing"
)

// A variable declared string? is a string inside a null-guarded block. strVars
// only holds what was declared string, so every site testing it directly
// missed a narrowed optional: the method call fell through to a user-function
// call and emitted a bare len(...), and the return type was unknown so
// fmt.println chose %d for a DexString*.
func TestCodegenStringMethodOnNarrowedOptional(t *testing.T) {
	out := generate(t, `import "fmt"
	fn f(): string? { return "alice" }
	fn main(): void {
		let n: string? = f()
		if (n != null) {
			fmt.println(n.len())
		}
	}`)
	assertContains(t, out, "dex_str_len(n)")
	if strings.Contains(out, " len(n)") {
		t.Error("emitted a bare len(...) instead of the string method")
	}
}

// And the method's return type has to be known, or the print picks the wrong
// specifier for it.
func TestCodegenNarrowedStringMethodReturnType(t *testing.T) {
	out := generate(t, `import "fmt"
	fn f(): string? { return "alice" }
	fn main(): void {
		let n: string? = f()
		if (n != null) {
			fmt.println(n.toUpper())
			fmt.println(n.contains("a"))
		}
	}`)
	assertContains(t, out, "dex_str_toUpper(n)")
	// A string result prints as %s and a bool as the true/false ternary.
	assertContains(t, out, `"true" : "false"`)
}

func TestCodegenOptionalStructCopiesShareABox(t *testing.T) {
	out := generate(t, `struct Named {
		label: string
		fn size(): int { return self.label.len() }
	}
	struct Holder { named: Named? }
	fn size(n: Named?): int {
		if (n == null) { return 0 }
		return n.size()
	}
	fn main(): void {
		let s: string = "a" + "b"
		let h: Holder = Holder { named: Named { label: s } }
		let r: Named? = h.named
		h.named = Named { label: s }
		assert(size(r) + size(Named { label: s }) == 4)
	}`)
	assertContains(t, out, "dex_optbox_Named(")
	assertContains(t, out, "dex_optbox_retain(")
	assertContains(t, out, "dex_optbox_release(")
	assertContains(t, out, "Named_size(n")
	if strings.Contains(out, "malloc(sizeof(Dex_Named))") {
		t.Error("an optional struct was boxed outside the refcounted allocator")
	}
	if strings.Contains(out, "Named_size(&n") {
		t.Error("a method on a narrowed optional took the address of the pointer")
	}
}
