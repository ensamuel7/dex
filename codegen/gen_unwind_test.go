package codegen

import (
	"strings"
	"testing"
)

// A throw is a longjmp, so the frames between it and the catch never run their
// releases. Each heap value a frame would release therefore also goes on a
// cleanup stack, and each try records that stack's height so the catch can
// release what the unwind skipped. These tests pin the emitted shape: the
// symptom of getting it wrong is a leak or a double free at runtime, neither of
// which a behavioural test on a single throw would show.

func TestTryRecordsACleanupMarkAndUnwindsToIt(t *testing.T) {
	out := generate(t, `fn boom(): void { throw Exception("x") }
fn main(): void {
    try {
        boom()
    } catch (e: Exception) {
        let seen = 1
        assert(seen == 1)
    }
}
`)
	mark := lineContaining(t, out, "= _dex_cleanup_top;")
	if mark == "" {
		t.Fatal("a try must record the cleanup height before its setjmp")
	}
	// The unwind belongs on the caught path, before any handler runs.
	assertContains(t, out, "_dex_cleanup_unwind_to(")
}

func TestSingleAssignmentHeapLocalIsRegistered(t *testing.T) {
	// `held` is written once and read once, so the cleanup stack can hold its
	// pointer: an unwind past this frame releases it.
	out := generate(t, `fn boom(): void { throw Exception("x") }
fn middle(): void {
    let held: string = "kept"
    boom()
    assert(held != "")
}
fn main(): void {
    try {
        middle()
    } catch (e: Exception) {
        assert(true)
    }
}
`)
	assertContains(t, out, "_dex_cleanup_push(held)")
}

func TestReassignedHeapLocalKeepsItsEntryCurrent(t *testing.T) {
	// The stack holds the pointer, not the variable, and the second assignment
	// releases the first value — so the entry has to be pointed at the new one
	// or it names freed memory. The slot is what makes that possible.
	out := generate(t, `fn boom(): void { throw Exception("x") }
fn middle(): void {
    let held: string = "first"
    held = "second"
    boom()
    assert(held != "")
}
fn main(): void {
    try {
        middle()
    } catch (e: Exception) {
        assert(true)
    }
}
`)
	assertContains(t, out, "= _dex_cleanup_push(held)")
	assertContains(t, out, "_dex_cleanup_set(")
}

func TestStructLocalRegistersItsHeapFields(t *testing.T) {
	// A struct cannot be registered — releasing one needs its address, which
	// the unwind cannot safely read — but its fields are ordinary pointers and
	// the struct owns them.
	out := generate(t, `struct Record(name: string, n: int) {}
fn boom(): void { throw Exception("x") }
fn middle(): void {
    let r = Record("held", 1)
    boom()
    assert(r.n == 1)
}
fn main(): void {
    try {
        middle()
    } catch (e: Exception) {
        assert(true)
    }
}
`)
	assertContains(t, out, "_dex_cleanup_push(r.name)")
}

func TestWrittenStructFieldKeepsItsEntryCurrent(t *testing.T) {
	// Writing a field releases what it held, so that field's entry has to
	// follow — which is why a struct whose fields are written gets a slot per
	// field rather than a plain push.
	out := generate(t, `struct Record(name: string, n: int) {}
fn boom(): void { throw Exception("x") }
fn middle(): void {
    let r = Record("original", 1)
    r.name = "replaced"
    boom()
    assert(r.n == 1)
}
fn main(): void {
    try {
        middle()
    } catch (e: Exception) {
        assert(true)
    }
}
`)
	assertContains(t, out, "= _dex_cleanup_push(r.name)")
	assertContains(t, out, "_dex_cleanup_set(")
}

func TestStatementTemporaryIsRegistered(t *testing.T) {
	// The string built for the argument belongs to the calling statement and is
	// released after it — which the longjmp skips.
	out := generate(t, `import "str"
fn boom(s: string): void { throw Exception("x") }
fn main(): void {
    try {
        boom("arg " + str.fromInt(1))
    } catch (e: Exception) {
        assert(true)
    }
}
`)
	line := lineContaining(t, out, "_dex_cleanup_push(_dex_tmp")
	if line == "" {
		t.Fatal("a hoisted temporary must be registered before the call that may throw")
	}
}

func TestNoExceptionsMeansNoCleanupCode(t *testing.T) {
	// The whole mechanism exists for the unwind. A program that cannot throw
	// pays nothing for it — not a push, not a mark.
	out := generate(t, `fn main(): void {
    let a: string = "x"
    let b: string = a + "y"
    assert(b != "")
}
`)
	assertNotContains(t, out, "_dex_cleanup_push(")
	assertNotContains(t, out, "_dex_cleanup_drop_to(")
}

func TestReturnFromInsideATryPopsItsFrame(t *testing.T) {
	// A try pushes its frame before the setjmp and pops it after the handlers,
	// so a return from the body skips that pop. The frame stack is a fixed
	// array: skipped pops accumulate until it is walked past its end.
	out := generate(t, `fn guarded(n: int): int {
    try {
        return n * 2
    } catch (e: Exception) {
        return 0
    }
}
fn main(): void {
    assert(guarded(2) == 4)
}
`)
	assertContains(t, out, "_dex_exc_top -= 1;")
}

func TestBreakFromInsideATryPopsItsFrame(t *testing.T) {
	out := generate(t, `fn main(): void {
    for (let i = 0; i < 3; i = i + 1) {
        try {
            break
        } catch (e: Exception) {
            assert(false)
        }
    }
    assert(true)
}
`)
	assertContains(t, out, "_dex_exc_top -= 1;")
}

func TestEnteringATryIsBoundsChecked(t *testing.T) {
	// Running out of frames should name the limit rather than write past the
	// end of a thread-local array.
	out := generate(t, `fn main(): void {
    try {
        assert(true)
    } catch (e: Exception) {
        assert(false)
    }
}
`)
	assertContains(t, out, "_dex_exc_push_frame();")
}

func TestFramesArePoppedAfterTheReturnValueIsEvaluated(t *testing.T) {
	// A return expression can throw, and until it has produced a value the
	// enclosing try is still its handler. Popping first sent the exception to
	// the caller's catch instead — so the value is computed into a temporary,
	// and only then are the frames closed.
	out := generate(t, `fn mightThrow(): int { throw Exception("x") }
fn guarded(): int {
    try {
        return mightThrow()
    } catch (e: Exception) {
        return -1
    }
}
fn main(): void {
    assert(guarded() == -1)
}
`)
	tmp := strings.Index(out, "_ret_tmp = mightThrow()")
	pop := strings.Index(out, "_dex_exc_top -= 1;")
	if tmp < 0 || pop < 0 {
		t.Fatalf("expected both the temporary and the pop:\n%s", out)
	}
	if tmp > pop {
		t.Errorf("the return value must be evaluated before the frames are popped:\n%s", out)
	}
}

// A struct's auto-constructor is the same initialiser as a brace literal
// written another way, so it has the same ownership question: a borrowed value
// stored in it needs a reference, because the struct's scope will release it.
//
// Keying the retain on the literal alone meant `return Result(status, message)`
// kept the caller's string without taking a reference, released it at the call,
// and released it again at scope exit — a dangling read, then a double free
// that hung the program on the next allocation.
func TestConstructorCallRetainsABorrowedField(t *testing.T) {
	out := generate(t, `struct Boxed(label: string, n: int) {}

fn viaConstructor(label: string): Boxed {
    return Boxed(label, 1)
}

fn main(): void {
    let a: Boxed = viaConstructor("one")
    assert(a.label == "one")
}
`)
	line := lineContaining(t, out, "_ret_tmp = (Dex_Boxed){ .label = label")
	if line == "" {
		t.Fatal("the constructor's result must be hoisted so its fields can be retained")
	}
	assertContains(t, out, "dex_retain(_ret_tmp.label)")
}

// The same question one level down: a constructor call nested in a literal.
func TestNestedConstructorCallRetainsToo(t *testing.T) {
	out := generate(t, `struct Inner(label: string) {}
struct Outer(inner: Inner, n: int) {}

fn wrap(label: string): Outer {
    return Outer(Inner(label), 1)
}

fn main(): void {
    let o: Outer = wrap("x")
    assert(o.inner.label == "x")
}
`)
	assertContains(t, out, "dex_retain(_ret_tmp.inner.label)")
}

// A freshly minted value carries the only reference there is, so taking
// another would leak it.
func TestConstructorCallDoesNotRetainAMintedField(t *testing.T) {
	out := generate(t, `struct Boxed(label: string, n: int) {}

fn main(): void {
    let a: Boxed = Boxed("minted", 1)
    assert(a.label == "minted")
}
`)
	assertNotContains(t, out, "dex_retain(a.label)")
}
