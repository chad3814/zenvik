//go:build integration && darwin

package udfimage

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestMacOSMountsImages checks the writer against the macOS UDF driver.
func TestMacOSMountsImages(t *testing.T) {
	files := map[string]File{
		"BDMV/index.bdmv":          {Data: []byte("INDX0200 test")},
		"BDMV/PLAYLIST/00800.mpls": {Data: bytes.Repeat([]byte("p"), 5000)},
		"BDMV/STREAM/00001.m2ts":   {Data: bytes.Repeat([]byte{0x47, 0x00}, 3*1024+9)},
		"BDMV/META/DL/タイトル.txt":    {Data: []byte("unicode name")},
		"CERTIFICATE/empty.bin":    {},
	}
	for _, tc := range []struct {
		name string
		opt  Options
	}{
		{"udf102", Options{Revision: 0x0102, Label: "ZENVIK_102"}},
		{"udf250", Options{Revision: 0x0250, Label: "ZENVIK_250"}},
		{"udf250-embedded", Options{Revision: 0x0250, Label: "ZENVIK_EMB", Embed: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img, err := Build(files, tc.opt)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			iso := filepath.Join(dir, "test.iso")
			if err := os.WriteFile(iso, img, 0o644); err != nil {
				t.Fatal(err)
			}
			mnt := filepath.Join(dir, "mnt")
			if err := os.Mkdir(mnt, 0o755); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command("hdiutil", "attach", "-readonly", "-nobrowse",
				"-imagekey", "diskimage-class=CRawDiskImage", "-mountpoint", mnt, iso).CombinedOutput()
			if err != nil {
				t.Fatalf("hdiutil attach: %v\n%s", err, out)
			}
			t.Cleanup(func() { _ = exec.Command("hdiutil", "detach", "-force", mnt).Run() })
			for name, f := range files {
				got, err := os.ReadFile(filepath.Join(mnt, filepath.FromSlash(name)))
				if err != nil {
					t.Errorf("%s: %v", name, err)
					continue
				}
				if !bytes.Equal(got, f.Data) {
					t.Errorf("%s: got %d bytes, want %d", name, len(got), len(f.Data))
				}
			}
		})
	}
}
