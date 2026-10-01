package testdisc

import "github.com/chad3814/zenvik/internal/testdisc/udfimage"

// ISO returns the disc as a UDF image built by udfimage.
func (d *Disc) ISO(opt udfimage.Options) ([]byte, error) {
	files := map[string]udfimage.File{}
	for name, data := range d.Files() {
		files[name] = udfimage.File{Data: data}
	}
	return udfimage.Build(files, opt)
}
