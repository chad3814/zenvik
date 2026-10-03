package queue

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type stored struct {
	Version int     `json:"version"`
	Paused  bool    `json:"paused"`
	Entries []Entry `json:"entries"`
}

var errCorrupt = errors.New("corrupt queue file")

func load(path string) (stored, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return stored{}, err
	}
	var st stored
	if err := json.Unmarshal(b, &st); err != nil {
		return stored{}, fmt.Errorf("%w: %v", errCorrupt, err)
	}
	if st.Version != 1 {
		return stored{}, fmt.Errorf("%w: version %d", errCorrupt, st.Version)
	}
	for _, e := range st.Entries {
		if e.ID == "" || !validState(e.State) || !filepath.IsAbs(e.OutputPath) {
			return stored{}, fmt.Errorf("%w: bad entry %q", errCorrupt, e.ID)
		}
	}
	return st, nil
}

// save writes st to a temp file beside path and renames it into place, so a
// crash never leaves a half-written queue.
func save(path string, st stored) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".gui-queue-*.json")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func validState(s State) bool {
	switch s {
	case Waiting, Ripping, Done, Failed, Canceled:
		return true
	}
	return false
}
