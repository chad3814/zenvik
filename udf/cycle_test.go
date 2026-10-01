package udf_test

import (
	"encoding/binary"
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
	"github.com/chad3814/zenvik/udf"
)

// fidOffsets returns the offsets of every file identifier descriptor in img
// whose name is the one-byte-compressed string name.
func fidOffsets(img []byte, name string) []int {
	var offs []int
	for off := 0; off+38 <= len(img); off += 4 {
		if binary.LittleEndian.Uint16(img[off:]) != 257 { // tagFID
			continue
		}
		liu, lfi := int(binary.LittleEndian.Uint16(img[off+36:])), int(img[off+19])
		if lfi != len(name)+1 || off+38+liu+lfi > len(img) {
			continue
		}
		if img[off+38+liu] == 8 && string(img[off+38+liu+1:off+38+liu+lfi]) == name {
			offs = append(offs, off)
		}
	}
	return offs
}

func TestDirectoryCycleIsCorrupt(t *testing.T) {
	files := map[string]udfimage.File{"A/B/x.txt": {Data: []byte("x")}}
	for _, l := range layouts {
		t.Run(l.name, func(t *testing.T) {
			img := buildImage(t, files, l.opt)
			as, bs := fidOffsets(img, "A"), fidOffsets(img, "B")
			if len(as) == 0 || len(bs) == 0 {
				t.Fatalf("FIDs not found: A=%v B=%v", as, bs)
			}
			icb := append([]byte(nil), img[as[0]+20:as[0]+36]...)
			for _, b := range bs {
				copy(img[b+20:], icb) // B now points at A's file entry
				fixTag(img[b:])
			}
			fsys := openImage(t, img)

			type result struct{ errs map[string]error }
			done := make(chan result, 1)
			go func() {
				r := result{errs: map[string]error{}}
				_ = fs.WalkDir(fsys, ".", func(p string, _ fs.DirEntry, err error) error {
					if err != nil {
						r.errs[p] = err
					}
					return nil
				})
				done <- r
			}()
			select {
			case r := <-done:
				if err := r.errs["A/B"]; !errors.Is(err, udf.ErrCorrupt) {
					t.Errorf("walk error at A/B = %v, want ErrCorrupt (all: %v)", err, r.errs)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("fs.WalkDir did not terminate on a directory cycle")
			}
			if _, err := fsys.Open("A/B/B/B"); !errors.Is(err, udf.ErrCorrupt) {
				t.Errorf("Open through cycle = %v, want ErrCorrupt", err)
			}
		})
	}
}
