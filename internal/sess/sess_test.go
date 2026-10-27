package sess

import (
	"os"
	"path/filepath"
	"testing"

	"v/internal/config"
)

func TestResolveIdentity(t *testing.T) {
	cases := []struct {
		name                string
		managed             bool
		vp, vr, vroot, cwd  string
		wantP, wantR, wantRt string
	}{
		{"proj.srv", true, "shop", "pi", "/r", "/c", "shop", "pi", "/r"},
		{"proj.srv", true, "", "", "", "/c", "proj", "srv", "/c"},
		{"main", true, "", "", "", "/c", "main", "dev", "/c"},
		{"other", false, "", "", "", "/c/repo", "repo", "-", "/c/repo"},
		{"x", false, "p", "r", "/root", "/c", "p", "r", "/root"},
	}
	for _, c := range cases {
		p, r, rt := ResolveIdentity(c.name, c.managed, c.vp, c.vr, c.vroot, c.cwd)
		if p != c.wantP || r != c.wantR || rt != c.wantRt {
			t.Errorf("%s: got %q,%q,%q want %q,%q,%q", c.name, p, r, rt, c.wantP, c.wantR, c.wantRt)
		}
	}
}

func TestParseFileTarget(t *testing.T) {
	dir := t.TempDir()
	weird := filepath.Join(dir, "a:12")
	if err := os.WriteFile(weird, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		in   string
		want FileTarget
	}{
		{"a.go", FileTarget{Path: "a.go"}},
		{"a.go:12", FileTarget{Path: "a.go", Line: 12}},
		{"a.go:12:3", FileTarget{Path: "a.go", Line: 12, Col: 3}},
		{weird, FileTarget{Path: weird}},
	}
	for _, c := range cases {
		if got := ParseFileTarget(c.in); got != c.want {
			t.Errorf("ParseFileTarget(%q) = %#v want %#v", c.in, got, c.want)
		}
	}
}

func TestNameFromSock(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	inside := filepath.Join(config.SockDir(), "main.sock")
	if got := NameFromSock(inside); got != "main" {
		t.Errorf("inside: got %q", got)
	}
	if got := NameFromSock("/tmp/other.sock"); got != "other.sock" {
		t.Errorf("outside: got %q", got)
	}
}
