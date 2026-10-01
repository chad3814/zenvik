//go:build windows

package mount

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestAttachWindows(t *testing.T) {
	var calls []string
	oldRun, oldLook := runner, lookPath
	t.Cleanup(func() { runner, lookPath = oldRun, oldLook })
	lookPath = func(string) (string, error) { return `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, nil }
	outputs := []string{"F\r\n", ""}
	runner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		out := outputs[0]
		outputs = outputs[1:]
		return []byte(out), nil
	}
	m, err := Attach(context.Background(), `C:\Movies\Bob's.iso`)
	if err != nil || m.Dir != `F:\` {
		t.Fatalf("Attach = %+v, %v", m, err)
	}
	if !strings.Contains(calls[0], `$i = Mount-DiskImage -ImagePath 'C:\Movies\Bob''s.iso' -Access ReadOnly -PassThru; try { ($i | Get-Volume).DriveLetter } catch { Dismount-DiskImage -ImagePath 'C:\Movies\Bob''s.iso' | Out-Null; throw }`) {
		t.Errorf("mount call = %s", calls[0])
	}
	if err := m.Detach(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(calls[1], `Dismount-DiskImage -ImagePath 'C:\Movies\Bob''s.iso'`) {
		t.Errorf("dismount call = %s", calls[1])
	}
	outputs = []string{"G\r\n"}
	if _, err := Attach(context.Background(), `rel\x.iso`); err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`-ImagePath '([^']*)'`)
	if sub := re.FindStringSubmatch(calls[len(calls)-1]); sub == nil || !filepath.IsAbs(sub[1]) {
		t.Errorf("relative input not made absolute: %s", calls[len(calls)-1])
	}
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	if _, err := Attach(context.Background(), `C:\x.iso`); !errors.Is(err, ErrUnavailable) {
		t.Errorf("no powershell: err = %v", err)
	}
}
