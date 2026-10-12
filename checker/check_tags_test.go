package checker

import "testing"

func TestFieldTagWellFormed(t *testing.T) {
	mustCheck(t, `struct S {
		a: string `+"`"+`db:"x"`+"`"+`
		b: long `+"`"+`db:"y" json:"z"`+"`"+`
		c: int
	}
	fn main(): void {
		let s: S = S { a: "", b: 0, c: 0 }
	}`)
}

// The compiler checks the shape of a tag, not what any key means.
func TestFieldTagRejectsUnquotedValue(t *testing.T) {
	mustFail(t, `struct S {
		a: string `+"`"+`db:x`+"`"+`
	}
	fn main(): void {
		let s: S = S { a: "" }
	}`, "must have a quoted value")
}

func TestFieldTagRejectsMissingValue(t *testing.T) {
	mustFail(t, `struct S {
		a: string `+"`"+`db`+"`"+`
	}
	fn main(): void {
		let s: S = S { a: "" }
	}`, "missing its")
}

func TestFieldTagRejectsUnclosedQuote(t *testing.T) {
	mustFail(t, `struct S {
		a: string `+"`"+`db:"x`+"`"+`
	}
	fn main(): void {
		let s: S = S { a: "" }
	}`, "unclosed quote")
}

func TestFieldTagRejectsRepeatedKey(t *testing.T) {
	mustFail(t, `struct S {
		a: string `+"`"+`db:"x" db:"y"`+"`"+`
	}
	fn main(): void {
		let s: S = S { a: "" }
	}`, "repeats key")
}

func TestFieldTagRejectsMissingSeparator(t *testing.T) {
	mustFail(t, `struct S {
		a: string `+"`"+`db:"x"json:"y"`+"`"+`
	}
	fn main(): void {
		let s: S = S { a: "" }
	}`, "needs a space between keys")
}

func TestReflectFieldTagChecks(t *testing.T) {
	mustCheck(t, `import "reflect"
	struct S {
		a: string `+"`"+`db:"x"`+"`"+`
	}
	fn main(): void {
		let s: S = S { a: "" }
		let got: string = reflect.fieldTag(s, 0, "db")
	}`)
	mustFail(t, `import "reflect"
	struct S {
		a: string
	}
	fn main(): void {
		let s: S = S { a: "" }
		let got: string = reflect.fieldTag(s, 0, 1)
	}`, "key must be a string")
	mustFail(t, `import "reflect"
	struct S {
		a: string
	}
	fn main(): void {
		let s: S = S { a: "" }
		let got: string = reflect.fieldTag(s, 0)
	}`, "takes exactly 3 argument")
}
