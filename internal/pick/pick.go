// fzf-driven pickers and prompts.
package pick

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"v/internal/config"
	"v/internal/proj"
	"v/internal/sess"
)

// Row is one entry in the picker.
type Row struct {
	Kind    string // "live" or "new"
	Index   int
	Session *sess.Session
	Repo    *proj.Repo
}

// Action is the requested operation.
type Action int

const (
	ActAttach Action = iota
	ActKill
	ActNewRole
	ActMove
	ActCancel
)

// Result is the picker outcome.
type Result struct {
	Action Action
	Rows   []Row
}

// BuildRows assembles live and dormant rows per section 9.2.
func BuildRows(sessions []sess.Session, repos []proj.Repo, mruOrder []string, current string, liveOnly bool) []Row {
	rank := map[string]int{}
	for i, n := range mruOrder {
		if _, ok := rank[n]; !ok {
			rank[n] = i
		}
	}
	live := make([]sess.Session, len(sessions))
	copy(live, sessions)
	sort.SliceStable(live, func(i, j int) bool {
		ri, iok := rank[live[i].Name]
		rj, jok := rank[live[j].Name]
		if iok != jok {
			return iok
		}
		if iok && jok && ri != rj {
			return ri < rj
		}
		if live[i].Project != live[j].Project {
			return live[i].Project < live[j].Project
		}
		if live[i].Role != live[j].Role {
			return live[i].Role < live[j].Role
		}
		return live[i].Name < live[j].Name
	})
	var rows []Row
	for i := range live {
		s := live[i]
		rows = append(rows, Row{Kind: "live", Index: len(rows), Session: &s})
	}
	if current != "" && len(rows) > 1 && rows[0].Session != nil && rows[0].Session.Name == current {
		rows[0], rows[1] = rows[1], rows[0]
	}
	if liveOnly {
		for i := range rows {
			rows[i].Index = i
		}
		return rows
	}
	coveredRoot := map[string]bool{}
	coveredProject := map[string]bool{}
	for _, s := range live {
		if s.Root != "" {
			coveredRoot[s.Root] = true
		}
		if s.Project != "" {
			coveredProject[proj.CleanPart(s.Project)] = true
		}
	}
	dormant := make([]proj.Repo, 0, len(repos))
	for _, r := range repos {
		if coveredRoot[r.Path] || coveredProject[proj.CleanPart(r.Name)] {
			continue
		}
		dormant = append(dormant, r)
	}
	sort.SliceStable(dormant, func(i, j int) bool { return dormant[i].Name < dormant[j].Name })
	for i := range dormant {
		r := dormant[i]
		rows = append(rows, Row{Kind: "new", Index: len(rows), Repo: &r})
	}
	for i := range rows {
		rows[i].Index = i
	}
	return rows
}

func marker(r Row) string {
	if r.Kind == "live" && r.Session != nil {
		switch {
		case r.Session.Busy:
			return "!"
		case r.Session.UIs > 0:
			return "@"
		default:
			return "*"
		}
	}
	return "+"
}

func rowFields(r Row) (project, role, ports, jobs, mod, cwd string) {
	if r.Kind == "live" && r.Session != nil {
		s := r.Session
		project = s.Project
		role = s.Role
		if len(s.Ports) == 0 {
			ports = "-"
		} else {
			parts := make([]string, len(s.Ports))
			for i, p := range s.Ports {
				parts[i] = strconv.Itoa(p)
			}
			ports = strings.Join(parts, ",")
		}
		jobs = fmt.Sprintf("%d/%d", s.JobsRunning, s.JobsExited)
		mod = strconv.Itoa(s.Modified)
		cwd = s.Cwd
	} else if r.Repo != nil {
		project = r.Repo.Name
		role = "dev"
		ports = "-"
		jobs = "-"
		mod = "-"
		cwd = r.Repo.Path
	}
	return
}

func cleanField(s string) string {
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// FormatLines returns DISPLAY<TAB>INDEX lines and the header.
func FormatLines(rows []Row) (lines []string, header string) {
	type cols struct{ marker, project, role, ports, jobs, mod, cwd string }
	all := make([]cols, len(rows))
	wm, wp, wr, wports, wj, wmod := 1, 0, 0, 1, 1, 1
	head := cols{marker: " ", project: "project", role: "role", ports: "ports", jobs: "jobs", mod: "mod", cwd: "cwd"}
	for i, r := range rows {
		p, ro, po, j, m, c := rowFields(r)
		c2 := cols{
			marker:  marker(r),
			project: cleanField(p),
			role:    cleanField(ro),
			ports:   cleanField(po),
			jobs:    cleanField(j),
			mod:     cleanField(m),
			cwd:     cleanField(c),
		}
		all[i] = c2
		if len(c2.marker) > wm {
			wm = len(c2.marker)
		}
		if len(c2.project) > wp {
			wp = len(c2.project)
		}
		if len(c2.role) > wr {
			wr = len(c2.role)
		}
		if len(c2.ports) > wports {
			wports = len(c2.ports)
		}
		if len(c2.jobs) > wj {
			wj = len(c2.jobs)
		}
		if len(c2.mod) > wmod {
			wmod = len(c2.mod)
		}
	}
	if len(head.project) > wp {
		wp = len(head.project)
	}
	if len(head.role) > wr {
		wr = len(head.role)
	}
	if len(head.ports) > wports {
		wports = len(head.ports)
	}
	if len(head.jobs) > wj {
		wj = len(head.jobs)
	}
	if len(head.mod) > wmod {
		wmod = len(head.mod)
	}
	pad := func(s string, n int) string {
		if len(s) >= n {
			return s
		}
		return s + strings.Repeat(" ", n-len(s))
	}
	header = "  " + pad(head.project, wp) + " " + pad(head.role, wr) + " " +
	pad(head.ports, wports) + " " + pad(head.jobs, wj) + " " + pad(head.mod, wmod) + " cwd"
	for i, c := range all {
		display := pad(c.marker, wm) + " " + pad(c.project, wp) + " " + pad(c.role, wr) + " " +
		pad(c.ports, wports) + " " + pad(c.jobs, wj) + " " + pad(c.mod, wmod) + " " + c.cwd
		lines = append(lines, display+"\t"+strconv.Itoa(rows[i].Index))
	}
	return lines, header
}

// ParseFzfOutput splits fzf output into the pressed key (when expect is set)
// and the selected lines.
func ParseFzfOutput(out string, expect bool) (key string, lines []string) {
	parts := strings.Split(out, "\n")
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if expect {
		if len(parts) == 0 {
			return "", nil
		}
		key = parts[0]
		return key, parts[1:]
	}
	return "", parts
}

func keyToAction(key string) Action {
	switch key {
	case "":
		return ActAttach
	case "ctrl-x":
		return ActKill
	case "ctrl-n":
		return ActNewRole
	case "alt-m":
		return ActMove
	}
	return ActCancel
}

// Run invokes fzf over rows and parses the selection.
func Run(rows []Row, header string, multi bool, hubKeys bool) (Result, error) {
	lines, hdr := FormatLines(rows)
	if header == "" {
		header = hdr
	}
	args := []string{
		"--delimiter=\t",
		"--with-nth=1",
		"--tiebreak=index",
		"--prompt=v> ",
		"--header=" + header,
	}
	if multi {
		args = append(args, "--multi")
	}
	if hubKeys {
		args = append(args, "--expect=ctrl-x,ctrl-n,alt-m")
	}
	cmd := exec.Command(config.FzfBin, args...)
	cmd.Stdin = strings.NewReader(strings.Join(lines, "\n") + "\n")
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGQUIT)
	defer signal.Stop(sigCh)
	err := cmd.Run()
	signal.Stop(sigCh)
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code := ee.ExitCode()
			if code == 1 || code == 130 {
				return Result{Action: ActCancel}, nil
			}
			return Result{}, fmt.Errorf("fzf failed (exit %d)", code)
		}
		return Result{}, err
	}
	key, sel := ParseFzfOutput(out.String(), hubKeys)
	act := keyToAction(key)
	byIndex := map[int]Row{}
	for _, r := range rows {
		byIndex[r.Index] = r
	}
	var picked []Row
	for _, line := range sel {
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		idx, err := strconv.Atoi(fields[len(fields)-1])
		if err != nil {
			continue
		}
		if r, ok := byIndex[idx]; ok {
			picked = append(picked, r)
		}
	}
	if act == ActAttach && len(picked) == 0 {
		act = ActCancel
	}
	return Result{Action: act, Rows: picked}, nil
}

// PickProject prompts for one of names via fzf, with a "+ new project" entry.
func PickProject(names []string) (string, bool, error) {
	opts := append([]string{"+ new project"}, names...)
	cmd := exec.Command(config.FzfBin,
	"--prompt=project> ",
)
cmd.Stdin = strings.NewReader(strings.Join(opts, "\n") + "\n")
var out strings.Builder
cmd.Stdout = &out
cmd.Stderr = os.Stderr
sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, syscall.SIGINT, syscall.SIGQUIT)
err := cmd.Run()
signal.Stop(sigCh)
if err != nil {
	if ee, ok := err.(*exec.ExitError); ok {
		code := ee.ExitCode()
		if code == 1 || code == 130 {
			return "", false, nil
		}
		return "", false, fmt.Errorf("fzf failed (exit %d)", code)
	}
	return "", false, err
}
line := strings.TrimSpace(out.String())
if line == "" {
	return "", false, nil
}
if line == "+ new project" {
	name, perr := Prompt("project: ")
	if perr != nil {
		return "", false, perr
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false, nil
	}
	return name, true, nil
}
return line, true, nil
}

// Prompt prints label to stderr and reads one line from stdin.
func Prompt(label string) (string, error) {
	fmt.Fprint(os.Stderr, label)
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
