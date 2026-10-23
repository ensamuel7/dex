package codegen

import (
	"strings"
	"testing"
)

// A closure reaches a module-level variable where it lives. Capturing a copy
// of a mutex made `defer mu.unlock()` release a lock nobody held.
func TestCodegenDeferDoesNotCaptureGlobalMutex(t *testing.T) {
	out := generate(t, `let mu: mutex = mutex
let n: int = 0
fn bump(): int {
    mu.lock()
    defer mu.unlock()
    n = n + 1
    return n
}
fn main(): void {
    bump()
}`)
	assertNotContains(t, out, "->mu = mu")
	assertContains(t, out, "pthread_mutex_unlock(&mu)")
}

// A local of the same name shadows the global and is captured as before.
func TestCodegenLocalShadowingGlobalIsCaptured(t *testing.T) {
	out := generate(t, `let total: int = 0
fn shadowed(): int {
    let total: int = 7
    let read: fn(): int = fn(): int { return total }
    return read()
}
fn main(): void {
    shadowed()
}`)
	assertContains(t, out, "->total = total")
}

// A lambda capturing nothing is still a closure value, so it can be stored in
// a fn variable.
func TestCodegenCaptureFreeLambdaIsClosure(t *testing.T) {
	out := generate(t, `fn main(): void {
    let f: fn(int): int = fn(x: int): int { return x * 2 }
    f(2)
}`)
	assertNotContains(t, out, "DexClosure* f = _dex_lambda_0;")
	assertContains(t, out, "dex_closure_new")
}

// A lambda's return releases its own locals, never the enclosing function's,
// and falling off the end releases them too.
func TestCodegenLambdaScopeIsItsOwn(t *testing.T) {
	out := generate(t, `fn main(): void {
    let outer: string = "o" + "x"
    let f: fn(int): void = fn(n: int): void {
        let inner: string = "i${n}"
    }
    f(1)
}`)
	start := strings.Index(out, "static void _dex_lambda_0(int n) {")
	if start < 0 {
		t.Fatal("expected the lambda's function in the output")
	}
	end := strings.Index(out[start:], "\n}\n")
	body := out[start : start+end]
	if strings.Contains(body, "dex_release(outer)") {
		t.Error("the lambda released the enclosing function's local")
	}
	if !strings.Contains(body, "dex_release(inner)") {
		t.Error("the lambda did not release its own local at the end of its body")
	}
}
