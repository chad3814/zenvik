// Package udfrepair fixes, in place, UDF disc images whose directory
// entries' tag CRC lengths leave out padding (see udf.PaddingCRCFixes),
// keeping a backup of every byte it changes so the repair can be undone.
package udfrepair

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/chad3814/zenvik/udf"
)

// BackupSuffix is appended to an image's path to name its backup.
const BackupSuffix = ".udf-repair-backup"

var (
	// ErrBackupExists: a backup from an earlier repair is in the way.
	ErrBackupExists = errors.New("udfrepair: a backup from an earlier repair exists")
	// ErrChanged: the image's bytes aren't what the repair or undo expects.
	ErrChanged = errors.New("udfrepair: the image changed since it was checked")
	// ErrNoBackup: there is no backup to undo.
	ErrNoBackup = errors.New("udfrepair: no backup to undo")
)

// Plan is what a repair would change.
type Plan struct {
	Patches []udf.Patch
	Dirs    []string // distinct directories with patches, in walk order
}

// Entries is the number of directory entries the plan fixes (a tag that
// crosses an extent boundary is one entry but two patches).
func (p Plan) Entries() int {
	n := 0
	for i, pt := range p.Patches {
		if i == 0 || p.Patches[i-1].Off+int64(len(p.Patches[i-1].New)) != pt.Off {
			n++
		}
	}
	return n
}

// Check opens image read-only and returns what a repair would change.
func Check(image string) (Plan, error) {
	img, err := udf.OpenImage(image)
	if err != nil {
		return Plan{}, err
	}
	defer img.Close()
	ps, err := img.PaddingCRCFixes()
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{Patches: ps}
	for _, p := range ps {
		if n := len(plan.Dirs); n == 0 || plan.Dirs[n-1] != p.Dir {
			plan.Dirs = append(plan.Dirs, p.Dir)
		}
	}
	return plan, nil
}

type backup struct {
	Version   int           `json:"version"`
	ImageSize int64         `json:"image_size"`
	Patches   []backupPatch `json:"patches"`
}

type backupPatch struct {
	Off int64  `json:"off"`
	Old string `json:"old"`
	New string `json:"new"`
}

// Test hooks.
var (
	beforeWrite = func() {}
	recheck     = recheckImage
)

// Repair fixes image in place and returns the number of entries fixed (0
// when it needs nothing). It writes image+BackupSuffix first.
func Repair(image string) (int, error) {
	plan, err := Check(image)
	if err != nil {
		return 0, err
	}
	if len(plan.Patches) == 0 {
		return 0, nil
	}
	bpath := image + BackupSuffix
	if _, err := os.Lstat(bpath); err == nil {
		return 0, fmt.Errorf("%w: %s", ErrBackupExists, bpath)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return 0, err
	}
	f, err := os.OpenFile(image, os.O_RDWR, 0)
	if err != nil {
		return 0, fmt.Errorf("can't write %s: %w", image, err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if err := writeBackup(bpath, st.Size(), plan.Patches); err != nil {
		return 0, err
	}
	beforeWrite()
	if err := expect(f, plan.Patches, false); err != nil {
		return 0, fmt.Errorf("%w; nothing was written, and the backup %s is safe to delete", err, bpath)
	}
	if err := write(f, plan.Patches, false); err != nil {
		_ = write(f, plan.Patches, true)
		_ = f.Sync()
		return 0, fmt.Errorf("writing %s: %w (the original bytes were written back; %s holds them too)", image, err, bpath)
	}
	if err := f.Sync(); err != nil {
		return 0, err
	}
	if err := recheck(image); err != nil {
		werr := write(f, plan.Patches, true)
		if werr == nil {
			werr = f.Sync()
		}
		if werr != nil {
			return 0, fmt.Errorf("the repaired image didn't check out (%w), and restoring it failed (%w); %s holds the original bytes", err, werr, bpath)
		}
		_ = os.Remove(bpath)
		return 0, fmt.Errorf("the repaired image didn't check out, so it was restored: %w", err)
	}
	return plan.Entries(), nil
}

// Undo writes back the bytes a Repair replaced and removes the backup.
func Undo(image string) error {
	bpath := image + BackupSuffix
	raw, err := os.ReadFile(bpath)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrNoBackup, bpath)
	}
	if err != nil {
		return err
	}
	var b backup
	if err := json.Unmarshal(raw, &b); err != nil || b.Version != 1 {
		return fmt.Errorf("udfrepair: %s isn't a version 1 backup", bpath)
	}
	ps := make([]udf.Patch, len(b.Patches))
	for i, bp := range b.Patches {
		o, err1 := hex.DecodeString(bp.Old)
		n, err2 := hex.DecodeString(bp.New)
		if err1 != nil || err2 != nil || len(o) != len(n) {
			return fmt.Errorf("udfrepair: %s has a malformed patch at offset %d", bpath, bp.Off)
		}
		ps[i] = udf.Patch{Off: bp.Off, Old: o, New: n}
	}
	f, err := os.OpenFile(image, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("can't write %s: %w", image, err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.Size() != b.ImageSize {
		return fmt.Errorf("%w: it is %d bytes, the backup is for %d", ErrChanged, st.Size(), b.ImageSize)
	}
	if err := expect(f, ps, true); err != nil {
		return fmt.Errorf("%w; nothing was written", err)
	}
	if err := write(f, ps, true); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return os.Remove(bpath)
}

func writeBackup(path string, size int64, ps []udf.Patch) error {
	b := backup{Version: 1, ImageSize: size}
	for _, p := range ps {
		b.Patches = append(b.Patches, backupPatch{Off: p.Off, Old: hex.EncodeToString(p.Old), New: hex.EncodeToString(p.New)})
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("writing the backup: %w", err)
	}
	if _, err := f.Write(raw); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("writing the backup: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("writing the backup: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("writing the backup: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("writing the backup: %w", err)
	}
	return nil
}

// expect checks every patch's range holds Old (or New, when repaired).
func expect(f *os.File, ps []udf.Patch, repaired bool) error {
	for _, p := range ps {
		want := p.Old
		if repaired {
			want = p.New
		}
		got := make([]byte, len(want))
		if _, err := f.ReadAt(got, p.Off); err != nil {
			return fmt.Errorf("%w: reading offset %d: %w", ErrChanged, p.Off, err)
		}
		if !bytes.Equal(got, want) {
			return fmt.Errorf("%w: offset %d", ErrChanged, p.Off)
		}
	}
	return nil
}

// write writes every patch's New (or Old, when undoing).
func write(f *os.File, ps []udf.Patch, undo bool) error {
	for _, p := range ps {
		b := p.New
		if undo {
			b = p.Old
		}
		if _, err := f.WriteAt(b, p.Off); err != nil {
			return err
		}
	}
	return nil
}

func recheckImage(image string) error {
	plan, err := Check(image)
	if err != nil {
		return err
	}
	if len(plan.Patches) > 0 {
		return fmt.Errorf("it still needs %d fixes", len(plan.Patches))
	}
	return nil
}
