package dvd_test

import (
	"testing"
	"time"

	"github.com/chad3814/zenvik/dvd"
	"github.com/chad3814/zenvik/internal/testdisc"
)

func TestNAVRoundTrip(t *testing.T) {
	want := dvd.NAV{StartPTM: 900000, EndPTM: 945045, VOBID: 3, CellID: 7, CellElapsed: 12*time.Second + 15*1001*time.Second/30000}
	got, ok := dvd.ParseNAV(testdisc.NAVPack(want))
	if !ok || got != want {
		t.Errorf("ParseNAV = %+v, %v; want %+v", got, ok, want)
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
		if _, ok := dvd.ParseNAV(p); ok {
			t.Errorf("%s: ParseNAV accepted it", name)
		}
	}
}
