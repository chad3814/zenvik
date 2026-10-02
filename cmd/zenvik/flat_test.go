package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chad3814/zenvik/internal/testdisc"
)

func writeFlat(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "FLAT_MOVIE")
	if err := testdisc.SampleMovie().WriteDir(root); err != nil {
		t.Fatal(err)
	}
	if err := testdisc.Flatten(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestInfoFlat(t *testing.T) {
	root := writeFlat(t)
	code, out, errOut := runCLI("info", root)
	if code != 0 || errOut != "" {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if !strings.Contains(strings.SplitN(out, "\n", 2)[0], "(BDMV folder (flattened): ") {
		t.Errorf("header = %q", strings.SplitN(out, "\n", 2)[0])
	}
	if main := titleRow(out, "★"); !strings.Contains(main, "00800") {
		t.Errorf("main row = %q", main)
	}

	code, out, _ = runCLI("info", "--json", filepath.Join(root, "BDMV"))
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	var disc struct{ Kind, Main string }
	if err := json.Unmarshal([]byte(out), &disc); err != nil {
		t.Fatal(err)
	}
	if disc.Kind != "bdmv_flat" || disc.Main != "00800" {
		t.Errorf("kind %q, main %q", disc.Kind, disc.Main)
	}
}
