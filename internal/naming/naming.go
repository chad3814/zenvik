// Package naming builds human-readable names and safe file names.
package naming

import (
	"path"
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

// maxComponentBytes keeps "<component>.partial" within the common 255-byte
// file name limit.
const maxComponentBytes = 255 - len(".partial")

var reservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// cleanComponent makes one path component safe on macOS, Linux and
// Windows: invalid characters (<>:"/\|?* and controls) become "_",
// surrounding spaces and trailing dots are removed, and Windows reserved
// device names get a "_" suffix. The result may be empty.
func cleanComponent(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`<>:"/\|?*`, r) {
			b.WriteRune('_')
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimRight(strings.TrimSpace(b.String()), ". ")
	base, ext, _ := strings.Cut(out, ".")
	if reservedNames[strings.ToUpper(base)] {
		out = base + "_"
		if ext != "" {
			out += "." + ext
		}
	}
	return out
}

// limitLength shortens s to at most max bytes at a rune boundary, keeping a
// short extension (such as ".mkv") intact.
func limitLength(s string, max int) string {
	if len(s) <= max {
		return s
	}
	ext := path.Ext(s)
	if len(ext) > 16 {
		ext = ""
	}
	base := strings.TrimSuffix(s, ext)
	limit := max - len(ext)
	cut := 0
	for i := range base {
		if i > limit {
			break
		}
		cut = i
	}
	if len(base) <= limit {
		cut = len(base)
	}
	return strings.TrimRight(base[:cut], ". ") + ext
}

// SafeFileName makes s usable as a file name on macOS, Linux and Windows
// (see cleanComponent), limits it to 247 bytes, and returns "untitled" for
// an empty result.
func SafeFileName(s string) string {
	out := limitLength(cleanComponent(s), maxComponentBytes)
	if out == "" {
		return "untitled"
	}
	return out
}
