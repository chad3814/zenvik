package udf

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// A Patch replaces len(Old) bytes at byte offset Off of the image with New.
type Patch struct {
	Off      int64
	Old, New []byte
	Dir      string // the directory holding the entry, slash-separated ("." for the root)
}

// ErrEmbeddedDir: a directory stored inside its file entry can't be patched
// without rewriting that entry, which PaddingCRCFixes doesn't do.
var ErrEmbeddedDir = errors.New("udf: directory is embedded in its file entry")

// PaddingCRCFixes returns one Patch for each file identifier descriptor whose
// descriptor CRC length leaves out its padding (ECMA-167 4/14.4: the padding
// is part of the descriptor). Linux's UDF driver rejects such directories.
// Each patch rewrites only the 16-byte tag: the CRC length set to padded
// length − 16, the CRC recomputed over that length, and the tag checksum
// recomputed. A tag that crosses an extent boundary yields one patch per
// contiguous run of image bytes. It reads every directory reachable from
// the root, including deleted entries' tags, and never writes. A nil slice
// means nothing needs fixing.
func (f *FS) PaddingCRCFixes() ([]Patch, error) {
	type dir struct {
		e    *entry
		path string
	}
	var out []Patch
	seen := map[entryAddr]bool{f.root.addr: true}
	queue := []dir{{f.root, "."}}
	for len(queue) > 0 {
		d := queue[0]
		queue = queue[1:]
		if d.e.size > maxDirSize {
			return nil, fmt.Errorf("%w: directory %s of %d bytes", ErrCorrupt, d.path, d.e.size)
		}
		data := make([]byte, d.e.size)
		if _, err := d.e.ReadAt(data, 0); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		for off := 0; off < len(data) && !allZero(data[off:]); {
			rest := data[off:]
			if len(rest) < 38 {
				return nil, fmt.Errorf("%w: truncated file identifier in %s", ErrCorrupt, d.path)
			}
			lfi, liu := int(rest[19]), int(le16(rest[36:]))
			used := 38 + liu + lfi
			if used > len(rest) {
				return nil, fmt.Errorf("%w: file identifier overruns directory %s", ErrCorrupt, d.path)
			}
			padded := (used + 3) &^ 3
			total := min(padded, len(rest))
			fid := rest[:total]
			if _, err := expectTag(fid, anyLocation, tagFID); err != nil {
				return nil, fmt.Errorf("directory %s: %w", d.path, err)
			}
			if total == padded && used != padded && int(le16(fid[10:])) == used-16 {
				if d.e.inline {
					return nil, fmt.Errorf("%w: %s", ErrEmbeddedDir, d.path)
				}
				ps, err := f.tagPatches(d.e, int64(off), fid, d.path)
				if err != nil {
					return nil, err
				}
				out = append(out, ps...)
			}
			chars := fid[18]
			if chars&fidDirectory != 0 && chars&(fidParent|fidDeleted) == 0 {
				name, err := decodeDchars(fid[38+liu : used])
				if err != nil {
					return nil, fmt.Errorf("directory %s: %w", d.path, err)
				}
				icb := parseLongAD(fid[20:36])
				child, err := f.readEntry(icb.ref, icb.block)
				if err != nil {
					return nil, err
				}
				if child.fileType == fileTypeDirectory && !seen[child.addr] {
					seen[child.addr] = true
					p := name
					if d.path != "." {
						p = d.path + "/" + name
					}
					queue = append(queue, dir{child, p})
				}
			}
			off += total
		}
	}
	return out, nil
}

// tagPatches returns the patches that correct the tag of fid, which starts
// dirOff bytes into directory e's data. fid is the whole padded descriptor.
func (f *FS) tagPatches(e *entry, dirOff int64, fid []byte, path string) ([]Patch, error) {
	fixed := make([]byte, 16)
	copy(fixed, fid[:16])
	binary.LittleEndian.PutUint16(fixed[10:], uint16(len(fid)-16))
	binary.LittleEndian.PutUint16(fixed[8:], crc16(fid[16:]))
	var sum byte
	for i := 0; i < 16; i++ {
		if i != 4 {
			sum += fixed[i]
		}
	}
	fixed[4] = sum

	var out []Patch
	for i := 0; i < 16; i++ {
		img, err := f.entryImageOffset(e, dirOff+int64(i))
		if err != nil {
			return nil, err
		}
		if n := len(out); n > 0 && out[n-1].Off+int64(len(out[n-1].Old)) == img {
			out[n-1].Old = append(out[n-1].Old, fid[i])
			out[n-1].New = append(out[n-1].New, fixed[i])
			continue
		}
		out = append(out, Patch{Off: img, Old: []byte{fid[i]}, New: []byte{fixed[i]}, Dir: path})
	}
	return out, nil
}

// entryImageOffset maps byte off of entry e's data to a byte offset in the
// image.
func (f *FS) entryImageOffset(e *entry, off int64) (int64, error) {
	var base int64
	for _, x := range e.extents {
		l := int64(x.length)
		if off < base+l {
			if x.typ != 0 {
				return 0, fmt.Errorf("%w: directory data in an unrecorded extent", ErrCorrupt)
			}
			return f.imageOffset(x.ref, x.block, off-base)
		}
		base += l
	}
	return 0, fmt.Errorf("%w: offset %d past the entry's extents", ErrCorrupt, off)
}

// imageOffset maps byte off of logical block block in partition reference
// ref to a byte offset in the image, through the metadata file for a
// metadata partition. All arithmetic is int64: images exceed 4 GiB.
func (f *FS) imageOffset(ref uint16, block uint32, off int64) (int64, error) {
	if int(ref) >= len(f.parts) {
		return 0, fmt.Errorf("%w: partition reference %d", ErrCorrupt, ref)
	}
	part := &f.parts[ref]
	pos := int64(block)*sectorSize + off
	if part.isMeta {
		if part.meta == nil {
			return 0, fmt.Errorf("%w: metadata partition used before it is loaded", ErrCorrupt)
		}
		return f.entryImageOffset(part.meta, pos)
	}
	if pos < 0 || pos >= int64(part.length)*sectorSize {
		return 0, fmt.Errorf("%w: offset past the end of partition %d", ErrCorrupt, ref)
	}
	return int64(part.start)*sectorSize + pos, nil
}
