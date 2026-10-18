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

func TestExternalFreedByNeedsOwned(t *testing.T) {
	mustFail(t, `external module sq {
		fn f(): string freed by "sqlite3_free"
	}
	fn main(): void {}`, "needs an 'owned string' return")
}

// A symbol is pasted into the generated C as written, so anything that is not
// an identifier would be code run at build time.
func TestExternalRejectsSymbolsThatAreNotIdentifiers(t *testing.T) {
	mustFail(t, `external module z {
		fn f(): int = "abs(0); exit"
	}
	fn main(): void {}`, "is not a C identifier")
	mustFail(t, `external module z {
		type H closed by "fclose); exit(1"
		fn f(): H? = "g"
	}
	fn main(): void {}`, "is not a C identifier")
	mustFail(t, `external module z {
		fn f(): owned string freed by "free); exit(1" = "g"
	}
	fn main(): void {}`, "is not a C identifier")
}

func TestExternalRejectsPkgNameThatIsAnOption(t *testing.T) {
	mustFail(t, `external module z {
		pkg "--variable=prefix"
		fn f(): int
	}
	fn main(): void {}`, "is not a library name")
}

func TestExternalReportsUnknownPkg(t *testing.T) {
	mustFail(t, `external module z {
		pkg "no-such-library-anywhere"
		fn f(): int
	}
	fn main(): void {}`, "no-such-library-anywhere")
}

const outModule = `external module sq {
		include "<sqlite3.h>"
		type Db closed by "sqlite3_close"
		fn open(path: string, db: &Db?): int = "sqlite3_open"
		fn frexp(x: double, exponent: &int): double
	}
	`

func TestExternalOutParamsPass(t *testing.T) {
	mustCheck(t, outModule+`fn main(): void {
		let db: Db? = null
		let rc: int = sq.open(":memory:", db)
		let e: int = 0
		let m: double = sq.frexp(8.0, e)
		let skipped: int = sq.open(":memory:", null)
	}`)
}

func TestExternalOutArgMustBeAVariable(t *testing.T) {
	mustFail(t, outModule+`fn main(): void {
		let m: double = sq.frexp(8.0, 3)
	}`, "must be a variable of type int")
}

func TestExternalOutArgMustNotBeConst(t *testing.T) {
	mustFail(t, outModule+`fn main(): void {
		const e: int = 0
		let m: double = sq.frexp(8.0, e)
	}`, "is a const")
}

// C writes the representation it was declared with, so a variable of another
// type — even a wider one — would be half written.
func TestExternalOutArgMustMatchExactly(t *testing.T) {
	mustFail(t, outModule+`fn main(): void {
		let e: long = 0
		let m: double = sq.frexp(8.0, e)
	}`, "but 'e' is long")
	mustFail(t, outModule+`fn main(): void {
		let db: Db? = null
		if (db != null) {
			let rc: int = sq.open(":memory:", db)
		}
	}`, "written by the C function as")
}

func TestExternalRejectsStringOutParam(t *testing.T) {
	mustFail(t, `external module z {
		fn f(out: &string): int
	}
	fn main(): void {}`, "cannot be written by C")
}

// Handles are numbered after type parameters, and an unbounded range check made
// every handle a type parameter: a function taking one failed with an internal
// error about instantiation.
func TestExternalHandleAsFunctionParameter(t *testing.T) {
	mustCheck(t, `external module cio {
		include "<stdio.h>"
		type File closed by "fclose"
		fn open(path: string, mode: string): File? = "fopen"
		fn readByte(f: File): int = "fgetc"
	}
	fn first(f: File): int {
		return cio.readByte(f)
	}
	fn reopen(path: string): File? {
		return cio.open(path, "r")
	}
	fn main(): void {
		let f: File? = reopen("/etc/hosts")
		if (f != null) {
			let b: int = first(f)
		}
	}`)
}

// A library wrapping C keeps its connection in a struct its callers can name.
func TestExternalHandleAsStructField(t *testing.T) {
	mustCheck(t, `external module c {
		include "<stdio.h>"
		type File closed by "fclose"
		fn open(path: string, mode: string): File? = "fopen"
	}
	struct Log {
		file: File?
		path: string
	}
	fn main(): void {
		let l: Log = Log { file: c.open("/tmp/x", "w"), path: "/tmp/x" }
	}`)
}
