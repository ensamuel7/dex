package checker

import "testing"

// The declarations every case below is written against: two interfaces, a
// struct that satisfies one of them, and a struct that satisfies neither.
const ifacePrelude = `
interface Greeter {
	fn greet(): string
}

interface Counter {
	fn bump(by: int): int
}

struct Person {
	name: string

	fn greet(): string {
		return "Hi " + name
	}
}

struct Empty {
	x: int
}
`

// --- Dispatch ---

func TestInterfaceParamCall(t *testing.T) {
	mustCheck(t, ifacePrelude+`
	fn greetingOf(g: Greeter): string {
		return g.greet()
	}
	fn main(): int {
		let p = Person { name: "Ada" }
		greetingOf(p)
		return 0
	}`)
}

func TestInterfaceLocalCall(t *testing.T) {
	mustCheck(t, ifacePrelude+`
	fn main(): int {
		let p = Person { name: "Ada" }
		let g: Greeter = p
		g.greet()
		return 0
	}`)
}

// An interface value handed straight on is already a vtable pair; the checker
// has to accept it where a struct would also fit.
func TestInterfaceForwarded(t *testing.T) {
	mustCheck(t, ifacePrelude+`
	fn greetingOf(g: Greeter): string {
		return g.greet()
	}
	fn forwarded(g: Greeter): string {
		return greetingOf(g)
	}
	fn main(): int {
		let p = Person { name: "Ada" }
		forwarded(p)
		return 0
	}`)
}

// --- Method resolution ---

// A receiver of interface type names the only methods it can have, so a method
// the struct happens to define is still not callable through the interface.
func TestInterfaceUndeclaredMethod(t *testing.T) {
	mustFail(t, ifacePrelude+`
	fn f(g: Greeter): string {
		return g.shout()
	}
	fn main(): int { return 0 }`,
		"interface Greeter has no method 'shout'")
}

func TestInterfaceMethodArgCount(t *testing.T) {
	mustFail(t, ifacePrelude+`
	fn f(c: Counter): int {
		return c.bump()
	}
	fn main(): int { return 0 }`,
		"takes exactly 1 argument(s), got 0")
}

func TestInterfaceMethodArgType(t *testing.T) {
	mustFail(t, ifacePrelude+`
	fn f(c: Counter): int {
		return c.bump("no")
	}
	fn main(): int { return 0 }`,
		"argument 1 must be int")
}

// --- Satisfaction, and why it failed ---

// The mismatch names what is absent rather than only that the types differ.
func TestInterfaceMissingMethodExplained(t *testing.T) {
	mustFail(t, ifacePrelude+`
	fn show(g: Greeter): void {}
	fn main(): int {
		let e = Empty { x: 1 }
		show(e)
		return 0
	}`,
		"missing fn greet(): string")
}

// A method present under the right name but the wrong signature is reported as
// a mismatch — the case a bare "missing" would mislead on.
func TestInterfaceWrongSignatureExplained(t *testing.T) {
	mustFail(t, `
	interface Greeter {
		fn greet(): string
	}
	struct Mute {
		n: int

		fn greet(): int {
			return n
		}
	}
	fn show(g: Greeter): void {}
	fn main(): int {
		let m = Mute { n: 1 }
		show(m)
		return 0
	}`,
		"greet() has the wrong signature, needs fn greet(): string")
}

// --- Owning positions ---
//
// An interface value owns a refcounted box holding a copy of the struct, so
// every position a borrow could not take is now legal.

func TestInterfaceCanBeReturned(t *testing.T) {
	mustCheck(t, ifacePrelude+`
	fn make(): Greeter {
		let p = Person { name: "Ada" }
		return p
	}
	fn main(): int {
		make()
		return 0
	}`)
}

func TestInterfaceCanBeStructField(t *testing.T) {
	mustCheck(t, ifacePrelude+`
	struct Holder {
		g: Greeter
	}
	fn main(): int {
		let p = Person { name: "Ada" }
		let h = Holder { g: p }
		h.g.greet()
		return 0
	}`)
}

func TestInterfaceCanBeArrayElement(t *testing.T) {
	mustCheck(t, ifacePrelude+`
	fn main(): int {
		let p = Person { name: "Ada" }
		let gs: Greeter[] = [p]
		gs[0].greet()
		return 0
	}`)
}

// The box copies the struct, so a value built in the initialiser no longer has
// to outlive the statement.
func TestInterfaceLocalFromTemporary(t *testing.T) {
	mustCheck(t, ifacePrelude+`
	fn main(): int {
		let g: Greeter = Person { name: "Ada" }
		g.greet()
		return 0
	}`)
}

// An argument is still accepted, and still needs no named struct.
func TestInterfaceArgumentMayBeTemporary(t *testing.T) {
	mustCheck(t, ifacePrelude+`
	fn greetingOf(g: Greeter): string {
		return g.greet()
	}
	fn main(): int {
		greetingOf(Person { name: "Grace" })
		return 0
	}`)
}

// Dispatch through an element rather than a named variable: the receiver is an
// expression, which the interface path has to accept like any other method.
func TestInterfaceMethodOnExpressionReceiver(t *testing.T) {
	mustCheck(t, ifacePrelude+`
	fn make(): Greeter {
		let p = Person { name: "Ada" }
		return p
	}
	fn main(): int {
		make().greet()
		return 0
	}`)
}

// The receiver still names the only methods it can have, expression or not.
func TestInterfaceExpressionReceiverUndeclaredMethod(t *testing.T) {
	mustFail(t, ifacePrelude+`
	fn make(): Greeter {
		let p = Person { name: "Ada" }
		return p
	}
	fn main(): int {
		make().shout()
		return 0
	}`,
		"interface Greeter has no method 'shout'")
}
