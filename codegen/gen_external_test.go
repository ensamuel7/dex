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

// The default for an external string return is to copy: the common C idiom
// hands back a pointer the caller must not free — getenv, strerror — and
// freeing one is heap corruption rather than a leak.
func TestCodegenExternalStringReturnIsCopied(t *testing.T) {
	out := generate(t, `external module cstr {
		include "<string.h>"
		fn errorText(n: int): string = "strerror"
	}
	fn main(): void {
		let e: string = cstr.errorText(2)
	}`)
	assertContains(t, out, "dex_string_copy_cstr(strerror(2))")
	if strings.Contains(out, "dex_string_from_cstr(strerror") {
		t.Error("freed a buffer the callee still owns")
	}
}

// `owned` is the other case: the callee malloc'd it and Dex frees it, which
// is also what every stdlib CName function returning a string does.
func TestCodegenExternalOwnedStringReturnIsTakenOver(t *testing.T) {
	out := generate(t, `external module cstr {
		include "<string.h>"
		fn dup(s: string): owned string = "strdup"
	}
	fn main(): void {
		let d: string = cstr.dup("hi")
	}`)
	assertContains(t, out, "dex_string_from_cstr(strdup(")
}

func TestCodegenExternalStringParamPassesTheBytes(t *testing.T) {
	out := generate(t, `external module cstr {
		include "<string.h>"
		fn length(s: string): long = "strlen"
	}
	fn main(): void {
		let s: string = "hello"
		let n: long = cstr.length(s)
	}`)
	assertContains(t, out, "strlen(s->data)")
}

// A handle is boxed on the way out and unboxed on the way in, so the C
// function sees its own pointer and Dex sees a refcounted object.
func TestCodegenExternalHandleBoxesAndUnboxes(t *testing.T) {
	out := generate(t, `external module cio {
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
	assertContains(t, out, "dex_handle_new(fopen(")
	assertContains(t, out, "dex_handle_close_cio_File")
	assertContains(t, out, "fgetc(dex_handle_ptr(f))")
	// The closing shim takes the box and must not free it; the runtime does.
	assertContains(t, out, "if (h->ptr) fclose(h->ptr);")
}

// A handle is a heap type, so the cleanup stack closes one when an exception
// unwinds past the frame holding it. That is what refcounting buys over
// move-only semantics here.
func TestCodegenExternalHandleRidesTheCleanupStack(t *testing.T) {
	out := generate(t, `external module cio {
		include "<stdio.h>"
		type File closed by "fclose"
		fn open(path: string, mode: string): File? = "fopen"
	}
	struct Boom(message: string) {}
	fn f(): void throws Boom {
		let h: File? = cio.open("x", "r")
		throw Boom("x")
	}
	fn main(): void {
		try { f() } catch (e: Boom) { }
	}`)
	assertContains(t, out, "_dex_cleanup_push(h)")
}

// A module that names no header has its C functions declared from the
// signatures written in Dex, so a library with no header to point at — a Rust
// staticlib exporting C-ABI symbols — needs no hand-written one.
func TestCodegenExternalPrototypesWhenNoHeader(t *testing.T) {
	out := generate(t, `external module ml {
		link "-lmathlib"
		fn add(a: int, b: int): int = "ml_add"
		fn scale(x: double, by: int): double = "ml_scale"
		fn name(): string = "ml_name"
	}
	fn main(): void {
		let n: int = ml.add(1, 2)
	}`)
	assertContains(t, out, "extern int ml_add(int, int);")
	assertContains(t, out, "extern double ml_scale(double, int);")
	assertContains(t, out, "extern const char* ml_name(void);")
}

// Naming a header is the default and keeps the C compiler's cross-check, so
// nothing is declared for it.
func TestCodegenNoPrototypesWhenHeaderNamed(t *testing.T) {
	out := generate(t, `external module cmath {
		include "<math.h>"
		fn hypot(x: double, y: double): double
	}
	fn main(): void {
		let d: double = cmath.hypot(3.0, 4.0)
	}`)
	if strings.Contains(out, "extern double hypot(") {
		t.Error("declared a function the named header already declares")
	}
}

func TestCodegenExternalFreedByCallsTheLibraryDeallocator(t *testing.T) {
	out := generate(t, `external module sq {
		include "<sqlite3.h>"
		fn expanded(n: int): owned string freed by "sqlite3_free" = "sqlite3_expanded_sql"
	}
	fn main(): void {
		let s: string = sq.expanded(1)
	}`)
	assertContains(t, out, "sqlite3_expanded_sql(1)")
	assertContains(t, out, "dex_string_copy_cstr(")
	assertContains(t, out, "sqlite3_free((void*)")
	if strings.Contains(out, "dex_string_from_cstr(sqlite3_expanded_sql") {
		t.Error("freed the library's string with free instead of its own deallocator")
	}
}

func TestCodegenExternalOutParams(t *testing.T) {
	out := generate(t, `external module sq {
		include "<sqlite3.h>"
		type Db closed by "sqlite3_close"
		fn open(path: string, db: &Db?): int = "sqlite3_open"
		fn frexp(x: double, exponent: &int): double
	}
	fn main(): void {
		let db: Db? = null
		let rc: int = sq.open(":memory:", db)
		let e: int = 0
		let m: double = sq.frexp(8.0, e)
		let skipped: int = sq.open(":memory:", null)
	}`)
	// A handle is written into a raw local, read back as characters, and boxed
	// into the variable, releasing what it held.
	assertContains(t, out, `sqlite3_open(":memory:", (void*)&`)
	assertContains(t, out, "memcpy(&_dex_written")
	assertContains(t, out, "db = dex_handle_new(_dex_written, dex_handle_close_sq_Db)")
	assertContains(t, out, "if (_dex_old) { dex_release(_dex_old); }")
	// A scalar is just the variable's address, uncast so the header checks it.
	assertContains(t, out, "frexp(8, &e)")
	// null skips the output.
	assertContains(t, out, `sqlite3_open(":memory:", NULL)`)
}

// With no header, the prototype is written from the Dex signature, so an
// out-parameter needs a pointer type there too.
func TestCodegenExternalOutParamPrototypes(t *testing.T) {
	out := generate(t, `external module z {
		type H closed by "h_close"
		fn open(h: &H?): int = "h_open"
		fn split(x: double, whole: &double): double = "z_modf"
	}
	fn main(): void {
		let h: H? = null
		let rc: int = z.open(h)
	}`)
	assertContains(t, out, "extern int h_open(void**);")
	assertContains(t, out, "extern double z_modf(double, double*);")
}

// A discarded result would otherwise be a statement expression whose value is
// unused, which clang warns about.
func TestCodegenExternalOutParamDiscardedResult(t *testing.T) {
	out := generate(t, `external module sq {
		include "<sqlite3.h>"
		type Db closed by "sqlite3_close"
		fn open(path: string, db: &Db?): int = "sqlite3_open"
	}
	fn main(): void {
		let db: Db? = null
		sq.open(":memory:", db)
	}`)
	assertContains(t, out, "(void)({ void*")
}
