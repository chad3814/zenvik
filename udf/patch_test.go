package udf_test

import (
	"bytes"
	"io/fs"
	"slices"
	"testing"

	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
)

// patchFiles: every directory name and file name below has a FID whose
// unpadded length isn't a multiple of 4, except "X" (38 + 2 = 40).
var patchFiles = map[string]udfimage.File{
	"BDMV/index.bdmv":          {Data: []byte("INDX0200")},
	"BDMV/PLAYLIST/00001.mpls": {Data: []byte("MPLS0200")},
	"CERTIFICATE/id.bdmv":      {Data: []byte("id")},
	"X":                        {Data: []byte("x")},
}

func TestPaddingCRCFixes(t *testing.T) {
	for _, rev := range []uint16{0x0102, 0x0250} {
		clean := buildImage(t, patchFiles, udfimage.Options{Revision: rev})
		if fixes, err := openImage(t, clean).PaddingCRCFixes(); err != nil || len(fixes) != 0 {
			t.Fatalf("rev %#x clean: %d fixes, err %v; want none", rev, len(fixes), err)
		}

		bad := buildImage(t, patchFiles, udfimage.Options{Revision: rev, UnpaddedFIDCRC: true})
		orig := slices.Clone(bad)
		fixes, err := openImage(t, bad).PaddingCRCFixes()
		if err != nil {
			t.Fatalf("rev %#x: %v", rev, err)
		}
		// root: BDMV, CERTIFICATE, X(no fix); BDMV: PLAYLIST, index.bdmv; PLAYLIST: 00001.mpls; CERTIFICATE: id.bdmv
		var dirs []string
		for _, p := range fixes {
			dirs = append(dirs, p.Dir)
			if len(p.Old) != 16 || len(p.New) != 16 {
				t.Errorf("rev %#x: patch at %d is %d/%d bytes, want 16 (no tag crosses an extent here)", rev, p.Off, len(p.Old), len(p.New))
			}
			if !bytes.Equal(bad[p.Off:p.Off+int64(len(p.Old))], p.Old) {
				t.Errorf("rev %#x: patch Old at %d doesn't match the image", rev, p.Off)
			}
		}
		slices.Sort(dirs)
		if want := []string{".", ".", "BDMV", "BDMV", "BDMV/PLAYLIST", "CERTIFICATE"}; !slices.Equal(dirs, want) {
			t.Errorf("rev %#x: patch dirs %v, want %v", rev, dirs, want)
		}

		for _, p := range fixes {
			copy(bad[p.Off:], p.New)
		}
		f := openImage(t, bad)
		if again, err := f.PaddingCRCFixes(); err != nil || len(again) != 0 {
			t.Errorf("rev %#x after patching: %d fixes, err %v; want none", rev, len(again), err)
		}
		for name, want := range patchFiles {
			got, err := fs.ReadFile(f, name)
			if err != nil || !bytes.Equal(got, want.Data) {
				t.Errorf("rev %#x after patching: %s = %q, %v", rev, name, got, err)
			}
		}
		patched := make([]bool, len(bad))
		for _, p := range fixes {
			for i := range p.New {
				patched[p.Off+int64(i)] = true
			}
		}
		for i := range bad {
			if !patched[i] && bad[i] != orig[i] {
				t.Fatalf("rev %#x: byte %d changed outside the patches", rev, i)
			}
		}
	}
}

func TestFlawedImageStillReads(t *testing.T) {
	bad := buildImage(t, patchFiles, udfimage.Options{Revision: 0x0250, UnpaddedFIDCRC: true})
	f := openImage(t, bad)
	got, err := fs.ReadFile(f, "BDMV/PLAYLIST/00001.mpls")
	if err != nil || string(got) != "MPLS0200" {
		t.Fatalf("read flawed image: %q, %v", got, err)
	}
}
