package bluray

// Navigation command fields (libbluray hdmv_insn.h).
const (
	groupBranch = 0
	groupSet    = 2

	branchJump = 1
	branchPlay = 2

	jumpObject = 0
	callObject = 2

	playPLPM = 2 // PLAY_PL=0, PLAY_PL_PI=1, PLAY_PL_PM=2

	setSet  = 0
	setMove = 1
)

// NavCommand is one 12-byte HDMV navigation command.
type NavCommand struct {
	Opcode uint32
	Dst    uint32
	Src    uint32
}

// Group returns the instruction group (0 branch, 1 compare, 2 set).
func (c NavCommand) Group() uint8 { return uint8(c.Opcode>>27) & 0x3 }

// SubGroup returns the instruction sub-group.
func (c NavCommand) SubGroup() uint8 { return uint8(c.Opcode>>24) & 0x7 }

// ImmDst reports whether Dst is an immediate value rather than a register.
func (c NavCommand) ImmDst() bool { return c.Opcode&(1<<23) != 0 }

// ImmSrc reports whether Src is an immediate value rather than a register.
func (c NavCommand) ImmSrc() bool { return c.Opcode&(1<<22) != 0 }

// BranchOpt returns the branch option of branch-group instructions.
func (c NavCommand) BranchOpt() uint8 { return uint8(c.Opcode>>16) & 0xF }

// SetOpt returns the set option of set-group instructions.
func (c NavCommand) SetOpt() uint8 { return uint8(c.Opcode) & 0x1F }

// MovieObject is one HDMV movie object.
type MovieObject struct {
	ResumeIntention bool
	MenuCallMask    bool
	TitleSearchMask bool
	Commands        []NavCommand
}

// MovieObjects is a parsed MovieObject.bdmv.
type MovieObjects struct {
	Version string
	Objects []MovieObject
}

// ParseMovieObjects parses the contents of BDMV/MovieObject.bdmv.
func ParseMovieObjects(b []byte) (*MovieObjects, error) {
	r := newReader(b)
	m := &MovieObjects{Version: r.header("MOBJ")}
	r.skip(4)  // extension data start address
	r.skip(28) // reserved
	mr := r.sub(int(r.u32()))
	mr.skip(4)
	n := int(mr.u16())
	for i := 0; i < n && mr.err() == nil; i++ {
		flags := mr.u16()
		obj := MovieObject{
			ResumeIntention: flags&0x8000 != 0,
			MenuCallMask:    flags&0x4000 != 0,
			TitleSearchMask: flags&0x2000 != 0,
		}
		nc := int(mr.u16())
		for j := 0; j < nc && mr.err() == nil; j++ {
			obj.Commands = append(obj.Commands, NavCommand{Opcode: mr.u32(), Dst: mr.u32(), Src: mr.u32()})
		}
		m.Objects = append(m.Objects, obj)
	}
	if err := r.err(); err != nil {
		return nil, err
	}
	return m, nil
}

// Playlists returns the playlist numbers that movie object objectID may
// play, in first-seen order without duplicates. It follows JumpObject and
// CallObject branches with immediate targets. A register operand is
// resolved only when an earlier Move in the same object loaded that
// register with a known value; anything else is skipped. Commands are
// scanned in order, ignoring conditional branches.
func (m *MovieObjects) Playlists(objectID int) []int {
	var out []int
	seen := map[int]bool{}
	visited := map[int]bool{}
	var walk func(id int)
	walk = func(id int) {
		if id < 0 || id >= len(m.Objects) || visited[id] {
			return
		}
		visited[id] = true
		regs := map[uint32]uint32{}
		for _, c := range m.Objects[id].Commands {
			switch {
			case c.Group() == groupSet && c.SubGroup() == setSet:
				v, ok := c.Src, c.ImmSrc()
				if !ok {
					v, ok = regs[c.Src]
				}
				if c.SetOpt() == setMove && ok {
					regs[c.Dst] = v
				} else {
					delete(regs, c.Dst)
				}
			case c.Group() == groupBranch && c.SubGroup() == branchPlay && c.BranchOpt() <= playPLPM:
				pl, ok := c.Dst, c.ImmDst()
				if !ok {
					pl, ok = regs[c.Dst]
				}
				if ok && !seen[int(pl)] {
					seen[int(pl)] = true
					out = append(out, int(pl))
				}
			case c.Group() == groupBranch && c.SubGroup() == branchJump && c.ImmDst() &&
				(c.BranchOpt() == jumpObject || c.BranchOpt() == callObject):
				walk(int(c.Dst))
			}
		}
	}
	walk(objectID)
	return out
}
