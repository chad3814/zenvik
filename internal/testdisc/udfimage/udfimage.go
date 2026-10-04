// Package udfimage builds small UDF disc images in memory for tests. It
// writes UDF 1.02 (one physical partition, short allocation descriptors)
// and UDF 2.50 (a physical partition plus a metadata partition, with long
// allocation descriptors for file data), the layouts the udf package
// reads. It is written independently of the reader so reader tests do not
// share the reader's assumptions.
package udfimage

import (
	"encoding/binary"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
)

const (
	sector     = 2048
	partStart  = 257        // first sector of the physical partition
	lvidSector = 64         // logical volume integrity descriptor
	maxExtent  = 0x3FFFF800 // largest extent length that is a whole number of sectors
	metaAlign  = 32         // metadata file allocation unit, in blocks
)

var stamp = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// File is the content of one regular file in the image.
type File struct {
	Data []byte
	// SparseSize, when non-zero, makes the file SparseSize zero bytes
	// stored as unallocated extents, so huge files cost no image space.
	SparseSize int64
}

// Options control the image layout.
type Options struct {
	Revision uint16 // 0x0102 or 0x0250
	// Label is written to the logical volume descriptor and, unless
	// PVDLabel is set, to the primary volume descriptor.
	Label string
	// PVDLabel, when non-empty, is written to the primary volume
	// descriptor instead of Label, so the two identifiers can differ.
	PVDLabel string
	// Embed stores each file small enough to fit inside its file entry.
	Embed bool
	// MaxInlineADs, when at least 2, limits a file entry to that many
	// allocation descriptors; the rest go into an allocation extent
	// descriptor.
	MaxInlineADs int
	// UnpaddedFIDCRC writes each named file identifier descriptor's tag
	// CRC length (and CRC) over the descriptor without its padding, the
	// way some authoring tools do. Linux's UDF driver rejects such
	// directories; the parent entry keeps the padded length, as those
	// tools write it.
	UnpaddedFIDCRC bool
}

type node struct {
	name     string
	dir      bool
	file     File
	parent   *node
	children []*node
	uid      uint64
	fe       uint32 // file entry block (fe space)
	dirBlk   uint32 // first directory data block (fe space)
	aed      uint32 // allocation extent descriptor block (fe space)
	hasAED   bool
	embedded bool
	data     uint32 // first data block (physical partition)
}

type ext struct {
	typ    uint8 // 0 recorded, 2 not recorded and not allocated, 3 continuation
	length uint32
	loc    uint32
}

// builder lays out two block spaces. "fe space" holds the file set
// descriptor, file entries, directories and AEDs: the physical partition
// for UDF 1.02, the metadata partition for UDF 2.50. File data always
// lives in the physical partition.
type builder struct {
	opt      Options
	meta     bool
	feRef    uint16 // partition reference number of fe space
	feSpace  map[uint32][]byte
	phys     map[uint32][]byte
	feNext   uint32
	metaLen  uint32 // metadata file length in blocks (UDF 2.50)
	physNext uint32
	nextUID  uint64
	files    uint32
	dirs     uint32
}

// Build returns a UDF image containing files, keyed by slash-separated
// paths such as "BDMV/index.bdmv". Directories are created as needed.
func Build(files map[string]File, opt Options) ([]byte, error) {
	if opt.Revision != 0x0102 && opt.Revision != 0x0250 {
		return nil, fmt.Errorf("udfimage: unsupported revision %#04x", opt.Revision)
	}
	if opt.MaxInlineADs == 1 {
		return nil, fmt.Errorf("udfimage: MaxInlineADs must be 0 or at least 2")
	}
	root, err := buildTree(files)
	if err != nil {
		return nil, err
	}
	b := &builder{
		opt:     opt,
		meta:    opt.Revision >= 0x0250,
		feSpace: map[uint32][]byte{},
		phys:    map[uint32][]byte{},
		feNext:  1, // block 0 holds the file set descriptor
		nextUID: 16,
	}
	if b.meta {
		b.feRef = 1
	}
	b.layout(root)
	if b.meta {
		b.metaLen = (b.feNext + metaAlign - 1) / metaAlign * metaAlign
		b.physNext = 2 + b.metaLen
	} else {
		b.physNext = b.feNext
	}
	b.assignData(root)
	b.emit(root)
	b.feSpace[0] = b.fsd(root)
	return b.image(), nil
}

func buildTree(files map[string]File) (*node, error) {
	root := &node{dir: true}
	root.parent = root
	for p, f := range files {
		if p == "." || !fs.ValidPath(p) {
			return nil, fmt.Errorf("udfimage: invalid path %q", p)
		}
		parts := strings.Split(p, "/")
		cur := root
		for i, part := range parts {
			last := i == len(parts)-1
			var child *node
			for _, c := range cur.children {
				if c.name == part {
					child = c
					break
				}
			}
			switch {
			case child == nil:
				child = &node{name: part, dir: !last, parent: cur}
				if last {
					child.file = f
				}
				cur.children = append(cur.children, child)
			case last || !child.dir:
				return nil, fmt.Errorf("udfimage: path conflict at %q", p)
			}
			cur = child
		}
	}
	sortTree(root)
	return root, nil
}

func sortTree(n *node) {
	sort.Slice(n.children, func(i, j int) bool { return n.children[i].name < n.children[j].name })
	for _, c := range n.children {
		sortTree(c)
	}
}

func blocks(n int64) uint32 { return uint32((n + sector - 1) / sector) }

func fidLen(nameLen int) int { return (38 + nameLen + 3) &^ 3 }

func dirLen(n *node) int {
	l := fidLen(0) // parent entry
	for _, c := range n.children {
		l += fidLen(len(encodeName(c.name)))
	}
	return l
}

func (b *builder) entryHeader() int {
	if b.meta {
		return 216 // extended file entry
	}
	return 176 // file entry
}

// layout assigns fe-space blocks in depth-first order.
func (b *builder) layout(n *node) {
	n.fe = b.feNext
	b.feNext++
	if n != n.parent {
		n.uid = b.nextUID
		b.nextUID++
	}
	if n.dir {
		b.dirs++
		n.dirBlk = b.feNext
		b.feNext += blocks(int64(dirLen(n)))
		for _, c := range n.children {
			b.layout(c)
		}
		return
	}
	b.files++
	n.embedded = b.opt.Embed && n.file.SparseSize == 0 && len(n.file.Data) <= sector-b.entryHeader()
	if !n.embedded && b.opt.MaxInlineADs >= 2 && len(b.extents(n)) > b.opt.MaxInlineADs {
		n.hasAED = true
		n.aed = b.feNext
		b.feNext++
	}
}

func (b *builder) extents(n *node) []ext {
	if n.file.SparseSize > 0 {
		var out []ext
		for left := n.file.SparseSize; left > 0; left -= maxExtent {
			out = append(out, ext{typ: 2, length: uint32(min(left, maxExtent))})
		}
		return out
	}
	if len(n.file.Data) == 0 {
		return nil
	}
	return []ext{{typ: 0, length: uint32(len(n.file.Data)), loc: n.data}}
}

func (b *builder) assignData(n *node) {
	if n.dir {
		for _, c := range n.children {
			b.assignData(c)
		}
		return
	}
	if n.embedded || n.file.SparseSize > 0 || len(n.file.Data) == 0 {
		return
	}
	n.data = b.physNext
	b.physNext += blocks(int64(len(n.file.Data)))
	for off := 0; off < len(n.file.Data); off += sector {
		b.phys[n.data+uint32(off/sector)] = n.file.Data[off:min(off+sector, len(n.file.Data))]
	}
}

type entryParams struct {
	loc      uint32
	fileType uint8
	adType   uint16
	ads      []byte
	size     uint64
	recorded uint64
	uid      uint64
	links    uint16
}

func (b *builder) emit(n *node) {
	e := entryParams{loc: n.fe, uid: n.uid, links: 1, fileType: 5}
	switch {
	case n.dir:
		data := b.dirData(n)
		for off := 0; off < len(data); off += sector {
			b.feSpace[n.dirBlk+uint32(off/sector)] = data[off:min(off+sector, len(data))]
		}
		e.fileType = 4
		e.size = uint64(len(data))
		e.recorded = uint64(blocks(int64(len(data))))
		e.ads = shortAD(0, uint32(len(data)), n.dirBlk)
		for _, c := range n.children {
			if c.dir {
				e.links++
			}
			b.emit(c)
		}
	case n.embedded:
		e.size = uint64(len(n.file.Data))
		e.adType = 3
		e.ads = n.file.Data
	default:
		exts := b.extents(n)
		e.size = uint64(len(n.file.Data))
		if n.file.SparseSize > 0 {
			e.size = uint64(n.file.SparseSize)
		}
		for _, x := range exts {
			if x.typ == 0 {
				e.recorded += uint64(blocks(int64(x.length)))
			}
		}
		e.adType, e.ads = b.fileADs(n, exts)
	}
	b.feSpace[n.fe] = b.entry(e)
}

// fileADs encodes a file's data extents: short ADs in the same partition
// for UDF 1.02, long ADs pointing at partition 0 for UDF 2.50.
func (b *builder) fileADs(n *node, exts []ext) (uint16, []byte) {
	enc := func(x ext) []byte {
		if b.meta {
			return longAD(x.typ, x.length, x.loc, 0)
		}
		return shortAD(x.typ, x.length, x.loc)
	}
	adType := uint16(0)
	if b.meta {
		adType = 1
	}
	inline, rest := exts, []ext(nil)
	if n.hasAED {
		k := b.opt.MaxInlineADs - 1 // the last inline slot is the continuation
		inline, rest = exts[:k], exts[k:]
	}
	var out []byte
	for _, x := range inline {
		out = append(out, enc(x)...)
	}
	if n.hasAED {
		var more []byte
		for _, x := range rest {
			more = append(more, enc(x)...)
		}
		d := make([]byte, 24+len(more))
		le32(d[20:], uint32(len(more)))
		copy(d[24:], more)
		b.feSpace[n.aed] = b.tag(258, n.aed, d)
		if b.meta {
			out = append(out, longAD(3, sector, n.aed, b.feRef)...)
		} else {
			out = append(out, shortAD(3, sector, n.aed)...)
		}
	}
	return adType, out
}

func (b *builder) dirData(n *node) []byte {
	var out []byte
	add := func(chars uint8, name []byte, target *node) {
		blk := n.dirBlk + uint32(len(out)/sector)
		out = append(out, b.fid(blk, chars, name, target.fe)...)
	}
	add(0x0A, nil, n.parent) // directory | parent
	for _, c := range n.children {
		var chars uint8
		if c.dir {
			chars = 0x02
		}
		add(chars, encodeName(c.name), c)
	}
	return out
}

func (b *builder) fid(blk uint32, chars uint8, name []byte, icb uint32) []byte {
	d := make([]byte, fidLen(len(name)))
	le16(d[16:], 1) // file version number
	d[18] = chars
	d[19] = uint8(len(name))
	copy(d[20:], longAD(0, sector, icb, b.feRef))
	copy(d[38:], name)
	d = b.tag(257, blk, d)
	if used := 38 + len(name); b.opt.UnpaddedFIDCRC && len(name) > 0 && used < len(d) {
		le16(d[10:], uint16(used-16))
		le16(d[8:], crc16(d[16:used]))
		d[4] = tagChecksum(d)
	}
	return d
}

func (b *builder) entry(e entryParams) []byte {
	hdr := b.entryHeader()
	d := make([]byte, hdr+len(e.ads))
	le16(d[16+4:], 4) // ICB strategy type 4
	le16(d[16+8:], 1) // maximum number of entries
	d[16+11] = e.fileType
	le16(d[16+18:], e.adType)
	le32(d[36:], 0xFFFFFFFF) // uid
	le32(d[40:], 0xFFFFFFFF) // gid
	le32(d[44:], 0x14A5)     // read and execute for owner, group, other
	le16(d[48:], e.links)
	le64(d[56:], e.size)
	if b.meta {
		le64(d[64:], e.size) // object size
		le64(d[72:], e.recorded)
		for _, off := range []int{80, 92, 104, 116} {
			timestamp(d[off:])
		}
		le32(d[128:], 1) // checkpoint
		regid(d[168:], "*zenvik", nil)
		le64(d[200:], e.uid)
		le32(d[212:], uint32(len(e.ads)))
	} else {
		le64(d[64:], e.recorded)
		for _, off := range []int{72, 84, 96} {
			timestamp(d[off:])
		}
		le32(d[108:], 1)
		regid(d[128:], "*zenvik", nil)
		le64(d[160:], e.uid)
		le32(d[172:], uint32(len(e.ads)))
	}
	copy(d[hdr:], e.ads)
	id := uint16(261)
	if b.meta {
		id = 266
	}
	return b.tag(id, e.loc, d)
}

func (b *builder) fsd(root *node) []byte {
	d := make([]byte, 512)
	timestamp(d[16:])
	le16(d[28:], 3) // interchange level
	le16(d[30:], 3)
	le32(d[32:], 1) // character set list
	le32(d[36:], 1)
	charspec(d[48:])
	dstring(d[112:240], b.opt.Label)
	charspec(d[240:])
	dstring(d[304:336], b.opt.Label)
	copy(d[400:], longAD(0, sector, root.fe, b.feRef))
	b.domain(d[416:])
	return b.tag(256, 0, d)
}

func (b *builder) vds(start, partLen uint32) [][]byte {
	pvd := make([]byte, 512)
	le32(pvd[16:], 1)
	pvdLabel := b.opt.PVDLabel
	if pvdLabel == "" {
		pvdLabel = b.opt.Label
	}
	dstring(pvd[24:56], pvdLabel)
	le16(pvd[56:], 1)
	le16(pvd[58:], 1)
	le16(pvd[60:], 2)
	le16(pvd[62:], 2)
	le32(pvd[64:], 1)
	le32(pvd[68:], 1)
	dstring(pvd[72:200], b.opt.Label)
	charspec(pvd[200:])
	charspec(pvd[264:])
	timestamp(pvd[376:])
	regid(pvd[388:], "*zenvik", nil)

	iuvd := make([]byte, 512)
	le32(iuvd[16:], 2)
	b.udfRegid(iuvd[20:], "*UDF LV Info")
	charspec(iuvd[52:])
	dstring(iuvd[116:244], b.opt.Label)
	regid(iuvd[352:], "*zenvik", nil)

	pd := make([]byte, 512)
	le32(pd[16:], 3)
	le16(pd[20:], 1) // allocated
	nsr := "+NSR02"
	if b.meta {
		nsr = "+NSR03"
	}
	regid(pd[24:], nsr, nil)
	le32(pd[184:], 1) // read-only access
	le32(pd[188:], partStart)
	le32(pd[192:], partLen)
	regid(pd[196:], "*zenvik", nil)

	maps := []byte{1, 6, 1, 0, 0, 0} // type 1: volume sequence 1, partition 0
	nmaps := uint32(1)
	if b.meta {
		m := make([]byte, 64)
		m[0], m[1] = 2, 64
		b.udfRegid(m[4:], "*UDF Metadata Partition")
		le16(m[36:], 1)          // volume sequence number
		le16(m[38:], 0)          // partition number
		le32(m[40:], 0)          // metadata file location
		le32(m[44:], 1)          // metadata mirror file location
		le32(m[48:], 0xFFFFFFFF) // no metadata bitmap file
		le32(m[52:], metaAlign)  // allocation unit size
		le16(m[56:], 1)          // alignment unit size
		maps = append(maps, m...)
		nmaps = 2
	}
	lvd := make([]byte, 440+len(maps))
	le32(lvd[16:], 4)
	charspec(lvd[20:])
	dstring(lvd[84:212], b.opt.Label)
	le32(lvd[212:], sector)
	b.domain(lvd[216:])
	copy(lvd[248:], longAD(0, sector, 0, b.feRef)) // file set descriptor
	le32(lvd[264:], uint32(len(maps)))
	le32(lvd[268:], nmaps)
	regid(lvd[272:], "*zenvik", nil)
	le32(lvd[432:], sector) // integrity sequence extent
	le32(lvd[436:], lvidSector)
	copy(lvd[440:], maps)

	usd := make([]byte, 24)
	le32(usd[16:], 5)
	td := make([]byte, 512)

	out := [][]byte{pvd, iuvd, pd, lvd, usd, td}
	ids := []uint16{1, 4, 5, 6, 7, 8}
	for i := range out {
		out[i] = b.tag(ids[i], start+uint32(i), out[i])
	}
	return out
}

func (b *builder) lvid(partLen uint32) []byte {
	n := 1
	if b.meta {
		n = 2
	}
	d := make([]byte, 80+8*n+46)
	timestamp(d[16:])
	le32(d[28:], 1) // closed
	le64(d[40:], b.nextUID)
	le32(d[72:], uint32(n))
	le32(d[76:], 46)
	le32(d[80+4*n:], partLen) // size table follows the (zero) free space table
	if b.meta {
		le32(d[80+4*n+4:], b.metaLen)
	}
	iu := d[80+8*n:]
	regid(iu, "*zenvik", nil)
	le32(iu[32:], b.files)
	le32(iu[36:], b.dirs)
	le16(iu[40:], b.opt.Revision) // minimum read revision
	le16(iu[42:], b.opt.Revision) // minimum write revision
	le16(iu[44:], b.opt.Revision) // maximum write revision
	return b.tag(9, lvidSector, d)
}

func (b *builder) avdp(loc uint32) []byte {
	d := make([]byte, 512)
	le32(d[16:], 16*sector) // main volume descriptor sequence
	le32(d[20:], 32)
	le32(d[24:], 16*sector) // reserve volume descriptor sequence
	le32(d[28:], 48)
	return b.tag(2, loc, d)
}

func (b *builder) image() []byte {
	partLen := b.physNext
	total := partStart + partLen + 1
	img := make([]byte, int(total)*sector)
	put := func(s uint32, d []byte) { copy(img[int(s)*sector:], d) }
	nsr := "NSR02"
	if b.meta {
		nsr = "NSR03"
	}
	put(16, vsd("BEA01"))
	put(17, vsd(nsr))
	put(18, vsd("TEA01"))
	for i, d := range b.vds(32, partLen) {
		put(32+uint32(i), d)
	}
	for i, d := range b.vds(48, partLen) {
		put(48+uint32(i), d)
	}
	put(lvidSector, b.lvid(partLen))
	put(256, b.avdp(256))
	put(total-1, b.avdp(total-1))
	feBase := uint32(partStart)
	if b.meta {
		feBase += 2
	}
	for blk, d := range b.feSpace {
		put(feBase+blk, d)
	}
	for blk, d := range b.phys {
		put(partStart+blk, d)
	}
	if b.meta {
		for i, ft := range []uint8{250, 251} { // metadata file, metadata mirror file
			put(partStart+uint32(i), b.entry(entryParams{
				loc: uint32(i), fileType: ft, links: 1,
				ads:      shortAD(0, b.metaLen*sector, 2),
				size:     uint64(b.metaLen) * sector,
				recorded: uint64(b.metaLen),
			}))
		}
	}
	return img
}

func vsd(id string) []byte {
	d := make([]byte, sector)
	copy(d[1:], id)
	d[6] = 1
	return d
}

func (b *builder) tag(id uint16, loc uint32, d []byte) []byte {
	ver := uint16(2)
	if b.meta {
		ver = 3
	}
	le16(d[0:], id)
	le16(d[2:], ver)
	le16(d[6:], 1) // serial number
	le16(d[8:], crc16(d[16:]))
	le16(d[10:], uint16(len(d)-16))
	le32(d[12:], loc)
	d[4] = tagChecksum(d)
	return d
}

// tagChecksum is the descriptor tag checksum: the sum of the tag's bytes
// other than the checksum itself.
func tagChecksum(d []byte) byte {
	var sum byte
	for i := 0; i < 16; i++ {
		if i != 4 {
			sum += d[i]
		}
	}
	return sum
}

// crc16 is the ECMA-167 descriptor CRC: polynomial 0x1021, initial 0.
func crc16(b []byte) uint16 {
	var c uint16
	for _, x := range b {
		c ^= uint16(x) << 8
		for range 8 {
			if c&0x8000 != 0 {
				c = c<<1 ^ 0x1021
			} else {
				c <<= 1
			}
		}
	}
	return c
}

func le16(d []byte, v uint16) { binary.LittleEndian.PutUint16(d, v) }
func le32(d []byte, v uint32) { binary.LittleEndian.PutUint32(d, v) }
func le64(d []byte, v uint64) { binary.LittleEndian.PutUint64(d, v) }

func shortAD(typ uint8, length, loc uint32) []byte {
	d := make([]byte, 8)
	le32(d, uint32(typ)<<30|length)
	le32(d[4:], loc)
	return d
}

func longAD(typ uint8, length, loc uint32, ref uint16) []byte {
	d := make([]byte, 16)
	le32(d, uint32(typ)<<30|length)
	le32(d[4:], loc)
	le16(d[8:], ref)
	return d
}

func regid(dst []byte, id string, suffix []byte) {
	copy(dst[1:24], id)
	copy(dst[24:32], suffix)
}

func (b *builder) udfRegid(dst []byte, id string) {
	regid(dst, id, binary.LittleEndian.AppendUint16(nil, b.opt.Revision))
}

func (b *builder) domain(dst []byte) { b.udfRegid(dst, "*OSTA UDF Compliant") }

func charspec(d []byte) {
	d[0] = 0
	copy(d[1:], "OSTA Compressed Unicode")
}

func timestamp(d []byte) {
	le16(d, 1<<12) // local time with a UTC offset of 0
	le16(d[2:], uint16(stamp.Year()))
	d[4] = byte(stamp.Month())
	d[5] = byte(stamp.Day())
	d[6] = byte(stamp.Hour())
	d[7] = byte(stamp.Minute())
	d[8] = byte(stamp.Second())
}

// encodeName returns OSTA CS0 d-characters: compression ID 8 (one byte
// per character) when every rune fits in Latin-1, otherwise 16 (UTF-16BE).
func encodeName(s string) []byte {
	latin1 := true
	for _, r := range s {
		if r > 0xFF {
			latin1 = false
			break
		}
	}
	if latin1 {
		out := []byte{8}
		for _, r := range s {
			out = append(out, byte(r))
		}
		return out
	}
	out := []byte{16}
	for _, u := range utf16.Encode([]rune(s)) {
		out = append(out, byte(u>>8), byte(u))
	}
	return out
}

// dstring fills a fixed-size dstring field; the last byte holds the
// number of bytes used.
func dstring(dst []byte, s string) {
	if s == "" {
		return
	}
	e := encodeName(s)
	if len(e) > len(dst)-1 {
		e = e[:len(dst)-1]
	}
	copy(dst, e)
	dst[len(dst)-1] = byte(len(e))
}
