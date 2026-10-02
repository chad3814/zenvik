package testdisc

import (
	"os"
	"path/filepath"
	"strings"
)

// Flatten rewrites the BDMV tree under root the way some releases ship it:
// the files of BDMV/PLAYLIST, BDMV/CLIPINF, BDMV/STREAM and BDMV/META/DL
// move to root, and their emptied directories are removed. Playlists and
// clip information files also get a BACKUP copy named "<id>.1.<ext>",
// BDMV/index.bdmv a copy named BDMV_index.bdmv, and BDMV/MovieObject.bdmv
// a copy named MovieObject.1.bdmv. BDMV/index.bdmv and BDMV/MovieObject.bdmv
// stay where they are.
func Flatten(root string) error {
	for _, sub := range []string{"PLAYLIST", "CLIPINF", "STREAM", "META/DL"} {
		dir := filepath.Join(root, "BDMV", filepath.FromSlash(sub))
		ents, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		for _, e := range ents {
			name := e.Name()
			if err := os.Rename(filepath.Join(dir, name), filepath.Join(root, name)); err != nil {
				return err
			}
			ext := filepath.Ext(name)
			if ext == ".mpls" || ext == ".clpi" {
				backup := strings.TrimSuffix(name, ext) + ".1" + ext
				if err := copyFile(filepath.Join(root, name), filepath.Join(root, backup)); err != nil {
					return err
				}
			}
		}
	}
	for _, d := range []string{"BDMV/PLAYLIST", "BDMV/CLIPINF", "BDMV/STREAM", "BDMV/META/DL", "BDMV/META"} {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(d))); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := copyFile(filepath.Join(root, "BDMV", "index.bdmv"), filepath.Join(root, "BDMV_index.bdmv")); err != nil {
		return err
	}
	return copyFile(filepath.Join(root, "BDMV", "MovieObject.bdmv"), filepath.Join(root, "MovieObject.1.bdmv"))
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}
