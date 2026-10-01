package bluray

import (
	"errors"
	"testing"
)

const sampleMeta = `<?xml version="1.0" encoding="utf-8"?>
<disclib xmlns="urn:BDA:bdmv;disclib" xmlns:di="urn:BDA:bdmv;discinfo">
  <di:discinfo>
    <di:date>2026-01-01</di:date>
    <di:title>
      <di:name> Big Buck Bunny </di:name>
      <di:numSets>1</di:numSets>
    </di:title>
    <di:description><di:thumbnail href="thumb.jpg"/></di:description>
    <di:language>eng</di:language>
  </di:discinfo>
</disclib>`

func TestParseMeta(t *testing.T) {
	m, err := ParseMeta([]byte(sampleMeta))
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Big Buck Bunny" || m.Language != "eng" {
		t.Errorf("ParseMeta = %+v", m)
	}
}

func TestParseMetaInvalid(t *testing.T) {
	if _, err := ParseMeta([]byte("<disclib><unclosed>")); !errors.Is(err, ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
}

func TestPickMetaFile(t *testing.T) {
	tests := []struct {
		in   []string
		want string
	}{
		{[]string{"bdmt_fra.xml", "bdmt_eng.xml"}, "bdmt_eng.xml"},
		{[]string{"thumb.jpg", "bdmt_jpn.xml", "bdmt_fra.xml"}, "bdmt_fra.xml"},
		{[]string{"thumb.jpg"}, ""},
		{nil, ""},
	}
	for _, tt := range tests {
		if got := PickMetaFile(tt.in); got != tt.want {
			t.Errorf("PickMetaFile(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
