package parser

import (
	"strings"
	"testing"
)

// An interface value owns a refcounted box, so it rides in an array like any
// other by-value element. The two containers that still refuse it do so for
// reasons of their own, and say which.
func TestParseInterfaceContainerRejected(t *testing.T) {
	tests := []struct {
		name   string
		source string
		substr string
	}{
		{
			name:   "map value",
			source: `interface Greeter { fn greet(): string } fn each(m: map[string, Greeter]): void {}`,
			substr: "a map value must be int, bool, string, long, double or char, not the interface Greeter",
		},
		{
			name:   "map key",
			source: `interface Greeter { fn greet(): string } fn each(m: map[Greeter, string]): void {}`,
			substr: "a map key must be string or int, not the interface Greeter",
		},
		{
			name:   "optional",
			source: `interface Greeter { fn greet(): string } fn maybe(g: Greeter?): void {}`,
			substr: "cannot make the interface Greeter optional",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseError(t, tt.source)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.substr)
			}
			if !strings.Contains(err.Error(), tt.substr) {
				t.Fatalf("error = %q, want to contain %q", err.Error(), tt.substr)
			}
		})
	}
}

// A map value or key the runtime has no suffix for used to fall through to
// "int": a long key was silently truncated and a struct value produced a C
// error about the generated code rather than about the program.
func TestParseMapTypeUnsupported(t *testing.T) {
	tests := []struct {
		name   string
		source string
		substr string
	}{
		{
			name:   "struct value",
			source: `struct Person { name: string } fn each(m: map[string, Person]): void {}`,
			substr: "not the struct Person",
		},
		{
			name:   "long key",
			source: `fn each(m: map[long, string]): void {}`,
			substr: "a map key must be string or int, not long",
		},
		{
			name:   "array value",
			source: `fn each(m: map[string, int[]]): void {}`,
			substr: "not an array",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseError(t, tt.source)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.substr)
			}
			if !strings.Contains(err.Error(), tt.substr) {
				t.Fatalf("error = %q, want to contain %q", err.Error(), tt.substr)
			}
		})
	}
}

// An interface array parses and is the position the owning box exists for.
func TestParseInterfaceArrayAccepted(t *testing.T) {
	parse(t, `interface Greeter { fn greet(): string }
	struct Person {
		name: string

		fn greet(): string {
			return name
		}
	}
	fn main(): int {
		let p = Person { name: "Ada" }
		let gs: Greeter[] = [p]
		return 0
	}`)
}

// A bare interface name is a type: the three positions the feature is for must
// still parse.
func TestParseInterfaceTypeAccepted(t *testing.T) {
	parse(t, `interface Greeter { fn greet(): string }
	struct Person {
		name: string

		fn greet(): string {
			return name
		}
	}
	fn greetingOf(g: Greeter): string {
		return g.greet()
	}
	fn main(): int {
		let p = Person { name: "Ada" }
		let g: Greeter = p
		greetingOf(g)
		return 0
	}`)
}
