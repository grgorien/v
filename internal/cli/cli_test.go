package cli

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want *Opts
		err  bool
	}{
		{"empty", nil, &Opts{}, false},
		{"list", []string{"-l"}, &Opts{List: true}, false},
		{"json", []string{"-l", "--json"}, &Opts{List: true, JSON: true}, false},
		{"name space", []string{"-n", "x"}, &Opts{Name: "x"}, false},
		{"name eq", []string{"--name=x"}, &Opts{Name: "x"}, false},
		{"project role", []string{"-p", "p", "-r", "r"}, &Opts{Project: "p", Role: "r"}, false},
		{"load", []string{"--load", "*.go", "."}, &Opts{Load: "*.go", Args: []string{"."}}, false},
		{"double dash", []string{"--", "-l"}, &Opts{Args: []string{"-l"}}, false},
		{"interleave", []string{"-p", "p", "file", "-r", "r"}, &Opts{Project: "p", Role: "r", Args: []string{"file"}}, false},
		{"mv", []string{"--mv", "a", "b"}, &Opts{Move: []string{"a", "b"}}, false},
		{"unknown", []string{"--nope"}, nil, true},
		{"missing name", []string{"-n"}, nil, true},
		{"missing load", []string{"--load"}, nil, true},
		{"detached", []string{"--detached"}, &Opts{Detached: true}, false},
		{"startuptime", []string{"--startuptime"}, &Opts{Startuptime: true}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Parse(c.args)
			if c.err {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %#v want %#v", got, c.want)
			}
		})
	}
}
