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

func TestFormatLinesMarkers(t *testing.T) {
	rows := []Row{
		{Kind: "live", Index: 0, Session: &sess.Session{Name: "s", Project: "p", Role: "dev", UIs: 1}},
		{Kind: "live", Index: 1, Session: &sess.Session{Name: "b", Project: "p", Role: "dev", Busy: true}},
		{Kind: "live", Index: 2, Session: &sess.Session{Name: "x", Project: "p", Role: "dev"}},
		{Kind: "new", Index: 3, Repo: &proj.Repo{Name: "r", Path: "/r"}},
	}
	lines, header := FormatLines(rows)
	if header == "" {
		t.Fatal("empty header")
	}
	if len(lines) != 4 {
		t.Fatalf("got %d lines", len(lines))
	}
	wants := []string{"@", "!", "*", "+"}
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
