package bluray

import (
	"errors"
	"reflect"
	"testing"
)

func cmd(op, dst, src uint32) []byte { return cat(be32(op), be32(dst), be32(src)) }

func movieObjectBytes() []byte {
	objs := cat(
		zeros(4), be16(3),
		be16(0x8000), be16(3), // object 0: resume intention, 3 commands
		cmd(0x50400001, 5, 801), // MOVE r5, 801 (immediate source)
		cmd(0x22000000, 5, 0),   // PLAY_PL r5 (register operand)
		cmd(0x21800000, 1, 0),   // JUMP_OBJECT 1
		be16(0), be16(2),        // object 1
		cmd(0x22800000, 800, 0), // PLAY_PL 800
		cmd(0x22800000, 801, 0), // PLAY_PL 801 (already seen from object 0)
		be16(0), be16(2),        // object 2
		cmd(0x22810000, 5, 0), // PLAY_PL_PI 5
		cmd(0x21800000, 2, 0), // JUMP_OBJECT 2 (jumps to itself)
	)
	return cat([]byte("MOBJ0200"), be32(0), zeros(28), be32(uint32(len(objs))), objs)
}

func TestParseMovieObjects(t *testing.T) {
	m, err := ParseMovieObjects(movieObjectBytes())
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != "0200" || len(m.Objects) != 3 {
		t.Fatalf("version %q, %d objects", m.Version, len(m.Objects))
	}
	o := m.Objects[0]
	if !o.ResumeIntention || o.MenuCallMask || o.TitleSearchMask || len(o.Commands) != 3 {
		t.Fatalf("object 0 = %+v", o)
	}
	move := o.Commands[0]
	if move.Group() != 2 || move.SubGroup() != 0 || move.SetOpt() != 1 || !move.ImmSrc() || move.ImmDst() {
		t.Errorf("MOVE decoded as group %d sub %d set %d immSrc %v immDst %v",
			move.Group(), move.SubGroup(), move.SetOpt(), move.ImmSrc(), move.ImmDst())
	}
	play := o.Commands[1]
	if play.Group() != 0 || play.SubGroup() != 2 || play.BranchOpt() != 0 || play.ImmDst() {
		t.Errorf("PLAY_PL decoded as group %d sub %d opt %d immDst %v",
			play.Group(), play.SubGroup(), play.BranchOpt(), play.ImmDst())
	}
	if pi := m.Objects[2].Commands[0]; pi.BranchOpt() != 1 || !pi.ImmDst() || pi.Dst != 5 {
		t.Errorf("PLAY_PL_PI = %+v", pi)
	}
}

func TestPlaylists(t *testing.T) {
	m, err := ParseMovieObjects(movieObjectBytes())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		obj  int
		want []int
	}{
		{0, []int{801, 800}},
		{1, []int{800, 801}},
		{2, []int{5}},
		{9, nil},
		{-1, nil},
	}
	for _, tt := range tests {
		if got := m.Playlists(tt.obj); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Playlists(%d) = %v, want %v", tt.obj, got, tt.want)
		}
	}
}

func TestPlaylistsJumpCycle(t *testing.T) {
	jump := func(to uint32) NavCommand { return NavCommand{Opcode: 0x21800000, Dst: to} }
	m := &MovieObjects{Objects: []MovieObject{
		{Commands: []NavCommand{jump(1), {Opcode: 0x22800000, Dst: 10}}},
		{Commands: []NavCommand{jump(0), {Opcode: 0x22800000, Dst: 11}}},
	}}
	if got, want := m.Playlists(0), []int{11, 10}; !reflect.DeepEqual(got, want) {
		t.Errorf("Playlists(0) = %v, want %v", got, want)
	}
}

func TestPlaylistsUnknownRegisterIsSkipped(t *testing.T) {
	m := &MovieObjects{Objects: []MovieObject{{Commands: []NavCommand{
		{Opcode: 0x50400001, Dst: 3, Src: 900}, // MOVE r3, 900
		{Opcode: 0x50400003, Dst: 3, Src: 1},   // ADD r3, 1 (no longer a known constant)
		{Opcode: 0x22000000, Dst: 3},           // PLAY_PL r3
		{Opcode: 0x22000000, Dst: 4},           // PLAY_PL r4 (never set)
	}}}}
	if got := m.Playlists(0); got != nil {
		t.Errorf("Playlists(0) = %v, want nil", got)
	}
}

func TestParseMovieObjectsErrors(t *testing.T) {
	b := movieObjectBytes()
	for name, in := range map[string][]byte{
		"truncated": b[:len(b)-1],
		"magic":     append([]byte("INDX"), b[4:]...),
	} {
		if _, err := ParseMovieObjects(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func FuzzParseMovieObjects(f *testing.F) {
	f.Add(movieObjectBytes())
	f.Fuzz(func(t *testing.T, b []byte) {
		m, err := ParseMovieObjects(b)
		if err != nil {
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("unexpected error type: %v", err)
			}
			return
		}
		for i := range m.Objects {
			m.Playlists(i)
		}
	})
}
