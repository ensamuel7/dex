package checker

import "testing"

// --- reflect module -------------------------------------------------------

func TestReflectReadsEveryAccessor(t *testing.T) {
	mustCheck(t, `import "reflect"
	struct User { id: long  tag: string  kw: double  count: int  live: bool }
	fn main(): void {
		let c: User = User { id: 1, tag: "t", kw: 1.0, count: 2, live: true }
		let name: string = reflect.typeName(c)
		let n: int = reflect.fieldCount(c)
		let names: string[] = reflect.fields(c)
		let fname: string = reflect.fieldName(c, 0)
		let kind: string = reflect.fieldKind(c, 0)
		let idx: int = reflect.indexOf(c, "tag")
		let present: bool = reflect.has(c, "tag")
		let id: long = reflect.asLong(c, 0)
		let tag: string = reflect.asString(c, 1)
		let kw: double = reflect.asDouble(c, 2)
		let count: int = reflect.asInt(c, 3)
		let live: bool = reflect.asBool(c, 4)
		let shown: string = reflect.toString(c, 0)
	}`)
}

// A helper taking its subject by reference reads the same as one taking it by
// value, so reflection has to see through &T.
func TestReflectThroughReference(t *testing.T) {
	mustCheck(t, `import "reflect"
	struct User { id: long  tag: string }
	fn describe(c: &User): string {
		return reflect.fieldName(c, 0)
	}
	fn main(): void {
		let c: User = User { id: 1, tag: "t" }
		let s: string = describe(c)
	}`)
}

func TestReflectRejectsNonStruct(t *testing.T) {
	mustFail(t, `import "reflect"
	fn main(): void {
		let n: int = 1
		let c: int = reflect.fieldCount(n)
	}`, "subject must be a struct")
}

// The accessors take the subject by pointer, and a temporary owns heap fields
// nobody would release.
func TestReflectRejectsTemporarySubject(t *testing.T) {
	mustFail(t, `import "reflect"
	struct User { id: long  tag: string }
	fn build(): User { return User { id: 1, tag: "t" } }
	fn main(): void {
		let n: int = reflect.fieldCount(build())
	}`, "not a temporary")
}

// An index written as a literal cannot become valid later, so it is a source
// error rather than a zero value at runtime.
func TestReflectRejectsOutOfRangeLiteralIndex(t *testing.T) {
	mustFail(t, `import "reflect"
	struct User { id: long  tag: string }
	fn main(): void {
		let c: User = User { id: 1, tag: "t" }
		let s: string = reflect.asString(c, 7)
	}`, "out of range")
}

func TestReflectRejectsNameWhereIndexExpected(t *testing.T) {
	mustFail(t, `import "reflect"
	struct User { id: long  tag: string }
	fn main(): void {
		let c: User = User { id: 1, tag: "t" }
		let s: string = reflect.asString(c, "tag")
	}`, "field index must be an int")
}

func TestReflectRejectsWrongArity(t *testing.T) {
	mustFail(t, `import "reflect"
	struct User { id: long  tag: string }
	fn main(): void {
		let c: User = User { id: 1, tag: "t" }
		let n: int = reflect.fieldCount(c, 0)
	}`, "takes exactly 1 argument")
}
