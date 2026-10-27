//go:build integration

package spawn


import (
	"os/exec"
	"testing"
	"time"

	"v/internal/sess"
)

func TestSpawnLifecycle(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not on PATH")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := t.TempDir()
	sock, pid, err := Start(Spec{
		Project:  "testproj",
		Role:     "dev",
		Root:     root,
		Terminal: true,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("bad pid %d", pid)
	}
	deadline := time.Now().Add(3 * time.Second)
	var found bool
	for time.Now().Before(deadline) {
		sessions, err := sess.DiscoverAll(500 * time.Millisecond)
		if err == nil {
			for _, s := range sessions {
				if s.Sock == sock {
					found = true
					if s.Project != "testproj" || s.Role != "dev" {
						t.Fatalf("identity: %q/%q", s.Project, s.Role)
					}
					if s.UIs != 0 {
						t.Fatalf("expected 0 UIs, got %d", s.UIs)
					}
				}
			}
		}
		if found {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !found {
		t.Fatal("session not discovered")
	}
	if err := sess.SetIdentity(sock, "testproj", "srv"); err != nil {
		t.Fatalf("SetIdentity: %v", err)
	}
	pr, err := sess.Probe(sock, time.Second)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if pr.Role != "srv" {
		t.Fatalf("role not updated: %q", pr.Role)
	}
	if err := Kill(sock, pid); err != nil {
		t.Fatalf("Kill: %v", err)
	}
}
