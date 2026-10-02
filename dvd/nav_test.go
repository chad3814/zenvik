package dvd_test

import (
	"errors"
	"testing"
	"time"

	"github.com/chad3814/zenvik/dvd"
	"github.com/chad3814/zenvik/internal/testdisc"
)

func TestNAVRoundTrip(t *testing.T) {
	want := dvd.NAV{StartPTM: 900000, EndPTM: 945045, VOBID: 3, CellID: 7, CellElapsed: time.Duration(12*30+15) * 1001 * time.Second / 30000} // 00:00:12:15, 375 NTSC frames
	got, err := dvd.ParseNAV(testdisc.NAVPack(want))
	if err != nil || got != want {
		t.Errorf("ParseNAV = %+v, %v; want %+v", got, err, want)
	}
}

func TestParseNAVRejects(t *testing.T) {
	good := testdisc.NAVPack(dvd.NAV{VOBID: 1, CellID: 1})
	for name, mut := range map[string]func([]byte){
		"short":            nil,
		"no pack header":   func(p []byte) { p[3] = 0xBB },
		"no system header": func(p []byte) { p[0x11] = 0xE0 },
		"PCI substream":    func(p []byte) { p[0x2C] = 0x01 },
		"DSI substream":    func(p []byte) { p[0x406] = 0x00 },
	} {
		p := append([]byte(nil), good...)
		if mut == nil {
			p = p[:2000]
		} else {
			mut(p)
		}
		if _, err := dvd.ParseNAV(p); !errors.Is(err, dvd.ErrNotNAV) {
			t.Errorf("%s: err = %v, want ErrNotNAV", name, err)
		}
	}
}

func TestParseNAVReversedPTM(t *testing.T) {
	if _, err := dvd.ParseNAV(testdisc.NAVPack(dvd.NAV{StartPTM: 900, EndPTM: 899, VOBID: 1, CellID: 1})); !errors.Is(err, dvd.ErrCorrupt) {
		t.Errorf("EndPTM < StartPTM: err = %v, want ErrCorrupt", err)
	}
	if _, err := dvd.ParseNAV(testdisc.NAVPack(dvd.NAV{StartPTM: 900, EndPTM: 900, VOBID: 1, CellID: 1})); err != nil {
		t.Errorf("EndPTM == StartPTM: err = %v", err)
	}
}
