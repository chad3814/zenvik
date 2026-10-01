package bluray

// ObjectType says how a title is implemented.
type ObjectType uint8

// Object types.
const (
	ObjectHDMV ObjectType = 1 // movie object navigation commands
	ObjectBDJ  ObjectType = 2 // BD-J (Java) application
)

// Object is the entry for first playback, top menu, or a title.
type Object struct {
	Type          ObjectType
	PlaybackType  uint8
	MovieObjectID uint16 // HDMV only
	BDJOName      string // BD-J only, e.g. "00000"
}

// IndexTitle is one numbered title in index.bdmv.
type IndexTitle struct {
	Object
	AccessType uint8
}

// Index is a parsed index.bdmv.
type Index struct {
	Version       string
	FirstPlayback Object
	TopMenu       Object
	Titles        []IndexTitle // Titles[0] is title 1
}

// ParseIndex parses the contents of BDMV/index.bdmv.
func ParseIndex(b []byte) (*Index, error) {
	r := newReader(b)
	idx := &Index{Version: r.header("INDX")}
	indexesStart := r.u32()
	r.seek(int(indexesStart))
	ir := r.sub(int(r.u32()))
	t, _ := objectHeader(ir)
	idx.FirstPlayback = objectBody(ir, t)
	t, _ = objectHeader(ir)
	idx.TopMenu = objectBody(ir, t)
	n := int(ir.u16())
	for i := 0; i < n && ir.err() == nil; i++ {
		t, access := objectHeader(ir)
		idx.Titles = append(idx.Titles, IndexTitle{Object: objectBody(ir, t), AccessType: access})
	}
	if err := r.err(); err != nil {
		return nil, err
	}
	return idx, nil
}

// objectHeader reads object_type (2 bits) and, for titles, access_type
// (2 bits) from a 32-bit field.
func objectHeader(r *reader) (ObjectType, uint8) {
	h := r.u32()
	return ObjectType(h >> 30), uint8(h>>28) & 0x3
}

// objectBody reads the 8-byte HDMV or BD-J object reference.
func objectBody(r *reader, t ObjectType) Object {
	o := Object{Type: t, PlaybackType: uint8(r.u16() >> 14)}
	switch t {
	case ObjectHDMV:
		o.MovieObjectID = r.u16()
		r.skip(4)
	case ObjectBDJ:
		o.BDJOName = r.str(5)
		r.skip(1)
	default:
		r.skip(6)
	}
	return o
}
