package udf

import "os"

// Image is a UDF file system read from a disc image file.
type Image struct {
	*FS
	f *os.File
}

// OpenImage opens the disc image at name.
func OpenImage(name string) (*Image, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	fsys, err := Open(f, st.Size())
	if err != nil {
		f.Close()
		return nil, err
	}
	return &Image{FS: fsys, f: f}, nil
}

// Close closes the image file.
func (i *Image) Close() error { return i.f.Close() }
