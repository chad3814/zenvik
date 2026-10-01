package testdisc

import (
	"os"
	"path/filepath"
	"testing/fstest"

	"github.com/chad3814/zenvik/bluray"
)

// Navigation command opcodes (see bluray.NavCommand).
const (
	opPlayPL     = 0x22800000 // PLAY_PL, immediate playlist
	opPlayPLReg  = 0x22000000 // PLAY_PL, playlist in register
	opMoveImm    = 0x50400001 // SET MOVE, immediate source
	opJumpObject = 0x21800000 // JUMP_OBJECT, immediate
	opJumpTitle  = 0x21810000 // JUMP_TITLE, immediate
)

// CmdPlayPL plays playlist pl.
func CmdPlayPL(pl uint32) bluray.NavCommand { return bluray.NavCommand{Opcode: opPlayPL, Dst: pl} }

// CmdPlayPLReg plays the playlist whose number is in general register reg.
func CmdPlayPLReg(reg uint32) bluray.NavCommand {
	return bluray.NavCommand{Opcode: opPlayPLReg, Dst: reg}
}

// CmdMove loads value into general register reg.
func CmdMove(reg, value uint32) bluray.NavCommand {
	return bluray.NavCommand{Opcode: opMoveImm, Dst: reg, Src: value}
}

// CmdJumpObject jumps to movie object id.
func CmdJumpObject(id uint32) bluray.NavCommand {
	return bluray.NavCommand{Opcode: opJumpObject, Dst: id}
}

// CmdJumpTitle jumps to title number title.
func CmdJumpTitle(title uint32) bluray.NavCommand {
	return bluray.NavCommand{Opcode: opJumpTitle, Dst: title}
}

// CleanM2TS returns units aligned units (32 source packets, 6144 bytes)
// of unencrypted M2TS: each packet has a 4-byte TP_extra_header followed
// by a TS null packet starting with the 0x47 sync byte.
func CleanM2TS(units int) []byte {
	b := make([]byte, units*6144)
	for p := 0; p < units*32; p++ {
		o := p * 192
		b[o+4] = 0x47
		b[o+5] = 0x1F // PID 0x1FFF (null packet)
		b[o+6] = 0xFF
		b[o+7] = 0x10 // payload only
	}
	return b
}

// Disc describes a synthetic BDMV tree.
type Disc struct {
	FirstPlayback bluray.Object
	TopMenu       bluray.Object
	Titles        []bluray.IndexTitle
	MovieObjects  []bluray.MovieObject
	Playlists     map[string]*bluray.Playlist // keyed by playlist ID, e.g. "00800"
	Clips         map[string]*bluray.Clip     // keyed by clip ID, e.g. "00001"
	ClipData      map[string][]byte           // M2TS contents by clip ID; default CleanM2TS(1)
	MetaTitle     string                      // writes BDMV/META/DL/bdmt_eng.xml when non-empty
}

// Files returns the disc's files keyed by slash-separated path.
func (d *Disc) Files() map[string][]byte {
	files := map[string][]byte{
		"BDMV/index.bdmv": Index(&bluray.Index{
			Version: "0200", FirstPlayback: d.FirstPlayback, TopMenu: d.TopMenu, Titles: d.Titles,
		}),
		"BDMV/MovieObject.bdmv": MovieObjects(&bluray.MovieObjects{Version: "0200", Objects: d.MovieObjects}),
	}
	for id, p := range d.Playlists {
		files["BDMV/PLAYLIST/"+id+".mpls"] = Playlist(p)
	}
	for id, c := range d.Clips {
		files["BDMV/CLIPINF/"+id+".clpi"] = Clip(c)
		data, ok := d.ClipData[id]
		if !ok {
			data = CleanM2TS(1)
		}
		files["BDMV/STREAM/"+id+".m2ts"] = data
	}
	if d.MetaTitle != "" {
		files["BDMV/META/DL/bdmt_eng.xml"] = Meta(d.MetaTitle, "eng")
	}
	return files
}

// MapFS returns the disc as an in-memory file system.
func (d *Disc) MapFS() fstest.MapFS {
	m := fstest.MapFS{}
	for name, data := range d.Files() {
		m[name] = &fstest.MapFile{Data: data, Mode: 0o444}
	}
	return m
}

// WriteDir writes the disc's files under dir.
func (d *Disc) WriteDir(dir string) error {
	for name, data := range d.Files() {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// ScrambledM2TS returns units aligned units that look AACS-encrypted: each
// unit keeps the 16 clear bytes a CleanM2TS unit starts with, and the rest
// is deterministic pseudo-random data with no 0x47 sync byte at any source
// packet's sync offset.
func ScrambledM2TS(units int) []byte {
	b := CleanM2TS(units)
	x := uint32(2463534242)
	for u := 0; u < units; u++ {
		base := u * 6144
		for i := 16; i < 6144; i++ {
			x ^= x << 13
			x ^= x >> 17
			x ^= x << 5
			v := byte(x)
			if i%192 == 4 && v == 0x47 {
				v = 0x48
			}
			b[base+i] = v
		}
	}
	return b
}

// StubClip returns minimal clip information for a one-unit M2TS clip.
func StubClip() *bluray.Clip {
	return &bluray.Clip{Version: "0200", StreamType: 1, ApplicationType: 1, TSRecordingRate: 48_000_000, SourcePackets: 32}
}
