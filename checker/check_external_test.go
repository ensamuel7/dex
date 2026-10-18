package checker

import "testing"

func TestExternalScalarsPass(t *testing.T) {
	mustCheck(t, `external module cmath {
		include "<math.h>"
		fn hypot(x: double, y: double): double
		fn absInt(n: int): int = "abs"
		fn flag(b: bool): bool
		fn code(c: char): char
		fn big(n: long): long
	}
	fn main(): void {
		let d: double = cmath.hypot(3.0, 4.0)
	}`)
}

func TestExternalNeedsNoImport(t *testing.T) {
	mustCheck(t, `external module cmath {
		include "<math.h>"
		fn hypot(x: double, y: double): double
	}
	fn main(): void {
		let d: double = cmath.hypot(1.0, 2.0)
	}`)
}

func TestExternalVoidReturnPasses(t *testing.T) {
	mustCheck(t, `external module c {
		include "<stdlib.h>"
		fn seed(n: int): void = "srand"
	}
	fn main(): void {
		c.seed(1)
	}`)
}

func TestExternalRejectsStringParam(t *testing.T) {
	mustFail(t, `external module z {
		fn f(s: string): int
	}
	fn main(): void {}`, "cannot cross the C boundary yet")
}

func TestExternalRejectsArrayParam(t *testing.T) {
	mustFail(t, `external module z {
		fn f(a: int[]): int
	}
	fn main(): void {}`, "cannot cross the C boundary yet")
}

func TestExternalRejectsStructReturn(t *testing.T) {
	mustFail(t, `struct P { x: int }
	external module z {
		fn f(n: int): P
	}
	fn main(): void {}`, "cannot cross the C boundary yet")
}

func TestExternalRejectsStdlibName(t *testing.T) {
	mustFail(t, `external module math {
		fn f(n: int): int
	}
	fn main(): void {}`, "shadows the stdlib module")
}

func TestExternalRejectsDuplicateModule(t *testing.T) {
	mustFail(t, `external module z {
		fn f(n: int): int
	}
	external module z {
		fn g(n: int): int
	}
	fn main(): void {}`, "is declared twice")
}

func TestExternalRejectsDuplicateFunction(t *testing.T) {
	mustFail(t, `external module z {
		fn f(n: int): int
		fn f(n: long): long
	}
	fn main(): void {}`, "declares 'f' twice")
}

func TestExternalArityIsChecked(t *testing.T) {
	mustFail(t, `external module cmath {
		include "<math.h>"
		fn hypot(x: double, y: double): double
	}
	fn main(): void {
		let d: double = cmath.hypot(1.0)
	}`, "takes exactly 2 argument")
}

func TestExternalArgumentTypeIsChecked(t *testing.T) {
	mustFail(t, `external module cmath {
		include "<math.h>"
		fn hypot(x: double, y: double): double
	}
	fn main(): void {
		let n: int = 1
		let d: double = cmath.hypot(n, 2.0)
	}`, "must be double")
}

func TestExternalUnknownFunctionIsRejected(t *testing.T) {
	mustFail(t, `external module cmath {
		include "<math.h>"
		fn hypot(x: double, y: double): double
	}
	fn main(): void {
		let d: double = cmath.cbrt(8.0)
	}`, "undefined function 'cbrt' in module 'cmath'")
}

// A link flag reaches the C compiler's command line, so an external block that
// could pass anything would be arbitrary code execution at build time once a
// module came from somewhere else.
func TestExternalLinkFlagIsRestricted(t *testing.T) {
	for _, bad := range []string{
		"-Wl,-rpath,/evil",
		"-lz; rm -rf /",
		"--version",
		"-fplugin=/tmp/x.so",
		"-L..",
	} {
		mustFail(t, `external module z {
			link "`+bad+`"
			fn f(n: int): int
		}
		fn main(): void {}`, "is not allowed")
	}
}

func TestExternalLinkFlagAllowsLibraries(t *testing.T) {
	mustCheck(t, `external module z {
		include "<zlib.h>"
		link "-lz"
		link "-L/usr/local/lib"
		fn compressBound(n: long): long
	}
	fn main(): void {
		let n: long = z.compressBound(1)
	}`)
}

func TestExternalIncludeMustBeAHeader(t *testing.T) {
	for _, bad := range []string{"<../../etc/passwd>", "a b", "x;y", "<unclosed"} {
		mustFail(t, `external module z {
			include "`+bad+`"
			fn f(n: int): int
		}
		fn main(): void {}`, "is not a header name")
	}
}

func TestExternalIncludeAcceptsBareAndAngled(t *testing.T) {
	mustCheck(t, `external module z {
		include "<math.h>"
		include "local.h"
		fn f(n: int): int
	}
	fn main(): void {
		let n: int = z.f(1)
	}`)
}
