//go:build integration && linux

package udfrepair

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestKernelUDFRepair proves the repair against Linux's real UDF driver. It
// needs root (for mount); CI runs it with sudo.
func TestKernelUDFRepair(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root to mount")
	}
	dir := t.TempDir()
	p := flawed(t, dir, true)
	mnt := filepath.Join(dir, "mnt")
	if err := os.Mkdir(mnt, 0o755); err != nil {
		t.Fatal(err)
	}
	mount := func() error {
		out, err := exec.Command("mount", "-t", "udf", "-o", "loop,ro", p, mnt).CombinedOutput()
		if err != nil && strings.Contains(string(out), "unknown filesystem type") {
			t.Skipf("this kernel has no UDF driver: %s", out)
		}
		if err != nil {
			t.Fatalf("mount: %v: %s", err, out)
		}
		return nil
	}
	umount := func() { _ = exec.Command("umount", mnt).Run() }
	t.Cleanup(func() { _ = exec.Command("umount", "-l", mnt).Run() })

	_ = mount()
	_, before := os.Stat(filepath.Join(mnt, "BDMV", "index.bdmv"))
	t.Logf("before repair, BDMV/index.bdmv: %v (an error is expected on kernels with udf_verify_fi)", before)
	umount()

	if n, err := Repair(p); err != nil || n == 0 {
		t.Fatalf("Repair = %d, %v", n, err)
	}

	_ = mount()
	defer umount()
	st, err := os.Stat(filepath.Join(mnt, "BDMV", "index.bdmv"))
	if err != nil || !st.Mode().IsRegular() {
		t.Fatalf("after repair, BDMV/index.bdmv: %v", err)
	}
}
