// Package update finds out whether a newer zenvik release exists. It talks
// to the GitHub Releases API, caches the answer for a day, and compares
// release tags by semantic-version precedence.
package update

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrBadVersion reports a string that is not a zenvik release tag.
var ErrBadVersion = errors.New("zenvik: not a release version")

// Version is a release tag: vMAJOR.MINOR.PATCH with an optional -PRERELEASE.
type Version struct {
	Major, Minor, Patch int
	Pre                 string // "" for a final release; "rc2" for v1.1.0-rc2
}

// Parse reads a tag such as "v1.2.0" or "v1.1.0-rc3". Anything else,
// including development version strings and build metadata ("+..."), wraps
// ErrBadVersion.
func Parse(tag string) (Version, error) {
	bad := func() (Version, error) { return Version{}, fmt.Errorf("%w: %q", ErrBadVersion, tag) }
	rest, ok := strings.CutPrefix(tag, "v")
	if !ok {
		return bad()
	}
	core, pre, hasPre := strings.Cut(rest, "-")
	if hasPre && !validPre(pre) {
		return bad()
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return bad()
	}
	var nums [3]int
	for i, p := range parts {
		n, ok := number(p)
		if !ok {
			return bad()
		}
		nums[i] = n
	}
	return Version{Major: nums[0], Minor: nums[1], Patch: nums[2], Pre: pre}, nil
}

// number parses a non-negative decimal with no leading zero.
func number(s string) (int, bool) {
	if !isNumeric(s) || (len(s) > 1 && s[0] == '0') {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// isNumeric reports whether s is one or more ASCII digits.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// validPre accepts dot-separated identifiers of ASCII letters, digits and
// hyphens, none empty.
func validPre(pre string) bool {
	for _, id := range strings.Split(pre, ".") {
		if id == "" {
			return false
		}
		for _, c := range id {
			alnum := c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
			if !alnum && c != '-' {
				return false
			}
		}
	}
	return true
}

// String renders v as its tag: "v1.2.0", "v1.1.0-rc3".
func (v Version) String() string {
	s := fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		s += "-" + v.Pre
	}
	return s
}

// Less reports whether v precedes o in semantic-version order: by major,
// minor and patch; a pre-release before its final; pre-releases identifier
// by identifier, numeric ones numerically and before alphanumeric ones,
// and a shorter list of equal identifiers first.
func (v Version) Less(o Version) bool {
	switch {
	case v.Major != o.Major:
		return v.Major < o.Major
	case v.Minor != o.Minor:
		return v.Minor < o.Minor
	case v.Patch != o.Patch:
		return v.Patch < o.Patch
	case v.Pre == o.Pre:
		return false
	case v.Pre == "":
		return false
	case o.Pre == "":
		return true
	}
	return preLess(v.Pre, o.Pre)
}

func preLess(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] == bs[i] {
			continue
		}
		an, bn := isNumeric(as[i]), isNumeric(bs[i])
		switch {
		case an && bn:
			x, _ := strconv.Atoi(as[i])
			y, _ := strconv.Atoi(bs[i])
			return x < y
		case an:
			return true
		case bn:
			return false
		}
		return as[i] < bs[i]
	}
	return len(as) < len(bs)
}
