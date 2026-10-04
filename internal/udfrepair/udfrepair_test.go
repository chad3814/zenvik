package udfrepair

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/chad3814/zenvik/internal/testdisc"
	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
)

func flawed(t *testing.T, dir string, unpadded bool) string {
	t.Helper()
	img, err := testdisc.SampleMovie().ISO(udfimage.Options{Revision: 0x0250, Label: "X", UnpaddedFIDCRC: unpadded})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "disc.iso")
	if err := os.WriteFile(p, img, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func read(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCheck(t *testing.T) {
	clean, err := Check(flawed(t, t.TempDir(), false))
	if err != nil || len(clean.Patches) != 0 {
		t.Fatalf("clean: %d patches, %v", len(clean.Patches), err)
	}
	plan, err := Check(flawed(t, t.TempDir(), true))
	if err != nil || len(plan.Patches) == 0 {
		t.Fatalf("flawed: %d patches, %v", len(plan.Patches), err)
	}
	if len(plan.Dirs) == 0 || plan.Dirs[0] != "." {
		t.Errorf("Dirs = %v, want the root first", plan.Dirs)
	}
}

func TestRepairAndUndo(t *testing.T) {
	p := flawed(t, t.TempDir(), true)
	orig := read(t, p)
	plan, err := Check(p)
	if err != nil {
		t.Fatal(err)
	}
	n, err := Repair(p)
	if err != nil || n == 0 {
		t.Fatalf("Repair = %d, %v", n, err)
	}
	var b backup
	if err := json.Unmarshal(read(t, p+BackupSuffix), &b); err != nil || b.Version != 1 || b.ImageSize != int64(len(orig)) || len(b.Patches) != len(plan.Patches) {
		t.Fatalf("backup = %+v, %v", b, err)
	}
	fixed := read(t, p)
	inPatch := map[int64]bool{}
	for _, pt := range plan.Patches {
		for i := range pt.New {
			inPatch[pt.Off+int64(i)] = true
		}
	}
	for i := range fixed {
		if fixed[i] != orig[i] && !inPatch[int64(i)] {
			t.Fatalf("byte %d changed outside the patches", i)
		}
	}
	if again, err := Check(p); err != nil || len(again.Patches) != 0 {
		t.Fatalf("after repair: %d patches, %v", len(again.Patches), err)
	}
	if err := os.Remove(p + BackupSuffix); err != nil { // so the next Repair isn't refused for the backup
		t.Fatal(err)
	}
	if n, err := Repair(p); err != nil || n != 0 {
		t.Fatalf("second Repair = %d, %v; want nothing to repair", n, err)
	}
	if _, err := os.Stat(p + BackupSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("nothing-to-repair wrote a backup: %v", err)
	}
}

func TestUndoRestoresTheOriginal(t *testing.T) {
	p := flawed(t, t.TempDir(), true)
	want := sha256.Sum256(read(t, p))
	if _, err := Repair(p); err != nil {
		t.Fatal(err)
	}
	if err := Undo(p); err != nil {
		t.Fatal(err)
	}
	if got := sha256.Sum256(read(t, p)); got != want {
		t.Fatal("Undo didn't restore the original bytes")
	}
	if _, err := os.Stat(p + BackupSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Undo left the backup: %v", err)
	}
	if err := Undo(p); !errors.Is(err, ErrNoBackup) {
		t.Fatalf("Undo without a backup: %v, want ErrNoBackup", err)
	}
}

func TestUndoRefusesAnImageThatIsNoLongerRepaired(t *testing.T) {
	p := flawed(t, t.TempDir(), true)
	orig := read(t, p)
	if _, err := Repair(p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, orig, 0o644); err != nil { // reverted by hand
		t.Fatal(err)
	}
	if err := Undo(p); !errors.Is(err, ErrChanged) {
		t.Fatalf("Undo of a hand-reverted image: %v, want ErrChanged", err)
	}
	if !bytes.Equal(read(t, p), orig) {
		t.Fatal("Undo wrote to the image")
	}
}

func TestRepairRefusesAnExistingBackup(t *testing.T) {
	p := flawed(t, t.TempDir(), true)
	orig := read(t, p)
	if err := os.WriteFile(p+BackupSuffix, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Repair(p); !errors.Is(err, ErrBackupExists) {
		t.Fatalf("err = %v, want ErrBackupExists", err)
	}
	if !bytes.Equal(read(t, p), orig) || string(read(t, p+BackupSuffix)) != "{}" {
		t.Fatal("refused repair changed the image or the backup")
	}
}

func TestRepairStopsIfTheImageChanged(t *testing.T) {
	p := flawed(t, t.TempDir(), true)
	plan, err := Check(p)
	if err != nil {
		t.Fatal(err)
	}
	old := beforeWrite
	t.Cleanup(func() { beforeWrite = old })
	var changed []byte
	beforeWrite = func() {
		b := read(t, p)
		b[plan.Patches[0].Off] ^= 0xFF
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		changed = b
	}
	if _, err := Repair(p); !errors.Is(err, ErrChanged) || !strings.Contains(err.Error(), "safe to delete") {
		t.Fatalf("err = %v, want ErrChanged mentioning the backup is safe to delete", err)
	}
	if !bytes.Equal(read(t, p), changed) {
		t.Fatal("repair wrote to an image that changed")
	}
}

func TestRepairRestoresWhenTheRecheckFails(t *testing.T) {
	p := flawed(t, t.TempDir(), true)
	orig := read(t, p)
	old := recheck
	t.Cleanup(func() { recheck = old })
	recheck = func(string) error { return errors.New("injected re-check failure") }
	if _, err := Repair(p); err == nil || !strings.Contains(err.Error(), "injected re-check failure") {
		t.Fatalf("err = %v, want the re-check failure", err)
	}
	if !bytes.Equal(read(t, p), orig) {
		t.Fatal("failed re-check didn't restore the original bytes")
	}
	if _, err := os.Stat(p + BackupSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed re-check left the backup: %v", err)
	}
}

func TestRepairReadOnly(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a non-root Unix user")
	}
	t.Run("image", func(t *testing.T) {
		p := flawed(t, t.TempDir(), true)
		if err := os.Chmod(p, 0o444); err != nil {
			t.Fatal(err)
		}
		if _, err := Repair(p); err == nil || !strings.Contains(err.Error(), "can't write") {
			t.Fatalf("err = %v, want can't write", err)
		}
		if _, err := os.Stat(p + BackupSuffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read-only image left a backup: %v", err)
		}
	})
	t.Run("directory", func(t *testing.T) {
		dir := t.TempDir()
		p := flawed(t, dir, true)
		orig := read(t, p)
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		if _, err := Repair(p); err == nil {
			t.Fatal("repair with an unwritable backup directory: want an error")
		}
		if !bytes.Equal(read(t, p), orig) {
			t.Fatal("repair changed the image although the backup couldn't be written")
		}
	})
}
