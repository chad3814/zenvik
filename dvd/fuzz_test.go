package dvd_test

import (
	"testing"

	"github.com/chad3814/zenvik/dvd"
	"github.com/chad3814/zenvik/internal/testdisc"
)

func FuzzParseVMG(f *testing.F) {
	f.Add(testdisc.VMGFile(&dvd.VMG{TitleSets: 1, Titles: []dvd.TitleEntry{{Angles: 1, Chapters: 3, TitleSet: 1, TitleSetTitle: 1}}}))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = dvd.ParseVMG(b)
	})
}

func FuzzParseVTS(f *testing.F) {
	f.Add(testdisc.VTSFile(sampleVTS()))
	f.Fuzz(func(t *testing.T, b []byte) {
		v, err := dvd.ParseVTS(b)
		if err != nil {
			return
		}
		for _, chapters := range v.Titles {
			for _, p := range chapters {
				_ = v.PGCs[p.PGC-1].Programs[p.Program-1] // must not panic: ParseVTS validated it
			}
		}
	})
}
