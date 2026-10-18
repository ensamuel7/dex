package checker

import "testing"

func TestMixedArithIntDoubleRefused(t *testing.T) {
	mustFail(t, `fn main(): void {
		let a: int = 3
		let b: double = 2.0
		let c: double = a * b
	}`, "cannot apply '*' to int and double")
}

func TestMixedArithLongIntRefused(t *testing.T) {
	mustFail(t, `fn main(): void {
		let a: long = 3
		let b: int = 2
		let c: long = a + b
	}`, "cannot apply '+' to long and int")
}

func TestMixedArithModRefused(t *testing.T) {
	mustFail(t, `fn main(): void {
		let a: long = 7
		let b: int = 2
		let c: long = a % b
	}`, "cannot apply '%' to long and int")
}

func TestIntVarPlusDoubleLiteralRefused(t *testing.T) {
	mustFail(t, `fn main(): void {
		let a: int = 3
		let c: double = a + 2.0
	}`, "cannot apply '+' to int and double")
}

func TestCharVarPlusIntLiteralRefused(t *testing.T) {
	mustFail(t, `fn main(): void {
		let c: char = 'A'
		let n: int = c + 0
	}`, "cannot apply '+' to char and int")
}

func TestIntLiteralAdaptsInArith(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let d: double = 2.5
		let e: double = d * 2
		let l: long = 5
		let m: long = l + 1
	}`)
}

func TestNegatedIntLiteralAdaptsInArith(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let d: double = 2.5
		let e: double = d + -1
	}`)
}

func TestSameTypeArithStillPasses(t *testing.T) {
	mustCheck(t, `fn main(): void {
		let a: double = 3.0
		let b: double = 2.0
		let c: double = a * b
	}`)
}

func TestNumConversionResolvesMixedArith(t *testing.T) {
	mustCheck(t, `import "num"
	fn main(): void {
		let a: int = 3
		let b: double = 2.0
		let c: double = num.toDouble(a) * b
	}`)
}
