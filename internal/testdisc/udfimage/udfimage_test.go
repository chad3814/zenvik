package udfimage

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
)

func sectorAt(img []byte, n int) []byte { return img[n*sector : (n+1)*sector] }

func checkTag(t *testing.T, d []byte, wantID uint16, wantLoc uint32) {
	t.Helper()
	var sum byte
	for i := 0; i < 16; i++ {
		if i != 4 {
			sum += d[i]
		}
	}
	if sum != d[4] {
		t.Errorf("tag checksum %#x, want %#x", d[4], sum)
	}
	if id := binary.LittleEndian.Uint16(d); id != wantID {
		t.Errorf("tag id %d, want %d", id, wantID)
	}
	if loc := binary.LittleEndian.Uint32(d[12:]); loc != wantLoc {
		t.Errorf("tag location %d, want %d", loc, wantLoc)
	}
	n := int(binary.LittleEndian.Uint16(d[10:]))
	if got, want := crc16(d[16:16+n]), binary.LittleEndian.Uint16(d[8:]); got != want {
		t.Errorf("tag CRC %#x, want %#x", got, want)
	}
}

func TestCRC16KnownVector(t *testing.T) {
	// CRC-16/XMODEM (poly 0x1021, init 0) of "123456789".
	if got := crc16([]byte("123456789")); got != 0x31C3 {
		t.Errorf("crc16 = %#x, want 0x31C3", got)
	}
}

func TestBuildStructure(t *testing.T) {
	files := map[string]File{"BDMV/index.bdmv": {Data: []byte("INDX0200")}}
	for _, tc := range []struct {
		rev uint16
		nsr string
	}{{0x0102, "NSR02"}, {0x0250, "NSR03"}} {
		img, err := Build(files, Options{Revision: tc.rev, Label: "TEST"})
		if err != nil {
			t.Fatal(err)
		}
		if len(img)%sector != 0 {
			t.Fatalf("image length %d not a multiple of %d", len(img), sector)
		}
		for i, id := range []string{"BEA01", tc.nsr, "TEA01"} {
			if got := string(sectorAt(img, 16+i)[1:6]); got != id {
				t.Errorf("rev %#x: VRS sector %d = %q, want %q", tc.rev, 16+i, got, id)
			}
		}
		last := len(img)/sector - 1
		checkTag(t, sectorAt(img, 256), 2, 256)
		checkTag(t, sectorAt(img, last), 2, uint32(last))
		checkTag(t, sectorAt(img, 32), 1, 32) // PVD
		checkTag(t, sectorAt(img, 35), 6, 35) // LVD
		checkTag(t, sectorAt(img, 37), 8, 37) // TD
		checkTag(t, sectorAt(img, 64), 9, 64) // LVID
		if got := string(sectorAt(img, 32)[25:29]); got != "TEST" {
			t.Errorf("PVD volume identifier = %q", got)
		}
	}
}

func TestBuildRejectsBadInput(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]File
		opt   Options
		want  string
	}{
		{"revision", map[string]File{"a": {}}, Options{Revision: 0x0201}, "unsupported revision"},
		{"conflict", map[string]File{"a": {}, "a/b": {}}, Options{Revision: 0x0102}, "path conflict"},
		{"invalid path", map[string]File{"/abs": {}}, Options{Revision: 0x0102}, "invalid path"},
		{"inline ADs", map[string]File{"a": {}}, Options{Revision: 0x0102, MaxInlineADs: 1}, "MaxInlineADs"},
	}
	for _, tt := range tests {
		_, err := Build(tt.files, tt.opt)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want containing %q", tt.name, err, tt.want)
		}
	}
}

type adRec struct {
	typ    uint32
	length uint32
	loc    uint32
}

func parseADs(d []byte, size int) []adRec {
	var out []adRec
	for off := 0; off+size <= len(d); off += size {
		v := binary.LittleEndian.Uint32(d[off:])
		out = append(out, adRec{v >> 30, v & 0x3FFFFFFF, binary.LittleEndian.Uint32(d[off+4:])})
	}
	return out
}

// fileEntry is the parsed subset of a file entry or extended file entry.
type fileEntry struct {
	flags    uint16
	info     uint64
	recorded uint64
	ads      []byte
}

func parseEntry(d []byte, meta bool) fileEntry {
	hdr, recOff, lenOff := 176, 64, 172
	if meta {
		hdr, recOff, lenOff = 216, 72, 212
	}
	n := int(binary.LittleEndian.Uint32(d[lenOff:]))
	return fileEntry{
		flags:    binary.LittleEndian.Uint16(d[16+18:]),
		info:     binary.LittleEndian.Uint64(d[56:]),
		recorded: binary.LittleEndian.Uint64(d[recOff:]),
		ads:      d[hdr : hdr+n],
	}
}

type dirChild struct {
	name string
	loc  uint32
}

// walkDir checks every FID of a directory and returns its children.
func walkDir(t *testing.T, img []byte, feBase int, e fileEntry) []dirChild {
	t.Helper()
	ad := parseADs(e.ads, 8)[0] // directories always use short ADs
	data := img[(feBase+int(ad.loc))*sector:]
	data = data[:ad.length]
	var out []dirChild
	for off := 0; off < len(data); {
		d := data[off:]
		checkTag(t, d, 257, ad.loc+uint32(off/sector))
		nameLen, iuLen := int(d[19]), int(binary.LittleEndian.Uint16(d[36:]))
		if d[18]&0x08 == 0 {
			name := string(d[38+iuLen+1 : 38+iuLen+nameLen])
			out = append(out, dirChild{name, binary.LittleEndian.Uint32(d[24:])})
		}
		off += (38 + iuLen + nameLen + 3) &^ 3
	}
	return out
}

func TestAllDescriptorsValid(t *testing.T) {
	files := map[string]File{
		"big.bin":   {SparseSize: 3 << 30},
		"small.txt": {Data: []byte("hello")},
	}
	for i := 0; i < 60; i++ {
		files[fmt.Sprintf("d/%02d_%s", i, strings.Repeat("n", 27))] = File{}
	}
	for _, rev := range []uint16{0x0102, 0x0250} {
		t.Run(fmt.Sprintf("%#04x", rev), func(t *testing.T) {
			meta := rev == 0x0250
			img, err := Build(files, Options{Revision: rev, Label: "TEST", Embed: true, MaxInlineADs: 2})
			if err != nil {
				t.Fatal(err)
			}
			feBase, adSize, entryID, fileADType := partStart, 8, uint16(261), uint16(0)
			if meta {
				feBase, adSize, entryID, fileADType = partStart+2, 16, 266, 1
				// metadata file and mirror file entries live in physical blocks 0 and 1
				checkTag(t, sectorAt(img, partStart), 266, 0)
				checkTag(t, sectorAt(img, partStart+1), 266, 1)
			}
			counts := map[uint16]int{}
			for s := feBase; s < len(img)/sector-1; s++ {
				d := sectorAt(img, s)
				switch id := binary.LittleEndian.Uint16(d); id {
				case 256, 258, entryID:
					counts[id]++
					checkTag(t, d, id, uint32(s-feBase))
				}
			}
			// FSD, root, d, big.bin, small.txt, 60 empty files; one AED.
			if counts[256] != 1 || counts[entryID] != 64 || counts[258] != 1 {
				t.Fatalf("descriptor counts %v", counts)
			}
			fsd := sectorAt(img, feBase)
			rootLoc := binary.LittleEndian.Uint32(fsd[400+4:])
			entryAt := func(loc uint32) fileEntry {
				return parseEntry(sectorAt(img, feBase+int(loc)), meta)
			}
			byName := map[string]dirChild{}
			for _, c := range walkDir(t, img, feBase, entryAt(rootLoc)) {
				byName[c.name] = c
			}

			// A directory spanning several blocks.
			dir := entryAt(byName["d"].loc)
			if dir.info <= sector {
				t.Fatalf("directory is %d bytes, want more than one block", dir.info)
			}
			if kids := walkDir(t, img, feBase, dir); len(kids) != 60 {
				t.Errorf("directory has %d children, want 60", len(kids))
			}

			// A sparse file whose descriptors spill into an AED.
			big := entryAt(byName["big.bin"].loc)
			if big.info != 3<<30 || big.recorded != 0 || big.flags&7 != fileADType {
				t.Errorf("big.bin: info %d recorded %d flags %#x", big.info, big.recorded, big.flags)
			}
			ads := parseADs(big.ads, adSize)
			if len(ads) != 2 || ads[0].typ != 2 || ads[1].typ != 3 {
				t.Fatalf("big.bin inline ADs %+v", ads)
			}
			total := uint64(ads[0].length)
			aed := sectorAt(img, feBase+int(ads[1].loc))
			checkTag(t, aed, 258, ads[1].loc)
			n := int(binary.LittleEndian.Uint32(aed[20:]))
			rest := parseADs(aed[24:24+n], adSize)
			if len(rest) < 2 {
				t.Fatalf("AED holds %d ADs", len(rest))
			}
			for _, x := range rest {
				if x.typ != 2 {
					t.Errorf("AED extent type %d, want 2", x.typ)
				}
				total += uint64(x.length)
			}
			if total != 3<<30 {
				t.Errorf("extents sum to %d, want %d", total, uint64(3<<30))
			}

			// An embedded file.
			small := entryAt(byName["small.txt"].loc)
			if small.flags&7 != 3 || string(small.ads) != "hello" || small.info != 5 {
				t.Errorf("small.txt: flags %#x ads %q info %d", small.flags, small.ads, small.info)
			}
		})
	}
}
