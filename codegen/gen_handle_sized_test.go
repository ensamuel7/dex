package codegen

import (
	"strings"
	"testing"
)

// `sized by` copies by the length the callee wrote, after the call, and frees
// according to the declaration's own ownership words.
func TestSizedByCopiesByLengthAndFreesAsDeclared(t *testing.T) {
	out := generate(t, `external module b {
    fn plain(n: &long): string sized by n = "c_plain"
    fn owned(n: &int): owned string sized by n = "c_owned"
    fn freed(n: &long): owned string sized by n freed by "c_free" = "c_freed"
    fn unsized(n: &long): string = "c_unsized"
}

fn main(): void {
    let n: long = 0
    let i: int = 0
    let a: string = b.plain(n)
    let c: string = b.owned(i)
    let d: string = b.freed(n)
    let e: string = b.unsized(n)
    assert(a.len() + c.len() + d.len() + e.len() >= 0)
}
`)
	for _, c := range []struct{ call, frees string }{
		{"c_plain(&n)", ""},
		{"c_owned(&i)", "free((void*)"},
		{"c_freed(&n)", "c_free((void*)"},
	} {
		line := lineContaining(t, out, c.call+"; long long")
		if !strings.Contains(line, "dex_string_new(_dex_tmp_") || strings.Contains(line, "dex_string_copy_cstr") {
			t.Errorf("%s is not copied by length:\n%s", c.call, line)
		}
		if !strings.Contains(line, "> 0)") {
			t.Errorf("%s does not guard a NULL or negative length:\n%s", c.call, line)
		}
		if c.frees == "" && strings.Contains(line, "free(") {
			t.Errorf("%s is borrowed and must not be freed:\n%s", c.call, line)
		}
		if c.frees != "" && !strings.Contains(line, c.frees) {
			t.Errorf("%s should call %s:\n%s", c.call, c.frees, line)
		}
	}
	if line := lineContaining(t, out, "c_unsized(&n)"); !strings.Contains(line, "dex_string_copy_cstr") {
		t.Errorf("without sized by, a string is still read to its first NUL:\n%s", line)
	}
}
