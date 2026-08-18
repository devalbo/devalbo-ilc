package clispec

import "strings"

// ShortEnum drops the prefix every value of an enum shares.
//
//	PROBLEM_UNSPECIFIED, PROBLEM_DIVIDE_BY_ZERO, PROBLEM_OVERFLOW
//	-> unspecified, divide_by_zero, overflow
//
// # Why the COMMON prefix, and not "everything before the last underscore"
//
// That was the first rule, and it turned `PROBLEM_DIVIDE_BY_ZERO` into `zero` —
// which is not an abbreviation, it is a different word. It happens to work for
// single-word values (`STYLE_PLAIN` -> `plain`) and silently mangles every
// multi-word one, so it is right exactly until an app writes a value name with
// two words in it.
//
// "Everything before the FIRST underscore" fails the other way: `SPEC_KIND_ENUM`
// would keep `KIND_ENUM`, because that enum's own name is two words.
//
// The shared prefix is the only rule that is right in both cases, and it needs
// the whole list — which is why this takes one rather than a single name.
//
// # What it never does
//
// It never changes the VALUE. This is how a name reads; the number the app
// declared is what crosses the wire, and `EnumNumbers` carries that.
func ShortEnum(values []string) []string {
	out := make([]string, len(values))
	copy(out, values)
	if len(values) < 2 {
		// ONE VALUE HAS NO SHARED PREFIX to speak of — any prefix is "common" to
		// a list of one, and stripping it would leave nothing. Left whole.
		return out
	}

	prefix := values[0]
	for _, value := range values[1:] {
		for !strings.HasPrefix(value, prefix) {
			prefix = prefix[:len(prefix)-1]
			if prefix == "" {
				return out
			}
		}
	}
	// TRIM BACK TO AN UNDERSCORE. Two values sharing `PROBLEM_OVER` (say
	// `PROBLEM_OVERFLOW` and `PROBLEM_OVERDRAFT`) share more characters than
	// they share WORDS, and cutting mid-word gives `flow` and `draft`.
	if at := strings.LastIndex(prefix, "_"); at >= 0 {
		prefix = prefix[:at+1]
	} else {
		return out
	}

	for i, value := range values {
		trimmed := strings.TrimPrefix(value, prefix)
		if trimmed == "" {
			// A value that IS the prefix keeps its full name rather than
			// becoming empty.
			continue
		}
		out[i] = strings.ToLower(trimmed)
	}
	return out
}
