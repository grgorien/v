package mru

import (
	"os"
	"strings"
	"testing"

	"v/internal/config"
)

func TestTouchOrderAndCap(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	if err := Touch("a"); err != nil {
		t.Fatal(err)
	}
	if err := Touch("b"); err != nil {
		t.Fatal(err)
	}
	if err := Touch("a"); err != nil {
		t.Fatal(err)
	}
	got := Load()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %v", got)
	}
	for i := 0; i < config.MRUCap+5; i++ {
		if err := Touch("n" + strings.Repeat("x", i)); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(Load()); n != config.MRUCap {
		t.Fatalf("cap: got %d want %d", n, config.MRUCap)
	}
	if _, err := os.Stat(config.MRUPath()); err != nil {
		t.Fatalf("mru file missing: %v", err)
	}
}
