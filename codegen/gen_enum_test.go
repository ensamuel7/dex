package codegen

import (
	"strings"
	"testing"
)

// A payload enum is a refcounted box, so it needs the refcount runtime even
// when nothing else in the program does.
func TestPayloadEnumEmitsRefcountRuntime(t *testing.T) {
	out := generate(t, `
	enum Shape {
		Circle(double)
		Rect(double, double)
	}
	fn main(): void {
		let s: Shape = Shape.Rect(2.0, 3.0)
	}`)
	assertContains(t, out, "typedef struct DexObjHeader {")
	assertContains(t, out, "dex_obj_alloc")
}

func TestPlainEnumEmitsNoRefcountRuntime(t *testing.T) {
	out := generate(t, `
	enum Color {
		Red
		Green
	}
	fn main(): void {
		let c: Color = Color.Red
	}`)
	if strings.Contains(out, "typedef struct DexObjHeader {") {
		t.Fatal("plain enum pulled in the refcount runtime")
	}
}
