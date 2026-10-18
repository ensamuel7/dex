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

func TestExternalAcceptsStrings(t *testing.T) {
	mustCheck(t, `external module cstr {
		include "<string.h>"
		fn length(s: string): long = "strlen"
		fn errorText(n: int): string = "strerror"
		fn dup(s: string): owned string = "strdup"
	}
	fn main(): void {
		let n: long = cstr.length("hi")
		let e: string = cstr.errorText(2)
		let d: string = cstr.dup("hi")
	}`)
}

func TestExternalOwnedNeedsAStringReturn(t *testing.T) {
	mustFail(t, `external module z {
		fn f(n: int): owned int
	}
	fn main(): void {}`, "needs a string return")
}

// `owned` is soft: only meaningful in an external return position.
func TestOwnedStaysUsableAsAName(t *testing.T) {
	mustCheck(t, `fn owned(): int { return 3 }
	fn main(): void {
		let owned: int = 5
	}`)
}

func TestExternalStillRejectsNonScalarContainers(t *testing.T) {
	mustFail(t, `external module z {
		fn f(a: string[]): int
	}
	fn main(): void {}`, "cannot cross the C boundary yet")
}

func TestExternalHandleTypeAndUse(t *testing.T) {
	mustCheck(t, `external module cio {
		include "<stdio.h>"
		type File closed by "fclose"
		fn open(path: string, mode: string): File? = "fopen"
		fn readByte(f: File): int = "fgetc"
	}
	fn main(): void {
		let f: File? = cio.open("x", "r")
		if (f != null) {
			let b: int = cio.readByte(f)
		}
	}`)
}

func TestExternalHandleRejectsDuplicateType(t *testing.T) {
	mustFail(t, `external module cio {
		type File closed by "fclose"
		type File closed by "fclose"
		fn f(n: int): int
	}
	fn main(): void {}`, "declares type 'File' twice")
}

func TestExternalHandleCollidesWithStruct(t *testing.T) {
	mustFail(t, `struct File { x: int }
	external module cio {
		type File closed by "fclose"
		fn f(n: int): int
	}
	fn main(): void {}`, "collides with a struct")
}
