package udf

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

type partition struct {
	number    uint16 // partition number (type 1 maps; target partition for metadata maps)
	start     uint32 // first sector (type 1)
	length    uint32 // length in blocks (type 1)
	isMeta    bool
	metaLoc   uint32 // metadata file ICB block in the physical partition
	mirrorLoc uint32 // metadata mirror file ICB block
	meta      *entry // loaded metadata file
}

// FS is a read-only UDF file system. It implements io/fs.FS; files also
// implement io.ReaderAt and io.Seeker. An FS is safe for concurrent use if
// the underlying io.ReaderAt is.
type FS struct {
	r     io.ReaderAt
	size  int64
	label string
	parts []partition // indexed by partition reference number
	root  *entry
}

type extentAD struct{ length, loc uint32 }

type longAD struct {
	length uint32
	typ    uint8
	block  uint32
	ref    uint16
}

func parseLongAD(b []byte) longAD {
	raw := le32(b)
	return longAD{length: raw & 0x3FFFFFFF, typ: uint8(raw >> 30), block: le32(b[4:]), ref: le16(b[8:])}
}

// Open reads the UDF volume in r, an image of size bytes.
func Open(r io.ReaderAt, size int64) (*FS, error) {
	f := &FS{r: r, size: size}
	if err := f.checkVRS(); err != nil {
		return nil, err
	}
	main, reserve, err := f.findAnchor()
	if err != nil {
		return nil, err
	}
	if err := f.readVDS(main); err != nil {
		if err2 := f.readVDS(reserve); err2 != nil {
			return nil, err
		}
	}
	return f, nil
}

// Label returns the logical volume identifier, or the primary volume
// identifier if that is empty. It is "" if neither can be decoded.
func (f *FS) Label() string { return f.label }

func readFull(r io.ReaderAt, p []byte, off int64) error {
	n, err := r.ReadAt(p, off)
	if n == len(p) {
		return nil
	}
	if err == nil || errors.Is(err, io.EOF) {
		err = io.ErrUnexpectedEOF
	}
	return fmt.Errorf("udf: read %d bytes at offset %d: %w", len(p), off, err)
}

func (f *FS) sector(n uint32) ([]byte, error) {
	d := make([]byte, sectorSize)
	return d, readFull(f.r, d, int64(n)*sectorSize)
}

// checkVRS looks for an NSR descriptor in the volume recognition sequence.
func (f *FS) checkVRS() error {
scan:
	for s := uint32(16); s < 16+64; s++ {
		d, err := f.sector(s)
		if err != nil {
			break
		}
		switch string(d[1:6]) {
		case "NSR02", "NSR03":
			return nil
		case "BEA01", "TEA01", "CD001", "CDW02", "BOOT2":
		default:
			break scan
		}
	}
	return ErrNotUDF
}

func (f *FS) findAnchor() (main, reserve extentAD, err error) {
	candidates := []uint32{256}
	if n := f.size / sectorSize; n > 257 {
		candidates = append(candidates, uint32(n-1), uint32(n-257))
	}
	for _, loc := range candidates {
		d, err := f.sector(loc)
		if err != nil {
			continue
		}
		if _, err := expectTag(d, loc, tagAVDP); err != nil {
			continue
		}
		return extentAD{le32(d[16:]), le32(d[20:])}, extentAD{le32(d[24:]), le32(d[28:])}, nil
	}
	return extentAD{}, extentAD{}, fmt.Errorf("%w: no anchor volume descriptor pointer", ErrNotUDF)
}

// readVDS reads one volume descriptor sequence and, if it is complete,
// mounts the logical volume it describes.
func (f *FS) readVDS(e extentAD) error {
	var pvd, lvd []byte
	pds := map[uint16][]byte{}
	loc, end := e.loc, uint64(e.loc)+uint64(e.length/sectorSize)
	for hops := 0; uint64(loc) < end; loc++ {
		d, err := f.sector(loc)
		if err != nil {
			return err
		}
		id, err := parseTag(d, loc)
		if err != nil || id == tagTD {
			break // a terminating descriptor or an unrecorded sector ends the sequence
		}
		switch id {
		case tagPVD:
			if pvd == nil || le32(d[16:]) >= le32(pvd[16:]) {
				pvd = d
			}
		case tagLVD:
			if lvd == nil || le32(d[16:]) >= le32(lvd[16:]) {
				lvd = d
			}
		case tagPD:
			num := le16(d[22:])
			if old, ok := pds[num]; !ok || le32(d[16:]) >= le32(old[16:]) {
				pds[num] = d
			}
		case tagVDP:
			if hops++; hops > 16 {
				return fmt.Errorf("%w: volume descriptor pointer loop", ErrCorrupt)
			}
			next := extentAD{length: le32(d[20:]), loc: le32(d[24:])}
			loc, end = next.loc-1, uint64(next.loc)+uint64(next.length/sectorSize)
		}
	}
	if lvd == nil || len(pds) == 0 {
		return fmt.Errorf("%w: incomplete volume descriptor sequence", ErrCorrupt)
	}
	return f.mount(pvd, lvd, pds)
}

func (f *FS) mount(pvd, lvd []byte, pds map[uint16][]byte) error {
	if bs := le32(lvd[212:]); bs != sectorSize {
		return fmt.Errorf("%w: logical block size %d", ErrUnsupported, bs)
	}
	label := volumeLabel(pvd, lvd)
	parts, err := parsePartitionMaps(lvd, pds)
	if err != nil {
		return err
	}
	f.parts = parts
	for i := range f.parts {
		if err := f.loadMetadata(&f.parts[i]); err != nil {
			return err
		}
	}

	fsdAD := parseLongAD(lvd[248:264])
	d := make([]byte, sectorSize)
	if err := f.readAt(fsdAD.ref, fsdAD.block, 0, d); err != nil {
		return err
	}
	if _, err := expectTag(d, fsdAD.block, tagFSD); err != nil {
		return err
	}
	rootAD := parseLongAD(d[400:416])
	root, err := f.readEntry(rootAD.ref, rootAD.block)
	if err != nil {
		return err
	}
	if root.fileType != fileTypeDirectory {
		return fmt.Errorf("%w: root is not a directory", ErrCorrupt)
	}
	f.root, f.label = root, label
	return nil
}

// volumeLabel returns the logical volume identifier, falling back to the
// primary volume identifier when it is empty. An undecodable identifier
// yields "": the label is informational and must not prevent opening.
func volumeLabel(pvd, lvd []byte) string {
	label, err := decodeDstring(lvd[84:212])
	if err != nil {
		return ""
	}
	if label == "" && pvd != nil {
		if label, err = decodeDstring(pvd[24:56]); err != nil {
			return ""
		}
	}
	return label
}

func parsePartitionMaps(lvd []byte, pds map[uint16][]byte) ([]partition, error) {
	tableLen := int64(le32(lvd[264:]))
	if 440+tableLen > int64(len(lvd)) {
		return nil, fmt.Errorf("%w: partition map table length %d", ErrCorrupt, tableLen)
	}
	table := lvd[440 : 440+tableLen]
	var parts []partition
	for i := uint32(0); i < le32(lvd[268:]); i++ {
		if len(table) < 2 || table[1] < 2 || int(table[1]) > len(table) {
			return nil, fmt.Errorf("%w: partition map %d", ErrCorrupt, i)
		}
		m := table[:table[1]]
		table = table[table[1]:]
		switch m[0] {
		case 1:
			if len(m) < 6 {
				return nil, fmt.Errorf("%w: short type 1 partition map", ErrCorrupt)
			}
			num := le16(m[4:])
			pd, ok := pds[num]
			if !ok {
				return nil, fmt.Errorf("%w: no partition descriptor for partition %d", ErrCorrupt, num)
			}
			parts = append(parts, partition{number: num, start: le32(pd[188:]), length: le32(pd[192:])})
		case 2:
			if len(m) < 64 {
				return nil, fmt.Errorf("%w: short type 2 partition map", ErrCorrupt)
			}
			if id := strings.TrimRight(string(m[5:28]), "\x00"); id != "*UDF Metadata Partition" {
				return nil, fmt.Errorf("%w: partition map %q", ErrUnsupported, id)
			}
			parts = append(parts, partition{
				number: le16(m[38:]), isMeta: true, metaLoc: le32(m[40:]), mirrorLoc: le32(m[44:]),
			})
		default:
			return nil, fmt.Errorf("%w: partition map type %d", ErrCorrupt, m[0])
		}
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("%w: no partition maps", ErrCorrupt)
	}
	return parts, nil
}

// loadMetadata reads the metadata file (or its mirror) backing a metadata
// partition. The file's extents must lie in the physical partition, which
// also rules out a metadata partition that refers to itself.
func (f *FS) loadMetadata(p *partition) error {
	if !p.isMeta {
		return nil
	}
	phys := -1
	for j, q := range f.parts {
		if !q.isMeta && q.number == p.number {
			phys = j
			break
		}
	}
	if phys < 0 {
		return fmt.Errorf("%w: metadata partition %d has no physical partition", ErrCorrupt, p.number)
	}
	var firstErr error
	for _, loc := range []uint32{p.metaLoc, p.mirrorLoc} {
		e, err := f.readEntry(uint16(phys), loc)
		if err == nil && e.fileType != fileTypeMetadata && e.fileType != fileTypeMetadataMirror {
			err = fmt.Errorf("%w: metadata file has file type %d", ErrCorrupt, e.fileType)
		}
		if err == nil {
			for _, x := range e.extents {
				if x.ref != uint16(phys) {
					err = fmt.Errorf("%w: metadata file extent outside physical partition", ErrCorrupt)
					break
				}
			}
		}
		if err == nil {
			p.meta = e
			return nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// readAt fills p from partition ref, starting off bytes after the start
// of logical block block.
func (f *FS) readAt(ref uint16, block uint32, off int64, p []byte) error {
	if int(ref) >= len(f.parts) {
		return fmt.Errorf("%w: partition reference %d", ErrCorrupt, ref)
	}
	part := &f.parts[ref]
	start := int64(block)*sectorSize + off
	if part.isMeta {
		if part.meta == nil {
			return fmt.Errorf("%w: metadata partition used before it is loaded", ErrCorrupt)
		}
		n, err := part.meta.ReadAt(p, start)
		if n == len(p) {
			return nil
		}
		if err == nil || errors.Is(err, io.EOF) {
			err = fmt.Errorf("%w: read past end of metadata partition", ErrCorrupt)
		}
		return err
	}
	if start < 0 || start+int64(len(p)) > int64(part.length)*sectorSize {
		return fmt.Errorf("%w: read past end of partition %d", ErrCorrupt, ref)
	}
	return readFull(f.r, p, int64(part.start)*sectorSize+start)
}
