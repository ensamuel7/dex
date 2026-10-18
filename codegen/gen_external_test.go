package codegen

import (
	"strings"
	"testing"

	"github.com/ensamuel7/dex/stdlib"
)

func lookupStdlibForTest(name string) *stdlib.Module { return stdlib.Lookup(name) }

func TestCodegenExternalCallsTheCSymbolDirectly(t *testing.T) {
	out := generate(t, `external module cmath {
		include "<math.h>"
		fn hypot(x: double, y: double): double
		fn absInt(n: int): int = "abs"
	}
	fn main(): void {
		let d: double = cmath.hypot(3.0, 4.0)
		let n: int = cmath.absInt(7)
	}`)
	assertContains(t, out, "hypot(3, 4)")
	// The declared name is not what gets called; the C symbol is.
	assertContains(t, out, "abs(7)")
	if strings.Contains(out, "absInt(") {
		t.Error("emitted the declared name instead of the C symbol")
	}
}

func TestCodegenExternalEmitsItsIncludes(t *testing.T) {
	out := generate(t, `external module z {
		include "<zlib.h>"
		include "local.h"
		fn compressBound(n: long): long
	}
	fn main(): void {
		let n: long = z.compressBound(1)
	}`)
	assertContains(t, out, "#include <zlib.h>")
	// A bare name becomes a quoted include rather than an angled one.
	assertContains(t, out, `#include "local.h"`)
}

// An external module is per-compilation state, so it must not reach the stdlib
// registry: the language server is one process compiling many files, and a
// module registered globally would outlive the file that declared it.
func TestExternalDoesNotEnterTheStdlibRegistry(t *testing.T) {
	generate(t, `external module uniqueExternName {
		fn f(n: int): int
	}
	fn main(): void {
		let n: int = uniqueExternName.f(1)
	}`)
	if m := lookupStdlibForTest("uniqueExternName"); m != nil {
		t.Error("external module leaked into the global stdlib registry")
	}
}
