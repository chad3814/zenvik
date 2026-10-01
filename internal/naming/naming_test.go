package naming

import (
	"strings"
	"testing"
)

func TestCleanLabel(t *testing.T) {
	tests := map[string]string{
		"THE_MATRIX":      "The Matrix",
		"THE_MATRIX_1999": "The Matrix 1999",
		"  big  buck  ":   "Big Buck",
		"ÉCOLE_DES_FANS":  "École Des Fans",
		"":                "",
		"___":             "",
	}
	for in, want := range tests {
		if got := CleanLabel(in); got != want {
			t.Errorf("CleanLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSafeFileName(t *testing.T) {
	tests := map[string]string{
		"Sample Movie":               "Sample Movie",
		`AC/DC: Live <at> "Wembley"`: "AC_DC_ Live _at_ _Wembley_",
		"a\\b|c?d*e":                 "a_b_c_d_e",
		"tab\there":                  "tab_here",
		"trailing dots...":           "trailing dots",
		"  spaced  ":                 "spaced",
		"":                           "untitled",
		"...":                        "untitled",
		"Amélie":                     "Amélie",
	}
	for in, want := range tests {
		if got := SafeFileName(in); got != want {
			t.Errorf("SafeFileName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSafeFileNameReservedAndLong(t *testing.T) {
	for in, want := range map[string]string{
		"CON":       "CON_",
		"nul.txt":   "nul_.txt",
		"Com1":      "Com1_",
		"COM10":     "COM10",
		"console":   "console",
		"COM0":      "COM0_",
		"lpt0":      "lpt0_",
		"COM¹":      "COM¹_",
		"com²":      "com²_",
		"COM³":      "COM³_",
		"LPT¹":      "LPT¹_",
		"LPT²":      "LPT²_",
		"lpt³.x":    "lpt³_.x",
		"CONIN$":    "CONIN$_",
		"conout$":   "conout$_",
		"CON .txt":  "CON_ .txt",
		"aux  .a.b": "aux_  .a.b",
		"CONIN":     "CONIN",
		"LPT⁴":      "LPT⁴",
	} {
		if got := SafeFileName(in); got != want {
			t.Errorf("SafeFileName(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("ab", 300)
	if got := SafeFileName(long); len(got) > maxComponentBytes {
		t.Errorf("SafeFileName(long) = %d bytes", len(got))
	}
}
