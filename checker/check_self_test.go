package checker

import "testing"

// Outside a method `self` is not a misspelling of anything, so the diagnostic
// says what it is rather than "undefined variable".
func TestSelfOutsideAMethodExplainsItself(t *testing.T) {
	mustFail(t, `let total: int = 0
	fn bump(): int {
		return self.total
	}
	fn main(): void {}`, "only available inside a struct method")
}

// Inside a method it reads fields, assigns them, and is returned for chaining.
func TestSelfStillWorksInsideAMethod(t *testing.T) {
	mustCheck(t, `struct Q {
		n: int
		label: string

		fn bump(): &Q {
			self.n = self.n + 1
			return self
		}

		fn describe(): string {
			return self.label
		}

		fn bare(): int {
			return n
		}
	}
	fn main(): void {
		let q: Q = Q { n: 0, label: "x" }
		q.bump().bump()
		let s: string = q.describe()
		let b: int = q.bare()
	}`)
}

// A module-level value is reached by its own name — what the rejected
// `self.total` above should have been written as.
func TestModuleLevelValueNeedsNoReceiver(t *testing.T) {
	mustCheck(t, `let total: int = 0
	fn bump(): int {
		total = total + 1
		return total
	}
	fn main(): void {
		let n: int = bump()
	}`)
}

// A catch clause's type is checked here rather than in the parser: the parser
// accepts any name so a typed handler can be written at all, and whether that
// name is a throwable type is a question about types.
func TestCatchUnknownTypeIsRejected(t *testing.T) {
	mustFail(t, `fn main(): void {
		try {
			throw Exception("x")
		} catch (e: WrongType) {
		}
	}`, "unknown type 'WrongType' in catch clause")
}

func TestCatchNonThrowableTypeIsRejected(t *testing.T) {
	mustFail(t, `struct Plain(code: int) {}
	fn main(): void {
		try {
			throw Exception("x")
		} catch (e: Plain) {
		}
	}`, "cannot be caught")
}

func TestThrowableStructWithScalarFieldsIsAccepted(t *testing.T) {
	mustCheck(t, `struct ParseError(message: string, line: int) {}
	fn load(): int throws ParseError {
		throw ParseError("bad", 3)
	}
	fn main(): void {
		try {
			let n = load()
		} catch (e: ParseError) {
			let l = e.line
		}
	}`)
}

func TestThrowsDeclarationRejectsANonThrowableType(t *testing.T) {
	mustFail(t, `struct Plain(code: int) {}
	fn f(): void throws Plain {
		throw Exception("x")
	}
	fn main(): void {
		f()
	}`, "cannot be thrown")
}
