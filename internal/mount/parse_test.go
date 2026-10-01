package mount

import "testing"

func TestParseUdisksLoop(t *testing.T) {
	for in, want := range map[string]string{
		"Mapped file /tmp/a b.iso as /dev/loop7.\n": "/dev/loop7",
		"Mapped file x.iso as /dev/loop12\n":        "/dev/loop12",
	} {
		if got, err := parseUdisksLoop(in); err != nil || got != want {
			t.Errorf("parseUdisksLoop(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := parseUdisksLoop("Error setting up loop device"); err == nil {
		t.Error("expected an error for unrecognized output")
	}
}

func TestParseUdisksMount(t *testing.T) {
	for in, want := range map[string]string{
		"Mounted /dev/loop7 at /media/chad/MY DISC\n": "/media/chad/MY DISC",
		"Mounted /dev/loop7 at /run/media/c/DISC.\n":  "/run/media/c/DISC",
	} {
		if got, err := parseUdisksMount(in); err != nil || got != want {
			t.Errorf("parseUdisksMount(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := parseUdisksMount("nothing"); err == nil {
		t.Error("expected an error for unrecognized output")
	}
}

func TestParseUdisksInfoMountPoint(t *testing.T) {
	out := "/org/freedesktop/UDisks2/block_devices/loop7:\n  org.freedesktop.UDisks2.Filesystem:\n    MountPoints:        /media/chad/MY DISC\n    Size:               123\n"
	if got, err := parseUdisksInfoMountPoint(out); err != nil || got != "/media/chad/MY DISC" {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := parseUdisksInfoMountPoint("    MountPoints:\n"); err == nil {
		t.Error("expected an error when not mounted")
	}
}

func TestParseDriveLetter(t *testing.T) {
	if got, err := parseDriveLetter("e\r\n"); err != nil || got != `E:\` {
		t.Errorf("got %q, %v", got, err)
	}
	for _, bad := range []string{"", "EF", "1"} {
		if _, err := parseDriveLetter(bad); err == nil {
			t.Errorf("parseDriveLetter(%q) should fail", bad)
		}
	}
}

func TestPSQuote(t *testing.T) {
	if got := psQuote(`C:\Movies\Bob's "Disc".iso`); got != `'C:\Movies\Bob''s "Disc".iso'` {
		t.Errorf("psQuote = %s", got)
	}
	for _, q := range []string{"\u2018", "\u2019", "\u201A", "\u201B"} {
		if got, want := psQuote("Bob"+q+"s.iso"), "'Bob"+q+q+"s.iso'"; got != want {
			t.Errorf("psQuote with %U = %s, want %s", []rune(q)[0], got, want)
		}
	}
}
