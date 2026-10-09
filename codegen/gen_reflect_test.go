package codegen

import (
	"strings"
	"testing"
)

// Reflection is opt-in: a program that does not use it must be exactly what it
// was before the module existed.
func TestReflectEmitsNothingWhenUnused(t *testing.T) {
	c := generate(t, `import "fmt"
	struct User { id: long  tag: string }
	fn main(): void {
		let unit: User = User { id: 1, tag: "t" }
		fmt.println(unit.tag)
	}`)
	if strings.Contains(c, "_dex_refl") {
		t.Error("emitted reflection metadata for a program that never reflects")
	}
}

// Metadata is per struct, not per program: reflecting one struct must not put
// tables on the others.
func TestReflectEmitsMetadataOnlyForReflectedStructs(t *testing.T) {
	c := generate(t, `import "fmt"
	import "reflect"
	struct Reflected { a: int  b: string }
	struct Untouched { c: int  d: string }
	fn main(): void {
		let r: Reflected = Reflected { a: 1, b: "x" }
		let u: Untouched = Untouched { c: 2, d: "y" }
		fmt.println(reflect.fieldName(r, 0))
		fmt.println(u.d)
	}`)
	if !strings.Contains(c, "_dex_refl_names_Reflected") {
		t.Error("missing metadata for the reflected struct")
	}
	if strings.Contains(c, "_dex_refl_names_Untouched") {
		t.Error("emitted metadata for a struct no reflect call named")
	}
}

// The field count is known while compiling, so it must appear as a literal
// rather than a call — that is what lets a loop over it be unrolled.
func TestReflectFieldCountIsAConstant(t *testing.T) {
	c := generate(t, `import "fmt"
	import "reflect"
	struct User { id: long  tag: string  kw: double }
	fn main(): void {
		let unit: User = User { id: 1, tag: "t", kw: 2.0 }
		fmt.println(reflect.fieldCount(unit))
	}`)
	if !strings.Contains(c, `printf("%d\n", 3)`) {
		t.Errorf("fieldCount did not fold to a literal 3:\n%s", c)
	}
}

// A literal field name folds to its index, so a reflective read of a known
// field costs what a field access costs.
func TestReflectIndexOfFoldsLiteralName(t *testing.T) {
	c := generate(t, `import "fmt"
	import "reflect"
	struct User { id: long  tag: string }
	fn main(): void {
		let unit: User = User { id: 1, tag: "t" }
		fmt.println(reflect.asString(unit, reflect.indexOf(unit, "tag")))
	}`)
	if !strings.Contains(c, "_dex_refl_str_User(&(unit), 1)") {
		t.Errorf("indexOf of a literal name did not fold to the constant 1:\n%s", c)
	}
	// The runtime's definition is always present once reflect is imported; what
	// must be absent is a call to it.
	if strings.Contains(c, "dex_refl_index_of((") {
		t.Error("emitted a runtime name search for a literal name")
	}
}

// A name only known at runtime is the one case that has to search the table.
func TestReflectIndexOfSearchesComputedName(t *testing.T) {
	c := generate(t, `import "fmt"
	import "reflect"
	struct User { id: long  tag: string }
	fn main(): void {
		let unit: User = User { id: 1, tag: "t" }
		let picked: string = "ta" + "g"
		fmt.println(reflect.indexOf(unit, picked))
	}`)
	if !strings.Contains(c, "dex_refl_index_of((") {
		t.Errorf("a computed name should search the name table:\n%s", c)
	}
}

// Only fields of the accessor's own type become cases, so a mismatched read
// falls to the default and returns a zero value instead of reinterpreting the
// bytes of a pointer as an integer.
func TestReflectAccessorsAreTypeSeparated(t *testing.T) {
	c := generate(t, `import "fmt"
	import "reflect"
	struct User { id: long  tag: string }
	fn main(): void {
		let unit: User = User { id: 1, tag: "t" }
		fmt.println(reflect.asLong(unit, 0))
		fmt.println(reflect.asString(unit, 1))
	}`)
	longFn := sliceFunc(c, "_dex_refl_long_User")
	if strings.Contains(longFn, "s->tag") {
		t.Errorf("the long accessor reads a string field:\n%s", longFn)
	}
	strFn := sliceFunc(c, "_dex_refl_str_User")
	if strings.Contains(strFn, "s->id") {
		t.Errorf("the string accessor reads a long field:\n%s", strFn)
	}
	// Reading a string field through reflection mints a reference, because
	// isNewAlloc treats every call as minting and the consumer will release it.
	if !strings.Contains(strFn, "dex_retained(s->tag)") {
		t.Errorf("the string accessor must retain what it hands back:\n%s", strFn)
	}
}

// sliceFunc returns the text of the named generated function.
func sliceFunc(c, name string) string {
	start := strings.Index(c, name+"(const")
	if start < 0 {
		return ""
	}
	end := strings.Index(c[start:], "\n}")
	if end < 0 {
		return c[start:]
	}
	return c[start : start+end]
}

// A metadata-only call answers from the subject's type and never reads its
// value, so without care the subject expression would not be emitted at all and
// a subject that does work would never run.
func TestReflectEvaluatesSubjectOfMetadataOnlyCall(t *testing.T) {
	c := generate(t, `import "fmt"
	import "reflect"
	struct User { id: long  tag: string }
	fn pick(): int { return 0 }
	fn main(): void {
		let people: User[] = [User { id: 1, tag: "a" }]
		fmt.println(reflect.fieldName(people[pick()], 1))
	}`)
	if !strings.Contains(c, "(((void)(") || !strings.Contains(c, "pick()") {
		t.Errorf("the subject expression was dropped instead of evaluated:\n%s", c)
	}
}

// A plain variable cannot do work, so it is not evaluated-and-discarded first.
func TestReflectDoesNotWrapPlainVariableSubject(t *testing.T) {
	c := generate(t, `import "fmt"
	import "reflect"
	struct User { id: long  tag: string }
	fn main(): void {
		let unit: User = User { id: 1, tag: "a" }
		fmt.println(reflect.fieldName(unit, 1))
	}`)
	if strings.Contains(c, "(((void)(unit))") {
		t.Errorf("needlessly evaluated a plain variable subject:\n%s", c)
	}
}
