package checker

import (
	"fmt"
	"strings"
)

// validateFieldTag checks that a field tag is well-formed: space-separated
// `key:"value"` pairs, the shape Go uses.
//
// The compiler checks the *syntax* and nothing else. It does not know what any
// particular key means — `db` is a database library's business, `json` a
// serializer's — and deliberately so, because a tag is data for a library
// rather than a directive to the compiler. What it can usefully catch is the
// typo: a missing quote or a stray colon silently makes a key invisible to
// whatever was meant to read it, and that failure surfaces far from its cause.
func validateFieldTag(structName, fieldName, tag string) error {
	if tag == "" {
		return nil
	}
	i := 0
	seen := map[string]bool{}
	for i < len(tag) {
		for i < len(tag) && (tag[i] == ' ' || tag[i] == '\t') {
			i++
		}
		if i >= len(tag) {
			break
		}
		keyStart := i
		for i < len(tag) && tag[i] != ':' && tag[i] != ' ' && tag[i] != '"' {
			i++
		}
		key := tag[keyStart:i]
		if key == "" {
			return fmt.Errorf("tag on '%s.%s' has a value with no key: expected key:\"value\"", structName, fieldName)
		}
		if i >= len(tag) || tag[i] != ':' {
			return fmt.Errorf("tag key '%s' on '%s.%s' is missing its ':\"value\"'", key, structName, fieldName)
		}
		i++ // consume ':'
		if i >= len(tag) || tag[i] != '"' {
			return fmt.Errorf("tag key '%s' on '%s.%s' must have a quoted value: %s:\"...\"", key, structName, fieldName, key)
		}
		i++ // consume opening quote
		valStart := i
		for i < len(tag) && tag[i] != '"' {
			i++
		}
		if i >= len(tag) {
			return fmt.Errorf("tag key '%s' on '%s.%s' has an unclosed quote", key, structName, fieldName)
		}
		_ = valStart
		i++ // consume closing quote
		if seen[key] {
			return fmt.Errorf("tag on '%s.%s' repeats key '%s'", structName, fieldName, key)
		}
		seen[key] = true
		// Anything other than whitespace between pairs is a mistake.
		if i < len(tag) && tag[i] != ' ' && tag[i] != '\t' {
			return fmt.Errorf("tag on '%s.%s' needs a space between keys, got %q", structName, fieldName, string(tag[i]))
		}
	}
	return nil
}

// tagKeys lists the keys a tag defines, for diagnostics and tooling.
func tagKeys(tag string) []string {
	var keys []string
	for _, part := range strings.Fields(tag) {
		if idx := strings.Index(part, ":"); idx > 0 {
			keys = append(keys, part[:idx])
		}
	}
	return keys
}
