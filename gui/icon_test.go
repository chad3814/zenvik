package main

import (
	"bytes"
	"image/png"
	"testing"
)

// The embedded icon is what the Linux window shows; macOS and Windows build
// their icons from the same file (build/appicon.png).
func TestAppIconIsSquareWithTransparentCorners(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(appIcon))
	if err != nil {
		t.Fatalf("decode embedded icon: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != 1024 || b.Dy() != 1024 {
		t.Fatalf("icon is %dx%d, want 1024x1024", b.Dx(), b.Dy())
	}
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
		t.Errorf("corner alpha = %d, want 0 (rounded tile on a transparent canvas)", a)
	}
	if _, _, _, a := img.At(512, 512).RGBA(); a != 0xffff {
		t.Errorf("centre alpha = %d, want opaque", a)
	}
}
