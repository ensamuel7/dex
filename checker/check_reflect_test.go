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

// --- reflect setters ------------------------------------------------------

func TestReflectWritesEverySetter(t *testing.T) {
	mustCheck(t, `import "reflect"
	struct User { id: long  tag: string  kw: double  count: int  live: bool }
	fn main(): void {
		let c: User = User { id: 1, tag: "t", kw: 1.0, count: 2, live: true }
		let a: bool = reflect.setLong(c, 0, 9)
		let b: bool = reflect.setString(c, 1, "x")
		let d: bool = reflect.setDouble(c, 2, 2.0)
		let e: bool = reflect.setInt(c, 3, 3)
		let f: bool = reflect.setBool(c, 4, false)
	}`)
}

// A &Struct is the form that means "write what the caller is holding".
func TestReflectSetterThroughReference(t *testing.T) {
	mustCheck(t, `import "reflect"
	struct User { tag: string }
	fn fill(c: &User): void { reflect.setString(c, 0, "x") }
	fn main(): void {
		let c: User = User { tag: "t" }
		fill(c)
	}`)
}

// A by-value struct parameter is a copy that shares the caller's string
// pointers without owning them, so writing one releases a reference the caller
// still holds.
func TestReflectSetterRejectsByValueParam(t *testing.T) {
	mustFail(t, `import "reflect"
	struct User { tag: string }
	fn fill(c: User): void { reflect.setString(c, 0, "x") }
	fn main(): void {
		let c: User = User { tag: "t" }
		fill(c)
	}`, "by-value struct parameter is a copy")
}

// And the same for a field reached through one: the root is what decides.
func TestReflectSetterRejectsFieldOfByValueParam(t *testing.T) {
	mustFail(t, `import "reflect"
	struct Inner { tag: string }
	struct Outer { inner: Inner }
	fn fill(c: Outer): void { reflect.setString(c.inner, 0, "x") }
	fn main(): void {
		let c: Outer = Outer { inner: Inner { tag: "t" } }
		fill(c)
	}`, "by-value struct parameter is a copy")
}

// A field of a *local* struct is fine — the local owns what it holds.
func TestReflectSetterAcceptsFieldOfLocal(t *testing.T) {
	mustCheck(t, `import "reflect"
	struct Inner { tag: string }
	struct Outer { inner: Inner }
	fn main(): void {
		let c: Outer = Outer { inner: Inner { tag: "t" } }
		reflect.setString(c.inner, 0, "x")
	}`)
}

// The language has no way to mutate a struct array element in place, and
// reflection is not a back door into it.
func TestReflectSetterRejectsArrayElement(t *testing.T) {
	mustFail(t, `import "reflect"
	struct User { tag: string }
	fn main(): void {
		let list: User[] = [User { tag: "t" }]
		reflect.setString(list[0], 0, "x")
	}`, "cannot write through an array element")
}

func TestReflectSetterRejectsWrongValueType(t *testing.T) {
	mustFail(t, `import "reflect"
	struct User { tag: string }
	fn main(): void {
		let c: User = User { tag: "t" }
		reflect.setString(c, 0, 5)
	}`, "value must be string, got int")
}

// An int literal widens into a long or double field the way it does at every
// other call in the language.
func TestReflectSetterWidensIntLiteral(t *testing.T) {
	mustCheck(t, `import "reflect"
	struct User { id: long  kw: double }
	fn main(): void {
		let c: User = User { id: 1, kw: 1.0 }
		reflect.setLong(c, 0, 9)
		reflect.setDouble(c, 1, 2)
	}`)
}

func TestReflectSetterRejectsOutOfRangeLiteralIndex(t *testing.T) {
	mustFail(t, `import "reflect"
	struct User { tag: string }
	fn main(): void {
		let c: User = User { tag: "t" }
		reflect.setString(c, 4, "x")
	}`, "out of range")
}

func TestReflectSetterRejectsWrongArity(t *testing.T) {
	mustFail(t, `import "reflect"
	struct User { tag: string }
	fn main(): void {
		let c: User = User { tag: "t" }
		reflect.setString(c, 0)
	}`, "takes exactly 3 argument(s)")
}

// A const is immutable whichever way it is written to.
func TestReflectSetterRejectsConstSubject(t *testing.T) {
	mustFail(t, `import "reflect"
	struct User { tag: string }
	const base: User = User { tag: "fixed" }
	fn main(): void {
		reflect.setString(base, 0, "x")
	}`, "cannot modify const variable 'base'")
}

func TestReflectFieldTagsReturnsStringArray(t *testing.T) {
	mustCheck(t, `import "reflect"
	struct User { tag: string }
	fn main(): void {
		let c: User = User { tag: "t" }
		let tags: string[] = reflect.fieldTags(c, "db")
	}`)
}

func TestReflectFieldTagsRejectsNonStringKey(t *testing.T) {
	mustFail(t, `import "reflect"
	struct User { tag: string }
	fn main(): void {
		let c: User = User { tag: "t" }
		let tags: string[] = reflect.fieldTags(c, 1)
	}`, "key must be a string")
}

// --- db.fill --------------------------------------------------------------

// db.fill writes through its subject, so it is held to the same rules a
// reflect setter is.
func TestDbFillAcceptsLocalAndReference(t *testing.T) {
	mustCheck(t, `import "db"
	struct Person { id: long  name: string }
	fn load(p: &Person, rows: int): void { db.fill(p, rows) }
	fn main(): void {
		let p: Person = Person { id: 0, name: "" }
		db.fill(p, 0)
		load(p, 0)
	}`)
}

func TestDbFillRejectsByValueParam(t *testing.T) {
	mustFail(t, `import "db"
	struct Person { id: long  name: string }
	fn load(p: Person, rows: int): void { db.fill(p, rows) }
	fn main(): void {
		let p: Person = Person { id: 0, name: "" }
		load(p, 0)
	}`, "by-value struct parameter is a copy")
}

func TestDbFillRejectsTemporary(t *testing.T) {
	mustFail(t, `import "db"
	struct Person { id: long  name: string }
	fn make(): Person { return Person { id: 0, name: "" } }
	fn main(): void {
		db.fill(make(), 0)
	}`, "not a temporary")
}

func TestDbFillRejectsNonStruct(t *testing.T) {
	mustFail(t, `import "db"
	fn main(): void {
		let n: int = 1
		db.fill(n, 0)
	}`, "must fill a struct")
}

func TestDbFillRejectsWrongArity(t *testing.T) {
	mustFail(t, `import "db"
	struct Person { id: long }
	fn main(): void {
		let p: Person = Person { id: 0 }
		db.fill(p)
	}`, "takes 2 or 3 arguments")
}

func TestDbFillRejectsNonIntHandle(t *testing.T) {
	mustFail(t, `import "db"
	struct Person { id: long }
	fn main(): void {
		let p: Person = Person { id: 0 }
		db.fill(p, "rows")
	}`, "result handle must be int")
}

// The filler is generated per key, so the key has to be known while compiling.
func TestDbFillRejectsComputedTagKey(t *testing.T) {
	mustFail(t, `import "db"
	struct Row { id: long }
	fn main(): void {
		let r: Row = Row {}
		let k: string = "le" + "an"
		db.fill(r, 0, k)
	}`, "must be a string literal")
}

func TestDbFillRejectsEmptyTagKey(t *testing.T) {
	mustFail(t, `import "db"
	struct Row { id: long }
	fn main(): void {
		let r: Row = Row {}
		db.fill(r, 0, "")
	}`, "cannot be empty")
}

func TestDbFillAcceptsATagKey(t *testing.T) {
	mustCheck(t, `import "db"
	struct Row { id: long  extra: string }
	fn main(): void {
		let r: Row = Row {}
		db.fill(r, 0, "lean")
	}`)
}
