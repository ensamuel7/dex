package codegen

import "testing"

// A module function that returns a string is the only source of one in a
// program that writes no literal: fmt.println(str.fromLong(n)) wraps the
// returned char* in a DexString. The string runtime was keyed off literals and
// string-typed declarations, so this five-line program did not compile.
func TestModuleReturnTypeEnablesStringRuntime(t *testing.T) {
	out := generate(t, `
	import "fmt"
	import "str"
	fn main(): void {
		let n: long = 42
		fmt.println(str.fromLong(n))
	}`)
	assertContains(t, out, "} DexString;")
	// str.fromLong builds the DexString itself (RawReturn), so the call is
	// direct rather than wrapped in dex_string_from_cstr. What this test is
	// about is that the string runtime exists at all for a program with no
	// string literal in it.
	assertContains(t, out, "dex_str_fromLong(n)")
	assertNotContains(t, out, "dex_string_from_cstr(dex_str_fromLong(n))")
}
