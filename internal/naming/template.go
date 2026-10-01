package naming

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// ErrTemplate reports an invalid name template.
var ErrTemplate = errors.New("zenvik: invalid name template")

var knownVariables = map[string]bool{"name": true, "year": true, "label": true, "playlist": true}

type segment struct {
	lit      string
	variable string // set for {variable} references
}

type part struct {
	segs     []segment
	optional bool
}

// Template is a parsed name template.
type Template struct{ parts []part }

func errf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrTemplate, fmt.Sprintf(format, args...))
}

// Parse parses a name template: literal text, {variable} references (name,
// year, label, playlist) and [optional groups], which do not nest.
func Parse(src string) (*Template, error) {
	t := &Template{}
	cur := part{}
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			cur.segs = append(cur.segs, segment{lit: lit.String()})
			lit.Reset()
		}
	}
	inGroup := false
	for i := 0; i < len(src); i++ {
		switch c := src[i]; c {
		case '{':
			end := strings.IndexByte(src[i:], '}')
			if end < 0 {
				return nil, errf("unclosed { at position %d", i)
			}
			name := src[i+1 : i+end]
			if !knownVariables[name] {
				return nil, errf("unknown variable {%s} (use {name}, {year}, {label} or {playlist})", name)
			}
			flush()
			cur.segs = append(cur.segs, segment{variable: name})
			i += end
		case '}':
			return nil, errf("unexpected } at position %d", i)
		case '[':
			if inGroup {
				return nil, errf("nested [ at position %d (groups do not nest)", i)
			}
			flush()
			if len(cur.segs) > 0 {
				t.parts = append(t.parts, cur)
			}
			cur = part{optional: true}
			inGroup = true
		case ']':
			if !inGroup {
				return nil, errf("unexpected ] at position %d", i)
			}
			flush()
			t.parts = append(t.parts, cur)
			cur = part{}
			inGroup = false
		default:
			lit.WriteByte(c)
		}
	}
	if inGroup {
		return nil, errf("unclosed [")
	}
	flush()
	if len(cur.segs) > 0 {
		t.parts = append(t.parts, cur)
	}
	if len(t.parts) == 0 {
		return nil, errf("template is empty")
	}
	return t, nil
}

// Render fills in vars (missing keys are empty) and returns a relative,
// slash-separated path whose components are safe file names. An optional
// group is dropped when any variable in it is empty. "/" and "\" inside a
// value become "_", so values cannot create directories. The final
// component gets ".mkv" unless it already ends with it.
func (t *Template) Render(vars map[string]string) (string, error) {
	var b strings.Builder
	absolute := false // the template's text itself starts with "/"
	for pi, p := range t.parts {
		var pb strings.Builder
		keep := true
		partAbsolute := false
		for i, s := range p.segs {
			if s.variable == "" {
				if pi == 0 && i == 0 {
					partAbsolute = strings.HasPrefix(s.lit, "/")
				}
				pb.WriteString(s.lit)
				continue
			}
			v := strings.TrimSpace(vars[s.variable])
			if v == "" && p.optional {
				keep = false
				break
			}
			v = strings.NewReplacer("/", "_", `\`, "_").Replace(v)
			if v != "" && strings.Trim(v, ".") == "" {
				v = "_" // a value of "." or ".." must not become a path component
			}
			pb.WriteString(v)
		}
		if keep {
			absolute = absolute || partAbsolute
			b.WriteString(pb.String())
		}
	}
	return finish(b.String(), absolute)
}

// finish cleans each component of the rendered path p. absolute reports
// that p's leading "/" came from template text rather than from an empty
// variable.
func finish(p string, absolute bool) (string, error) {
	if absolute {
		return "", errf("template must produce a relative path, got %q", p)
	}
	comps := strings.Split(p, "/")
	for i, c := range comps {
		if c == "." || c == ".." {
			return "", errf("template produced a %q path component in %q", c, p)
		}
		clean := cleanComponent(c)
		if clean == "" {
			return "", errf("template produced an empty path component in %q (a folder or file name rendered empty)", p)
		}
		comps[i] = limitLength(clean, maxComponentBytes)
	}
	last := len(comps) - 1
	if strings.EqualFold(path.Ext(comps[last]), ".mkv") {
		if base := comps[last][:len(comps[last])-len(".mkv")]; strings.Trim(base, ". ") == "" {
			return "", errf("template produced an empty file name in %q (the file name is empty before .mkv)", p)
		}
	} else {
		comps[last] = limitLength(comps[last]+".mkv", maxComponentBytes)
	}
	return strings.Join(comps, "/"), nil
}
