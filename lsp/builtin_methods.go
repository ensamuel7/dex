package lsp

import (
	"github.com/ensamuel7/dex/ast"
)

// builtinMethod is one method a built-in receiver has — an array, a string, a
// map or a StringBuilder. sig is written as it reads at a call site, with the
// receiver's own types filled in: a map[string, int] has set(key: string,
// value: int), not set(key, value).
//
// These tables are what both hover and completion read, so the two cannot
// drift apart. They mirror the method switches in checker/check_methods.go;
// a method added there needs a row here to be visible in the editor.
type builtinMethod struct {
	name string
	sig  string
	doc  string
}

// builtinMethods returns the methods a value of type t has, and false when t
// is not a built-in receiver.
func builtinMethods(t ast.Type) ([]builtinMethod, bool) {
	switch {
	case isArray(t):
		elem := ast.ElementType(t)
		return arrayMethodsFor(typeName(elem), ast.IsStructArrayType(t), elem == ast.TypeBool), true
	case t == ast.TypeString:
		return stringMethods, true
	case ast.IsMapType(t):
		return mapMethodsFor(typeName(ast.MapKeyType(t)), typeName(ast.MapValueType(t))), true
	case t == ast.TypeStringBuilder:
		return stringBuilderMethods, true
	}
	return nil, false
}

// builtinMethodOf looks up one method of a built-in receiver.
func builtinMethodOf(t ast.Type, name string) (builtinMethod, bool) {
	methods, ok := builtinMethods(t)
	if !ok {
		return builtinMethod{}, false
	}
	for _, m := range methods {
		if m.name == name {
			return m, true
		}
	}
	return builtinMethod{}, false
}

func isArray(t ast.Type) bool {
	return ast.IsArrayType(t) || ast.IsStructArrayType(t)
}

// arrayMethodsFor takes the element type by name so completion, which does not
// know the receiver, can ask for the methods of a T[].
//
// A struct array has no contains, indexOf or sort — there is no equality or
// ordering on a struct — and a bool array has no sort.
func arrayMethodsFor(elem string, structElem, boolElem bool) []builtinMethod {
	out := []builtinMethod{
		{"len", "len(): int", "The number of elements."},
		{"push", "push(value: " + elem + "): void", "Append an element."},
		{"pop", "pop(): " + elem, "Remove and return the last element."},
		{"remove", "remove(index: int): void", "Remove the element at index."},
	}
	if !structElem {
		out = append(out,
			builtinMethod{"contains", "contains(value: " + elem + "): bool", "Whether the array holds the value."},
			builtinMethod{"indexOf", "indexOf(value: " + elem + "): int", "The index of the value, or -1."},
		)
	}
	out = append(out, builtinMethod{"reverse", "reverse(): void", "Reverse the array in place."})
	if !structElem && !boolElem {
		out = append(out, builtinMethod{"sort", "sort(direction: string): void", "Sort in place: \"asc\" or \"desc\"."})
	}
	return out
}

var stringMethods = []builtinMethod{
	{"len", "len(): int", "The length of the string."},
	{"contains", "contains(sub: string): bool", "Whether the string contains sub."},
	{"startsWith", "startsWith(prefix: string): bool", "Whether the string starts with prefix."},
	{"endsWith", "endsWith(suffix: string): bool", "Whether the string ends with suffix."},
	{"indexOf", "indexOf(sub: string): int", "The index of sub, or -1."},
	{"toLower", "toLower(): string", "A lowercase copy."},
	{"toUpper", "toUpper(): string", "An uppercase copy."},
	{"trim", "trim(): string", "A copy without leading and trailing whitespace."},
	{"split", "split(delimiter: string): string[]", "Split into an array at each delimiter."},
	{"substring", "substring(start: int, end: int): string", "The characters from start up to, not including, end."},
	{"replace", "replace(old: string, new: string): string", "A copy with every occurrence of old replaced by new."},
	{"charAt", "charAt(index: int): char", "The character at index."},
	{"isAlphanumeric", "isAlphanumeric(): bool", "Whether every character is a letter or digit. False when empty."},
	{"isAlpha", "isAlpha(): bool", "Whether every character is a letter. False when empty."},
	{"isDigit", "isDigit(): bool", "Whether every character is a digit. False when empty."},
	{"isNumeric", "isNumeric(): bool", "Whether every character is a digit or '.'. False when empty."},
	{"isWhitespace", "isWhitespace(): bool", "Whether every character is whitespace. False when empty."},
	{"isEmpty", "isEmpty(): bool", "Whether the length is 0."},
	{"containsUppercase", "containsUppercase(): bool", "Whether any character is uppercase."},
	{"containsLowercase", "containsLowercase(): bool", "Whether any character is lowercase."},
	{"containsDigit", "containsDigit(): bool", "Whether any character is a digit."},
}

// mapMethodsFor takes the key and value types by name, for the same reason as
// arrayMethodsFor.
func mapMethodsFor(key, val string) []builtinMethod {
	return []builtinMethod{
		{"set", "set(key: " + key + ", value: " + val + "): void", "Set the value for key."},
		{"get", "get(key: " + key + "): " + val, "The value for key, or the zero value when absent."},
		{"has", "has(key: " + key + "): bool", "Whether key is present."},
		{"remove", "remove(key: " + key + "): void", "Remove key and its value."},
		{"len", "len(): int", "The number of entries."},
		{"clear", "clear(): void", "Remove every entry."},
		{"keys", "keys(): " + key + "[]", "Every key, as an array."},
		{"values", "values(): " + val + "[]", "Every value, as an array."},
	}
}

var stringBuilderMethods = []builtinMethod{
	{"append", "append(value: string | int | long | double | bool | char): void", "Append a value to the buffer."},
	{"toString", "toString(): string", "Build the string from the buffer."},
	{"len", "len(): int", "The current length of the buffer."},
	{"clear", "clear(): void", "Reset the buffer to empty."},
}

// builtinMethodCompletions is what completion offers after a dot whose receiver
// it cannot type: every built-in receiver's methods, with placeholder types. The
// detail names the receiver, because len, contains, indexOf and remove exist
// on more than one and mean something different on each.
func builtinMethodCompletions() []CompletionItem {
	groups := []struct {
		receiver string
		methods  []builtinMethod
	}{
		{"T[]", arrayMethodsFor("T", false, false)},
		{"string", stringMethods},
		{"map[K, V]", mapMethodsFor("K", "V")},
		{"StringBuilder", stringBuilderMethods},
	}
	var items []CompletionItem
	for _, g := range groups {
		for _, m := range g.methods {
			items = append(items, CompletionItem{
				Label:         m.name,
				Kind:          CompletionKindFunction,
				Detail:        g.receiver + "." + m.sig,
				Documentation: m.doc,
			})
		}
	}
	return items
}
