package udf

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// maxDirSize bounds how much directory data is read into memory.
const maxDirSize = 64 << 20

// File characteristics in a file identifier descriptor.
const (
	fidDirectory = 0x02
	fidDeleted   = 0x04
	fidParent    = 0x08
)

var errNotDir = errors.New("not a directory")

type dirent struct {
	name string
	dir  bool
	icb  longAD
}

// readDir parses the file identifier descriptors of a directory, skipping
// the parent entry, deleted entries, entries whose names cannot be used as
// a path element, and all but the first of any duplicate names.
func (e *entry) readDir() ([]dirent, error) {
	if e.fileType != fileTypeDirectory {
		return nil, errNotDir
	}
	if e.size > maxDirSize {
		return nil, fmt.Errorf("%w: directory of %d bytes", ErrCorrupt, e.size)
	}
	data := make([]byte, e.size)
	if _, err := e.ReadAt(data, 0); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	var out []dirent
	seen := map[string]bool{}
	for len(data) > 0 && !allZero(data) {
		if len(data) < 38 {
			return nil, fmt.Errorf("%w: truncated file identifier", ErrCorrupt)
		}
		lfi, liu := int(data[19]), int(le16(data[36:]))
		used := 38 + liu + lfi
		if used > len(data) {
			return nil, fmt.Errorf("%w: file identifier overruns directory", ErrCorrupt)
		}
		total := min((used+3)&^3, len(data))
		if _, err := expectTag(data[:total], anyLocation, tagFID); err != nil {
			return nil, err
		}
		chars := data[18]
		nameBytes := data[38+liu : used]
		icb := parseLongAD(data[20:36])
		data = data[total:]
		if chars&(fidParent|fidDeleted) != 0 {
			continue
		}
		name, err := decodeDchars(nameBytes)
		if err != nil {
			return nil, err
		}
		if !usableName(name) || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, dirent{name: name, dir: chars&fidDirectory != 0, icb: icb})
	}
	return out, nil
}

func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

// usableName reports whether name can be a single io/fs path element.
func usableName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\x00")
}
