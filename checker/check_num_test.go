package checker

import "testing"

// Widening a computed value is deliberately not implicit (see
// TestCannotAssignIntVarToLong). num is how it is spelled when it is meant.
func TestNumWidensComputedValues(t *testing.T) {
	mustCheck(t, `import "num"
fn main(): void {
	let a: int = 42
	let b: long = num.toLong(a)
	let c: double = num.toDouble(a)
	let d: int = num.toInt(3.99)
	let e: char = num.toChar(65)
	let f: int = num.toInt('A')
}`)
}

func TestNumAcceptsEveryNumericType(t *testing.T) {
	mustCheck(t, `import "num"
fn main(): void {
	let i: int = 1
	let l: long = 2
	let d: double = 3.0
	let c: char = 'x'
	let a: long = num.toLong(i) + num.toLong(l) + num.toLong(d) + num.toLong(c)
}`)
}

func TestNumRejectsNonNumeric(t *testing.T) {
	mustFail(t, `import "num"
fn main(): void {
	let x: int = num.toInt("hi")
}`, "num.toInt() argument must be int, long, double or char, got string")
	mustFail(t, `import "num"
fn main(): void {
	let x: int = num.toInt(true)
}`, "num.toInt() argument must be int, long, double or char, got bool")
}

// A double has no character code, and rounding one silently would be a
// conversion nobody asked for.
func TestNumToCharRejectsDouble(t *testing.T) {
	mustFail(t, `import "num"
fn main(): void {
	let c: char = num.toChar(3.5)
}`, "num.toChar() argument must be int, long or char, got double")
}

func TestNumArity(t *testing.T) {
	mustFail(t, `import "num"
fn main(): void {
	let x: int = num.toInt(1, 2)
}`, "num.toInt() takes exactly 1 argument, got 2")
}
