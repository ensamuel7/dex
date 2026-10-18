package codegen

import "testing"

// Arithmetic promotes, so the result is not the left operand's type whenever
// the operands differ. typeOfExpr took the left one, so a double on the right
// of an adapted int literal was printed with %d — fmt.println(2 * d) emitted
// 0. Strict arithmetic closed the variable-variable case but not this one.
func TestCodegenPromotedTypeNotLeftOperand(t *testing.T) {
	out := generate(t, `import "fmt"
	fn main(): void {
		let d: double = 2.5
		fmt.println(2 * d)
	}`)
	assertContains(t, out, `printf("%f\n"`)
}

func TestCodegenPromotedLongFromLiteralOnTheLeft(t *testing.T) {
	out := generate(t, `import "fmt"
	fn main(): void {
		let l: long = 3
		fmt.println(2 * l)
	}`)
	assertContains(t, out, `printf("%ld\n"`)
}

func TestCodegenSameTypeArithmeticUnchanged(t *testing.T) {
	out := generate(t, `import "fmt"
	fn main(): void {
		let a: int = 2
		let b: int = 3
		fmt.println(a * b)
	}`)
	assertContains(t, out, `printf("%d\n"`)
}
