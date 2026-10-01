package zenvik

import (
	"bytes"
	"errors"
	"testing"
	"testing/iotest"

	"github.com/chad3814/zenvik/internal/testdisc"
)

func TestClipEncrypted(t *testing.T) {
	broken := testdisc.CleanM2TS(1)
	broken[31*192+4] = 0x00 // only the last packet of the unit lacks its sync byte
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"clean", testdisc.CleanM2TS(1), false},
		{"clean multi-unit", testdisc.CleanM2TS(3), false},
		{"scrambled", testdisc.ScrambledM2TS(1), true},
		{"last packet scrambled", broken, true},
		{"shorter than a unit", testdisc.CleanM2TS(1)[:5*192+17], false},
		{"single packet", testdisc.ScrambledM2TS(1)[:192], false},
		{"empty", nil, false},
	}
	for _, tt := range tests {
		got, err := clipEncrypted(bytes.NewReader(tt.data))
		if err != nil || got != tt.want {
			t.Errorf("%s: clipEncrypted = %v, %v; want %v", tt.name, got, err, tt.want)
		}
	}
}

func TestClipEncryptedReadError(t *testing.T) {
	boom := errors.New("boom")
	if _, err := clipEncrypted(iotest.ErrReader(boom)); !errors.Is(err, boom) {
		t.Errorf("err = %v, want boom", err)
	}
}
