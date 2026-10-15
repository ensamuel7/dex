package checker

import "testing"

func TestPayloadEnumConstructAndMatch(t *testing.T) {
	mustCheck(t, `
enum Expr {
	Lit(long)
	Add(Expr, Expr)
}
fn eval(e: Expr): long {
	switch (e) {
		case Expr.Lit(v): { return v }
		case Expr.Add(a, b): { return eval(a) + eval(b) }
	}
	return 0
}
fn main(): void {
	let x: Expr = Expr.Add(Expr.Lit(1), Expr.Lit(2))
}`)
}

// The point of a closed set of variants is that the compiler knows it.
func TestPayloadEnumMatchMustBeExhaustive(t *testing.T) {
	mustFail(t, `
enum Expr { Lit(long) Add(Expr, Expr) }
fn f(e: Expr): long {
	switch (e) {
		case Expr.Lit(v): { return v }
	}
	return 0
}
fn main(): void {}`, "does not handle Add")
}

// An empty default is still a default.
func TestPayloadEnumDefaultSatisfiesExhaustiveness(t *testing.T) {
	mustCheck(t, `
enum Expr { Lit(long) Add(Expr, Expr) }
fn f(e: Expr): long {
	switch (e) {
		case Expr.Lit(v): { return v }
		default: {}
	}
	return 0
}
fn main(): void {}`)
}

func TestPayloadEnumArityIsChecked(t *testing.T) {
	mustFail(t, `
enum Expr { Lit(long) }
fn main(): void { let x: Expr = Expr.Lit(1, 2) }`, "takes 1 value(s), got 2")
	mustFail(t, `
enum Expr { Lit(long) }
fn f(e: Expr): long {
	switch (e) { case Expr.Lit(a, b): { return 1 } }
	return 0
}
fn main(): void {}`, "carries 1 value(s), but the pattern binds 2")
}

func TestPayloadEnumRejectsWrongArgType(t *testing.T) {
	mustFail(t, `
enum Expr { Lit(long) }
fn main(): void { let x: Expr = Expr.Lit("hi") }`, "must be long, got string")
}

// A variant that carries fields cannot be named without them, and one that
// carries none cannot be called.
func TestPayloadEnumVariantSpelling(t *testing.T) {
	mustFail(t, `
enum Expr { Lit(long) Nil }
fn main(): void { let x: Expr = Expr.Lit }`, "carries 1 value(s); write Expr.Lit(...)")
	mustFail(t, `
enum Expr { Lit(long) Nil }
fn main(): void { let x: Expr = Expr.Nil(3) }`, "carries nothing; write it without parentheses")
}

func TestPayloadEnumDuplicateCase(t *testing.T) {
	mustFail(t, `
enum Expr { Lit(long) Nil }
fn f(e: Expr): long {
	switch (e) {
		case Expr.Lit(v): { return v }
		case Expr.Lit(w): { return w }
		default: {}
	}
	return 0
}
fn main(): void {}`, "already handled by an earlier case")
}

// A plain enum keeps every bit of its old behaviour.
func TestPlainEnumUnaffected(t *testing.T) {
	mustCheck(t, `
enum Color { Red Green Blue }
fn main(): void {
	let c: Color = Color.Red
	let same: bool = c == Color.Green
	switch (c) {
		case Color.Red: {}
		default: {}
	}
}`)
}
