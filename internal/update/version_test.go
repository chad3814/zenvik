package update

import (
	"errors"
	"testing"
)

func TestParse(t *testing.T) {
	good := map[string]Version{
		"v1.2.0":        {1, 2, 0, ""},
		"v0.0.1":        {0, 0, 1, ""},
		"v10.20.30":     {10, 20, 30, ""},
		"v1.1.0-rc3":    {1, 1, 0, "rc3"},
		"v2.0.0-beta.1": {2, 0, 0, "beta.1"},
	}
	for tag, want := range good {
		got, err := Parse(tag)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %+v, %v; want %+v", tag, got, err, want)
		}
		if got.String() != tag {
			t.Errorf("Parse(%q).String() = %q", tag, got.String())
		}
	}
	bad := []string{"", "dev", "dev (0123456789ab, modified)", "1.2.0", "v1.2", "v1.2.0.1", "v01.2.0",
		"v1.2.0+1", "v1.2.0-", "v1.2.0-rc..1", "v1.2.0-rc_1", "V1.2.0", "v1.2.0 ", "v-1.2.0", "v1.a.0"}
	for _, tag := range bad {
		if _, err := Parse(tag); !errors.Is(err, ErrBadVersion) {
			t.Errorf("Parse(%q) error = %v, want ErrBadVersion", tag, err)
		}
	}
}

func TestLess(t *testing.T) {
	// Each pair is (lower, higher).
	ordered := [][2]string{
		{"v1.2.0", "v1.3.0"},
		{"v1.2.0", "v2.0.0"},
		{"v1.2.0", "v1.2.1"},
		{"v1.9.9", "v1.10.0"},
		{"v1.1.0-rc3", "v1.1.0"},
		{"v1.1.0-rc.2", "v1.1.0-rc.10"},
		{"v1.1.0-alpha", "v1.1.0-alpha.1"},
		{"v1.1.0-alpha.1", "v1.1.0-alpha.beta"},
		{"v1.1.0-alpha.beta", "v1.1.0-beta"},
		{"v1.1.0-beta.2", "v1.1.0-beta.11"},
		{"v1.1.0-beta.11", "v1.1.0-rc.1"},
		{"v1.1.0-1", "v1.1.0-a"},
		{"v1.2.0", "v1.3.0-rc1"}, // a final is older than the next minor's rc
	}
	for _, p := range ordered {
		lo, hi := mustParse(t, p[0]), mustParse(t, p[1])
		if !lo.Less(hi) {
			t.Errorf("%s should be less than %s", p[0], p[1])
		}
		if hi.Less(lo) {
			t.Errorf("%s should not be less than %s", p[1], p[0])
		}
		if lo.Less(lo) || hi.Less(hi) {
			t.Errorf("Less must be irreflexive for %s / %s", p[0], p[1])
		}
	}
}

func mustParse(t *testing.T, tag string) Version {
	t.Helper()
	v, err := Parse(tag)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
