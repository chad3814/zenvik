// Package naming builds human-readable names and safe file names.
package naming

import (
	"strings"
	"unicode"
)

// CleanLabel turns a volume label into a readable name: underscores become
// spaces and each word is title-cased ("THE_MATRIX" → "The Matrix").
func CleanLabel(label string) string {
	words := strings.FieldsFunc(label, func(r rune) bool { return r == '_' || unicode.IsSpace(r) })
	for i, w := range words {
		rs := []rune(strings.ToLower(w))
		rs[0] = unicode.ToUpper(rs[0])
		words[i] = string(rs)
	}
	return strings.Join(words, " ")
}

// SafeFileName makes s usable as a file name on macOS, Linux and Windows:
// characters invalid on any of them (<>:"/\|?* and control characters) become
// "_", surrounding spaces and trailing dots are removed, and an empty result
// becomes "untitled".
func SafeFileName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`<>:"/\|?*`, r) {
			b.WriteRune('_')
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimRight(strings.TrimSpace(b.String()), ". ")
	if out == "" {
		return "untitled"
	}
	return out
}
