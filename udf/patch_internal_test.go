package udf

import "testing"

func TestImageOffsetPast4GiB(t *testing.T) {
	f := &FS{size: 1 << 40, parts: []partition{{number: 0, start: 3_000_000, length: 10_000_000}}}
	got, err := f.imageOffset(0, 2_500_000, 5)
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(5_500_000)*sectorSize + 5; got != want {
		t.Fatalf("imageOffset = %d, want %d", got, want)
	}
	if _, err := f.imageOffset(0, 10_000_000, 0); err == nil {
		t.Fatal("offset past the partition: want an error")
	}
	if _, err := f.imageOffset(1, 0, 0); err == nil {
		t.Fatal("unknown partition reference: want an error")
	}
}
