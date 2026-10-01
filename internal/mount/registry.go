package mount

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Record describes a mount zenvik made. It is kept on disk from Attach
// until a successful Detach, so mounts left behind by a crash can be found.
type Record struct {
	Image   string    `json:"image"`
	Dir     string    `json:"dir"`
	Device  string    `json:"device,omitempty"`
	PID     int       `json:"pid"`
	Created time.Time `json:"created"`
	Path    string    `json:"-"` // record file; set by Leftovers
}

// Cleanup returns the command that removes this mount by hand, or "" when
// mounting is not supported on this OS.
func (r Record) Cleanup() string { return cleanupCommand(r) }

// stateDir is where mount records live: $XDG_STATE_HOME/zenvik/mounts, or
// the user cache directory's zenvik/mounts.
func stateDir() (string, error) {
	if x := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(x) {
		return filepath.Join(x, "zenvik", "mounts"), nil
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "zenvik", "mounts"), nil
}

// register writes r and returns the record's file path.
func register(r Record) (string, error) {
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, fmt.Sprintf("%d-*.tmp", os.Getpid()))
	if err != nil {
		return "", err
	}
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	p := strings.TrimSuffix(tmp.Name(), ".tmp") + ".json"
	if err := os.Rename(tmp.Name(), p); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	return p, nil
}

// Leftovers returns recorded mounts whose zenvik process is no longer
// running, oldest first. Unreadable record files are skipped.
func Leftovers() ([]Record, error) {
	dir, err := stateDir()
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, e := range ents {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var r Record
		if json.Unmarshal(b, &r) != nil || r.Dir == "" || processAlive(r.PID) {
			continue
		}
		r.Path = filepath.Join(dir, e.Name())
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out, nil
}
