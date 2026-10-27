package pick

import (
	"strings"
	"testing"

	"v/internal/proj"
	"v/internal/sess"
)

func TestBuildRowsOrderAndSwap(t *testing.T) {
	sessions := []sess.Session{
		{Name: "b", Project: "pb", Role: "dev", Root: "/b"},
		{Name: "a", Project: "pa", Role: "dev", Root: "/a"},
		{Name: "c", Project: "pc", Role: "dev", Root: "/c"},
	}
	rows := BuildRows(sessions, nil, []string{"b", "c"}, "b", false)
	if len(rows) != 3 {
		t.Fatalf("got %d rows", len(rows))
	}
	if rows[0].Session.Name != "c" || rows[1].Session.Name != "b" {
		t.Fatalf("swap rule failed: %q %q", rows[0].Session.Name, rows[1].Session.Name)
	}
	if rows[2].Session.Name != "a" {
		t.Fatalf("unknown should come last: %q", rows[2].Session.Name)
	}
}

func TestBuildRowsDormantExclusion(t *testing.T) {
	sessions := []sess.Session{
		{Name: "shop", Project: "shop", Role: "dev", Root: "/wrk/shop"},
	}
	repos := []proj.Repo{
		{Name: "shop", Path: "/wrk/shop"},
		{Name: "other", Path: "/wrk/other"},
		{Name: "third", Path: "/else/third"},
	}
	rows := BuildRows(sessions, repos, nil, "", false)
	var newNames []string
	for _, r := range rows {
		if r.Kind == "new" {
			newNames = append(newNames, r.Repo.Name)
		}
	}
	if len(newNames) != 2 {
		t.Fatalf("got %v", newNames)
	}
	if newNames[0] != "other" || newNames[1] != "third" {
		t.Fatalf("order: %v", newNames)
	}
}

func TestBuildRowsFoldsDuplicates(t *testing.T) {
	// The real carnis.srv lives at the managed path. Its fzf-lua twin is
	// unmanaged and carries the same identity. The folder must pick the
	// managed one and expose exactly one row.
	managed := sess.Session{
		Name: "carnis.srv", Project: "carnis", Role: "srv",
		Root: "/home/xi/.wrk/carnis", Sock: "/state/v/sockets/carnis.srv.sock",
		Managed: true, UIs: 0, Pid: 4242, JobsRunning: 1,
	}
	twin := sess.Session{
		Name: "fzf-lua.1793077903.3016616.0", Project: "carnis", Role: "srv",
		Root: "/home/xi/.wrk/carnis", Sock: "/tmp/fzf-lua.1793077903.3016616.0",
		Managed: false, UIs: 0, Pid: 999, JobsRunning: 1,
	}
	rows := BuildRows([]sess.Session{twin, managed}, nil, nil, "", false)
	if len(rows) != 1 {
		t.Fatalf("expected 1 folded row, got %d", len(rows))
	}
	if rows[0].Session.Name != "carnis.srv" {
		t.Fatalf("fold picked %q", rows[0].Session.Name)
	}
}

func TestBuildRowsFoldPrefersNoUIsThenShortName(t *testing.T) {
	// Same identity, both unmanaged, both UIs == 0: the shorter name wins.
	a := sess.Session{Name: "short", Project: "p", Role: "dev", Root: "/r"}
	b := sess.Session{Name: "a-much-longer-name", Project: "p", Role: "dev", Root: "/r"}
	rows := BuildRows([]sess.Session{b, a}, nil, nil, "", false)
	if len(rows) != 1 || rows[0].Session.Name != "short" {
		t.Fatalf("got %+v", rows)
	}
}

func TestBuildRowsKeepsStrangersSeparate(t *testing.T) {
	// An unmanaged session with no resolvable project must not be folded
	// with another stranger, even if role and root match.
	a := sess.Session{Name: "stranger-a", Role: "-", Root: "/r"}
	b := sess.Session{Name: "stranger-b", Role: "-", Root: "/r"}
	rows := BuildRows([]sess.Session{a, b}, nil, nil, "", false)
	if len(rows) != 2 {
		t.Fatalf("strangers must not fold, got %d rows", len(rows))
	}
}

func TestFormatLinesMarkers(t *testing.T) {
	rows := []Row{
		{Kind: "live", Index: 0, Session: &sess.Session{Name: "s", Project: "p", Role: "dev", UIs: 1, Managed: true}},
		{Kind: "live", Index: 1, Session: &sess.Session{Name: "b", Project: "p", Role: "dev", Busy: true}},
		{Kind: "live", Index: 2, Session: &sess.Session{Name: "x", Project: "p", Role: "dev", Managed: true}},
		{Kind: "live", Index: 3, Session: &sess.Session{Name: "u", Project: "p", Role: "dev", Managed: false}},
		{Kind: "new", Index: 4, Repo: &proj.Repo{Name: "r", Path: "/r"}},
	}
	lines, header := FormatLines(rows)
	if header == "" {
		t.Fatal("empty header")
	}
	if len(lines) != 5 {
		t.Fatalf("got %d lines", len(lines))
	}
	wants := []string{"@", "!", "*", "~", "+"}
	for i, want := range wants {
		disp := strings.SplitN(lines[i], "\t", 2)[0]
		if !strings.HasPrefix(disp, want) {
			t.Errorf("line %d marker: got %q want prefix %q", i, disp, want)
		}
		fields := strings.Split(lines[i], "\t")
		if fields[len(fields)-1] != string(rune('0'+i)) {
			t.Errorf("line %d index: got %q", i, fields[len(fields)-1])
		}
	}
}

func TestParseFzfOutput(t *testing.T) {
	key, lines := ParseFzfOutput("\nfoo\t0\n", true)
	if key != "" || len(lines) != 1 || lines[0] != "foo\t0" {
		t.Fatalf("enter: %q %v", key, lines)
	}
	key, lines = ParseFzfOutput("ctrl-x\nfoo\t0\nbar\t1\n", true)
	if key != "ctrl-x" || len(lines) != 2 {
		t.Fatalf("ctrl-x: %q %v", key, lines)
	}
	key, lines = ParseFzfOutput("foo\t0\n", false)
	if key != "" || len(lines) != 1 {
		t.Fatalf("no expect: %q %v", key, lines)
	}
	key, lines = ParseFzfOutput("", false)
	if key != "" || len(lines) != 0 {
		t.Fatalf("empty: %q %v", key, lines)
	}
}
