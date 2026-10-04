package main

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chad3814/zenvik/internal/testdisc"
	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
	"github.com/chad3814/zenvik/internal/udfrepair"
)

func writeImage(t *testing.T, dir, name string, unpadded bool) string {
	t.Helper()
	img, err := testdisc.SampleMovie().ISO(udfimage.Options{Revision: 0x0102, Label: "X", UnpaddedFIDCRC: unpadded})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, img, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func sum(t *testing.T, p string) [32]byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(b)
}

func TestRepairUDFDryRun(t *testing.T) {
	dir := t.TempDir()
	p := writeImage(t, dir, "bad.iso", true)
	before := sum(t, p)
	code, out, errOut := runCLI("repair-udf", "--dry-run", p)
	if code != 0 || !strings.Contains(lineWith(out, p), "would repair") || !strings.Contains(out, "(root)") {
		t.Fatalf("code %d, out %q, err %q", code, out, errOut)
	}
	if sum(t, p) != before {
		t.Fatal("--dry-run changed the image")
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Fatalf("--dry-run created files: %v", ents)
	}
}

func TestRepairUDFRepairThenUndo(t *testing.T) {
	p := writeImage(t, t.TempDir(), "bad.iso", true)
	before := sum(t, p)
	code, out, errOut := runCLI("repair-udf", p)
	if code != 0 || !strings.Contains(lineWith(out, p), "repaired") || !strings.Contains(out, udfrepair.BackupSuffix) {
		t.Fatalf("repair: code %d, out %q, err %q", code, out, errOut)
	}
	code, out, _ = runCLI("repair-udf", "--dry-run", p)
	if code != 0 || !strings.Contains(lineWith(out, p), "nothing to repair") {
		t.Fatalf("dry run after repair: code %d, out %q", code, out)
	}
	code, out, errOut = runCLI("repair-udf", "--undo", p)
	if code != 0 || !strings.Contains(lineWith(out, p), "undone") {
		t.Fatalf("undo: code %d, out %q, err %q", code, out, errOut)
	}
	if sum(t, p) != before {
		t.Fatal("undo didn't restore the original")
	}
}

func TestRepairUDFBatch(t *testing.T) {
	dir := t.TempDir()
	clean := writeImage(t, dir, "clean.iso", false)
	bad := writeImage(t, dir, "bad.iso", true)
	junk := filepath.Join(dir, "junk.iso")
	if err := os.WriteFile(junk, make([]byte, 64<<10), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runCLI("repair-udf", clean, bad, junk)
	if code != 1 {
		t.Fatalf("code %d, want 1; out %q", code, out)
	}
	if !strings.Contains(lineWith(out, clean), "nothing to repair") ||
		!strings.Contains(lineWith(out, bad), "repaired") ||
		!strings.Contains(lineWith(out, junk), "failed:") {
		t.Fatalf("out %q", out)
	}
}

func TestRepairUDFUsage(t *testing.T) {
	if code, _, _ := runCLI("repair-udf"); code != 2 {
		t.Errorf("no images: code %d, want 2", code)
	}
	if code, _, _ := runCLI("repair-udf", "--dry-run", "--undo", "x.iso"); code != 2 {
		t.Errorf("--dry-run --undo: code %d, want 2", code)
	}
}
