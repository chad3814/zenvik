package bluray

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// DiscMeta is the disc title information from a disc library metadata
// file (BDMV/META/DL/bdmt_<lang>.xml).
type DiscMeta struct {
	Title    string
	Language string
}

// ParseMeta parses a bdmt_*.xml file.
func ParseMeta(b []byte) (*DiscMeta, error) {
	var doc struct {
		XMLName  xml.Name `xml:"disclib"`
		DiscInfo struct {
			Title struct {
				Name string `xml:"name"`
			} `xml:"title"`
			Language string `xml:"language"`
		} `xml:"discinfo"`
	}
	if err := xml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%w: disc library metadata: %w", ErrInvalid, err)
	}
	return &DiscMeta{
		Title:    strings.TrimSpace(doc.DiscInfo.Title.Name),
		Language: strings.TrimSpace(doc.DiscInfo.Language),
	}, nil
}

// PickMetaFile chooses which metadata file to read from the names in
// BDMV/META/DL: English if present, otherwise the lexically first
// bdmt_*.xml, otherwise "".
func PickMetaFile(names []string) string {
	best := ""
	for _, n := range names {
		if !strings.HasPrefix(n, "bdmt_") || !strings.HasSuffix(n, ".xml") {
			continue
		}
		if n == "bdmt_eng.xml" {
			return n
		}
		if best == "" || n < best {
			best = n
		}
	}
	return best
}
