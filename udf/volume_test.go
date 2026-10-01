package udf_test

import (
	"bytes"
	"errors"
	"io/fs"
	"testing"

	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
	"github.com/chad3814/zenvik/udf"
)

func pattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7 + i/2048)
	}
	return b
}

var sample = map[string]udfimage.File{
	"BDMV/index.bdmv":          {Data: []byte("INDX0200 index")},
	"BDMV/MovieObject.bdmv":    {Data: []byte("MOBJ0200")},
	"BDMV/PLAYLIST/00800.mpls": {Data: bytes.Repeat([]byte("p"), 5000)},
	"BDMV/STREAM/00001.m2ts":   {Data: pattern(3*2048 + 17)},
	"BDMV/META/DL/タイトル.txt":    {Data: []byte("unicode name")},
	"BDMV/BACKUP/empty.bin":    {},
}

var layouts = []struct {
	name string
	opt  udfimage.Options
}{
	{"udf102", udfimage.Options{Revision: 0x0102, Label: "ZENVIK_102"}},
	{"udf102-embedded", udfimage.Options{Revision: 0x0102, Label: "ZENVIK_102", Embed: true}},
	{"udf250", udfimage.Options{Revision: 0x0250, Label: "ZENVIK_250"}},
	{"udf250-embedded", udfimage.Options{Revision: 0x0250, Label: "ZENVIK_250", Embed: true}},
}

func buildImage(t testing.TB, files map[string]udfimage.File, opt udfimage.Options) []byte {
	t.Helper()
	img, err := udfimage.Build(files, opt)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func openImage(t testing.TB, img []byte) *udf.FS {
	t.Helper()
	fsys, err := udf.Open(bytes.NewReader(img), int64(len(img)))
	if err != nil {
		t.Fatal(err)
	}
	return fsys
}

func TestOpenLabel(t *testing.T) {
	for _, l := range layouts {
		t.Run(l.name, func(t *testing.T) {
			fsys := openImage(t, buildImage(t, sample, l.opt))
			if fsys.Label() != l.opt.Label {
				t.Errorf("Label = %q, want %q", fsys.Label(), l.opt.Label)
			}
		})
	}
}

func TestOpenNotUDF(t *testing.T) {
	img := make([]byte, 1<<20)
	if _, err := udf.Open(bytes.NewReader(img), int64(len(img))); !errors.Is(err, udf.ErrNotUDF) {
		t.Errorf("err = %v, want ErrNotUDF", err)
	}
	if _, err := udf.Open(bytes.NewReader(nil), 0); !errors.Is(err, udf.ErrNotUDF) {
		t.Errorf("empty: err = %v, want ErrNotUDF", err)
	}
}

func TestOpenFallsBackToLastAnchor(t *testing.T) {
	img := buildImage(t, sample, layouts[2].opt)
	clear(img[256*2048 : 257*2048])
	fsys := openImage(t, img)
	if fsys.Label() != "ZENVIK_250" {
		t.Errorf("Label = %q", fsys.Label())
	}
}

func TestOpenFallsBackToReserveSequence(t *testing.T) {
	img := buildImage(t, sample, layouts[2].opt)
	img[35*2048+100] ^= 0xFF // corrupt the main LVD
	fsys := openImage(t, img)
	if fsys.Label() != "ZENVIK_250" {
		t.Errorf("Label = %q", fsys.Label())
	}
}

func TestOpenRejectsBadPartitionMapTable(t *testing.T) {
	img := buildImage(t, sample, layouts[0].opt)
	for _, lvd := range []int{35, 48 + 3} { // main and reserve LVD
		d := img[lvd*2048 : (lvd+1)*2048]
		d[264], d[265], d[266], d[267] = 0xFF, 0xFF, 0, 0 // map table length 65535
		fixTag(d)
	}
	if _, err := udf.Open(bytes.NewReader(img), int64(len(img))); !errors.Is(err, udf.ErrCorrupt) {
		t.Errorf("err = %v, want ErrCorrupt", err)
	}
}

func TestOpenLabelPrefersLVD(t *testing.T) {
	for _, rev := range []uint16{0x0102, 0x0250} {
		img := buildImage(t, sample, udfimage.Options{Revision: rev, Label: "LVD_LABEL", PVDLabel: "PVD_LABEL"})
		if got := openImage(t, img).Label(); got != "LVD_LABEL" {
			t.Errorf("rev %#x: Label = %q, want the LVD identifier", rev, got)
		}
	}
}

func TestOpenLabelFallsBackToPVD(t *testing.T) {
	img := buildImage(t, sample, udfimage.Options{Revision: 0x0250, PVDLabel: "PVD_LABEL"})
	if got := openImage(t, img).Label(); got != "PVD_LABEL" {
		t.Errorf("Label = %q, want the PVD identifier", got)
	}
}

func TestOpenCorruptLabelStillOpens(t *testing.T) {
	img := buildImage(t, sample, layouts[2].opt)
	for _, lvd := range []int{35, 48 + 3} { // main and reserve LVD
		d := img[lvd*2048 : (lvd+1)*2048]
		d[84] = 99 // invalid character compression ID
		fixTag(d)
	}
	fsys := openImage(t, img)
	if fsys.Label() != "" {
		t.Errorf("Label = %q, want empty", fsys.Label())
	}
	if _, err := fs.ReadFile(fsys, "BDMV/index.bdmv"); err != nil {
		t.Errorf("ReadFile: %v", err)
	}
}
