package main

import "testing"

func TestVersionDefaultsToDev(t *testing.T) {
	if got := NewApp(Deps{}).Version(); got != "dev" {
		t.Errorf("Version() = %q, want dev", got)
	}
}
