package proj

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSanitize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"main", "main"},
		{"a b", "a_b"},
		{"héllo", "h_llo"},
		{"a.b-c_d", "a.b-c_d"},
		{"", ""},
	}
	for _, c := range cases {
		if got := Sanitize(c.in); got != c.want {
			t.Errorf("Sanitize(%q) = %q want %q", c.in, got, c.want)
		}
	}
}

func TestValidName(t *testing.T) {
	good := []string{"a", "abc", "a-b_c", "A1"}
	bad := []string{"", "a.b", "a b", "a/b", "-x!"}
	for _, g := range good {
		if !ValidName(g) {
			t.Errorf("ValidName(%q) = false", g)
		}
	}
	for _, b := range bad {
		if ValidName(b) {
			t.Errorf("ValidName(%q) = true", b)
		}
	}
}

func TestCleanPart(t *testing.T) {
	if got := CleanPart("a.b c"); got != "a_b_c" {
		t.Errorf("CleanPart = %q", got)
	}
}

func TestSplitJoinName(t *testing.T) {
	cases := []struct {
		name    string
		project string
		role    string
	}{
		{"main", "main", "dev"},
		{"main.srv", "main", "srv"},
		{"kanam_", "kanam_", "dev"},
	}
	for _, c := range cases {
		p, r := SplitName(c.name)
		if p != c.project || r != c.role {
			t.Errorf("SplitName(%q) = %q,%q want %q,%q", c.name, p, r, c.project, c.role)
		}
		if got := JoinName(c.project, c.role); got != c.name {
			t.Errorf("JoinName(%q,%q) = %q want %q", c.project, c.role, got, c.name)
		}
	}
}

func TestMatchPattern(t *testing.T) {
	cases := []struct {
		pattern, rel string
		want         bool
	}{
		{"*.go", "a/b.go", true},
		{"*.go", "a/b.c", false},
		{"**/*.go", "b.go", true},
		{"**/*.go", "a/b/c.go", true},
		{"src/**/x.go", "src/x.go", true},
		{"src/**/x.go", "src/a/b/x.go", true},
		{"src/**/x.go", "src/a/b/y.go", false},
		{"a/*/c", "a/b/c", true},
		{"a/*/c", "a/b/d/c", false},
	}
	for _, c := range cases {
		if got := MatchPattern(c.pattern, c.rel); got != c.want {
			t.Errorf("MatchPattern(%q,%q) = %v want %v", c.pattern, c.rel, got, c.want)
		}
	}
}

func TestGitRootProjectName(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "myrepo")
	sub := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := GitRoot(sub); got != repo {
		t.Errorf("GitRoot = %q want %q", got, repo)
	}
	if got := ProjectName(sub); got != "myrepo" {
		t.Errorf("ProjectName = %q", got)
	}
	// .git as a file (worktree).
	repo2 := filepath.Join(dir, "wt")
	if err := os.MkdirAll(repo2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo2, ".git"), []byte("gitdir: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := GitRoot(repo2); got != repo2 {
		t.Errorf("GitRoot file = %q", got)
	}
}

func TestDiscoverReposDepth(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Join(p, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mk("r1")       // .git at depth 1: counts
	mk("r2/n1")    // .git at depth 2: counts
	mk("r3/n1/n2") // .git at depth 3: counts (find -maxdepth 3)
	repos := DiscoverRepos([]string{root}, 3)
	paths := map[string]bool{}
	for _, r := range repos {
		paths[r.Path] = true
	}
	if !paths[filepath.Join(root, "r1")] {
		t.Errorf("missing depth-1 repo")
	}
	if !paths[filepath.Join(root, "r2", "n1")] {
		t.Errorf("missing depth-2 repo")
	}
	if !paths[filepath.Join(root, "r3", "n1", "n2")] {
		t.Errorf("missing depth-3 repo")
	}
	// At depth 2 the .git at depth 3 must not be reachable.
	shallow := DiscoverRepos([]string{root}, 2)
	for _, r := range shallow {
		if r.Path == filepath.Join(root, "r3", "n1", "n2") {
			t.Errorf("depth-3 repo must not appear at ProjectDepth 2")
		}
	}
}

func TestFindLoadFiles(t *testing.T) {
	dir := t.TempDir()
	files := []string{"a.go", "sub/b.go", ".git/hook.go", "c.txt"}
	for _, f := range files {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := FindLoadFiles(dir, "*.go", 40)
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	seen := map[string]bool{}
	for _, g := range got {
		seen[g] = true
	}
	if !seen["./a.go"] || !seen["./sub/b.go"] {
		t.Fatalf("got %v", got)
	}
	capped := FindLoadFiles(dir, "*.go", 1)
	if len(capped) != 1 {
		t.Fatalf("cap failed: %v", capped)
	}
}
