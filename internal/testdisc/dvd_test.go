package testdisc

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/chad3814/zenvik/dvd"
	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
	"github.com/chad3814/zenvik/udf"
)

func TestSampleDVDFiles(t *testing.T) {
	d := SampleDVD()
	files := d.Files()
	vmg, err := dvd.ParseVMG(files["VIDEO_TS/VIDEO_TS.IFO"])
	if err != nil {
		t.Fatal(err)
	}
	if len(vmg.Titles) != 8 || vmg.TitleSets != 4 {
		t.Fatalf("VMG = %+v", vmg)
	}
	for n := 1; n <= 4; n++ {
		name := fmt.Sprintf("VIDEO_TS/VTS_%02d_0.IFO", n)
		if _, err := dvd.ParseVTS(files[name]); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if got := len(files["VIDEO_TS/VTS_01_1.VOB"]); got != 40*2048 {
		t.Errorf("VTS_01_1.VOB is %d bytes, want %d", got, 40*2048)
	}
	if got := len(files["VIDEO_TS/VTS_01_2.VOB"]); got != 30*2048 {
		t.Errorf("VTS_01_2.VOB is %d bytes", got)
	}
}

func TestDVDNamesAndAppleDouble(t *testing.T) {
	d := SampleDVD()
	d.LowerCase, d.AppleDouble = true, true
	files := d.Files()
	for _, n := range []string{"video_ts/video_ts.ifo", "video_ts/vts_01_1.vob", "video_ts/._vts_01_1.vob"} {
		if _, ok := files[n]; !ok {
			t.Errorf("missing %s", n)
		}
	}
	if _, ok := files["VIDEO_TS/VIDEO_TS.IFO"]; ok {
		t.Error("upper-case name written with LowerCase set")
	}
}

func TestVOBPacks(t *testing.T) {
	clear := VOBPacks(2, false)
	scr := VOBPacks(2, true)
	if len(clear) != 4096 || clear[3] != 0xBA || clear[17] != 0xE0 {
		t.Fatalf("pack header wrong: % X", clear[:24])
	}
	if clear[20]&0x30 != 0 || scr[20]&0x30 != 0x10 || scr[2048+20]&0x30 != 0x10 {
		t.Errorf("scrambling bits: clear %02X, scrambled %02X", clear[20], scr[20])
	}
}

func TestDVDISO(t *testing.T) {
	img, err := SampleDVD().ISO(udfimage.Options{Revision: 0x0102, Label: "SAMPLE_DVD"})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "dvd.iso")
	if err := os.WriteFile(p, img, 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := udf.OpenImage(p)
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	if u.Label() != "SAMPLE_DVD" {
		t.Errorf("label = %q", u.Label())
	}
	b, err := fs.ReadFile(u, "VIDEO_TS/VIDEO_TS.IFO")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dvd.ParseVMG(b); err != nil {
		t.Error(err)
	}
}

func TestSampleDVDNavigation(t *testing.T) {
	files := SampleDVD().Files()
	vts, err := dvd.ParseVTS(files["VIDEO_TS/VTS_02_0.IFO"])
	if err != nil {
		t.Fatal(err)
	}
	if want := []uint32{0, 20, 40}; !slices.Equal(vts.VOBUs, want) {
		t.Errorf("VTS 2 VOBUs = %v, want %v", vts.VOBUs, want)
	}
	vob := files["VIDEO_TS/VTS_02_1.VOB"]
	nav, err := dvd.ParseNAV(vob[20*2048 : 21*2048])
	const ep = 35964 * 3003 // a 20-minute cell is 35964 NTSC frames of 3003 ticks
	if err != nil || nav.VOBID != 1 || nav.CellID != 2 || nav.EndPTM-nav.StartPTM != ep || nav.StartPTM != ep {
		t.Errorf("NAV at sector 20 = %+v, %v", nav, err)
	}
	if _, err := dvd.ParseNAV(vob[2048:4096]); err == nil {
		t.Error("sector 1 is not a VOBU start")
	}
}
