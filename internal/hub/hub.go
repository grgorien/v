// Interactive picker/attach loop.
package hub

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"v/internal/config"
	"v/internal/mru"
	"v/internal/pick"
	"v/internal/proj"
	"v/internal/sess"
	"v/internal/spawn"
)

// Options configures the hub.
type Options struct {
	LiveOnly bool
	Initial  string
}

// Run executes the hub loop and returns the process exit code.
func Run(o Options) int {
	st, err := os.Stdin.Stat()
	if err != nil || st.Mode()&os.ModeCharDevice == 0 {
		fmt.Fprintf(os.Stderr, "%s: error: hub requires a terminal\n", config.Prog())
		return 1
	}
	if _, ok := sess.InsideNvim(); ok {
		fmt.Fprintf(os.Stderr, "%s: error: cannot attach: already inside neovim\n", config.Prog())
		return 1
	}
	if err := os.MkdirAll(config.SockDir(), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "%s: error: %v\n", config.Prog(), err)
		return 1
	}
	if err := os.MkdirAll(config.LogDir(), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "%s: error: %v\n", config.Prog(), err)
		return 1
	}
	saved := saveStty()
	if o.Initial != "" {
		code, quit := attach(o.Initial, saved)
		if quit {
			return code
		}
	}
	current := o.Initial
	fails := 0
	for {
		sessions, _ := sess.DiscoverAll(config.ProbeTimeout)
		var repos []proj.Repo
		if !o.LiveOnly {
			repos = proj.DiscoverRepos(config.ProjectRoots, config.ProjectDepth)
		}
		rows := pick.BuildRows(sessions, repos, mru.Load(), current, o.LiveOnly)
		if len(rows) == 0 {
			fmt.Fprintf(os.Stderr, "%s: error: no neovim sockets found\n", config.Prog())
			return 1
		}
		_, header := pick.FormatLines(rows)
		res, err := pick.Run(rows, header, true, true)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: error: %v\n", config.Prog(), err)
			return 1
		}
		switch res.Action {
		case pick.ActCancel:
			return 0
		case pick.ActKill:
			for _, r := range res.Rows {
				if r.Kind != "live" || r.Session == nil {
					continue
				}
				_ = spawn.Kill(r.Session.Sock, r.Session.Pid)
			}
			continue
		case pick.ActNewRole:
			if len(res.Rows) == 0 {
				continue
			}
			name, err := newRole(res.Rows[0], sessions, repos)
			if err != nil {
				if errors.Is(err, spawn.ErrExists) {
					fmt.Fprintf(os.Stderr, "%s: error: %v\n", config.Prog(), err)
				} else {
					fmt.Fprintf(os.Stderr, "%s: error: %v\n", config.Prog(), err)
				}
				continue
			}
			code, quit := attachWithFastFail(name, saved, &fails)
			if quit {
				return code
			}
			current = name
			continue
		case pick.ActMove:
			moveRows(res.Rows, sessions, repos)
			continue
		case pick.ActAttach:
			if len(res.Rows) == 0 {
				continue
			}
			r := res.Rows[0]
			name := ""
			if r.Kind == "live" && r.Session != nil {
				name = r.Session.Name
			} else if r.Repo != nil {
				n, err := spawnRepo(r.Repo.Path)
				if err != nil {
					fmt.Fprintf(os.Stderr, "%s: error: %v\n", config.Prog(), err)
					continue
				}
				name = n
			}
			if name == "" {
				continue
			}
			code, quit := attachWithFastFail(name, saved, &fails)
			if quit {
				return code
			}
			current = name
			continue
		}
	}
}

func saveStty() string {
	cmd := exec.Command("stty", "-g")
	cmd.Stdin = os.Stdin
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func restoreStty(saved string) {
	if saved == "" {
		return
	}
	cmd := exec.Command("stty", saved)
	cmd.Stdin = os.Stdin
	_ = cmd.Run()
}

const resetSeq = "\x1b[?1049l\x1b[?25h\x1b[0m\x1b[?1000l\x1b[?1006l\x1b[?2004l"

func attach(name, saved string) (int, bool) {
	return attachWithFastFail(name, saved, nil)
}

func attachWithFastFail(name, saved string, fails *int) (int, bool) {
	s, ok := findSession(name)
	if !ok {
		fmt.Fprintf(os.Stderr, "%s: error: no session named %q\n", config.Prog(), name)
		return 1, false
	}
	_ = mru.Touch(name)
	start := time.Now()
	cmd := exec.Command(config.NvimBin, "--server", s.Sock, "--remote-ui")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	sigCh := make(chan os.Signal, 4)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGHUP, syscall.SIGTERM)
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		signal.Stop(sigCh)
		fmt.Fprintf(os.Stderr, "%s: error: %v\n", config.Prog(), err)
		return 1, false
	}
	go func() { done <- cmd.Wait() }()
	var sig syscall.Signal
	select {
	case err := <-done:
		signal.Stop(sigCh)
		restoreStty(saved)
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				fmt.Fprint(os.Stdout, resetSeq)
			}
		}
		if fails != nil {
			if time.Since(start) < config.FastFailWindow {
				*fails++
				if *fails >= config.FastFailLimit {
					fmt.Fprintf(os.Stderr, "%s: error: client exited immediately; is the server alive?\n", config.Prog())
					return 1, true
				}
			} else {
				*fails = 0
			}
		}
		return 0, false
	case sg := <-sigCh:
		if s2, ok := sg.(syscall.Signal); ok {
			sig = s2
		}
		_ = cmd.Process.Signal(sig)
		<-done
		signal.Stop(sigCh)
		restoreStty(saved)
		if sig == syscall.SIGHUP {
			return 129, true
		}
		if sig == syscall.SIGTERM {
			return 143, true
		}
		return 0, false
	}
}

func findSession(name string) (sess.Session, bool) {
	sessions, err := sess.DiscoverAll(config.ProbeTimeout)
	if err != nil {
		return sess.Session{}, false
	}
	for _, s := range sessions {
		if s.Name == name {
			return s, true
		}
	}
	return sess.Session{}, false
}

func spawnRepo(root string) (string, error) {
	project := proj.CleanPart(proj.ProjectName(root))
	role := config.DefaultRole
	name := project
	for i := 2; ; i++ {
		sock := spawn.SockPath(name)
		if _, err := os.Lstat(sock); err != nil {
			break
		}
		_, perr := sess.Probe(sock, config.ProbeTimeout)
		if perr == nil || errors.Is(perr, os.ErrPermission) {
			break
		}
		name = fmt.Sprintf("%s-%d", project, i)
		project = name
	}
	_, _, err := spawn.Start(spawn.Spec{Project: project, Role: role, Root: root, Terminal: true})
	if err != nil {
		return "", err
	}
	return name, nil
}

func newRole(r pick.Row, sessions []sess.Session, repos []proj.Repo) (string, error) {
	var project, root string
	if r.Kind == "live" && r.Session != nil {
		project = r.Session.Project
		root = r.Session.Root
	} else if r.Repo != nil {
		project = proj.CleanPart(r.Repo.Name)
		root = r.Repo.Path
	}
	if project == "" {
		return "", errors.New("no project for selected row")
	}
	for _, s := range sessions {
		if s.Project != project {
			continue
		}
		if s.Role == config.DefaultRole {
			root = s.Root
			break
		}
		if root == "" || r.Kind == "new" {
			root = s.Root
		}
	}
	role, err := pick.Prompt("role: ")
	if err != nil {
		return "", err
	}
	role = strings.TrimSpace(role)
	if role == "" {
		return "", errors.New("cancelled")
	}
	if !proj.ValidName(role) {
		return "", errors.New("invalid role")
	}
	name := proj.JoinName(project, role)
	_, _, err = spawn.Start(spawn.Spec{Project: project, Role: role, Root: root, Terminal: true})
	if err != nil {
		return "", err
	}
	return name, nil
}

func moveRows(rows []pick.Row, sessions []sess.Session, repos []proj.Repo) {
	known := map[string]bool{}
	for _, s := range sessions {
		if s.Project != "" {
			known[s.Project] = true
		}
	}
	for _, r := range repos {
		known[proj.CleanPart(r.Name)] = true
	}
	var names []string
	for n := range known {
		names = append(names, n)
	}
	sort.Strings(names)
	target, ok, err := pick.PickProject(names)
	if err != nil || !ok {
		return
	}
	if !proj.ValidName(target) {
		fmt.Fprintf(os.Stderr, "%s: error: invalid project\n", config.Prog())
		return
	}
	role, err := pick.Prompt("role (empty keeps current): ")
	if err != nil {
		return
	}
	role = strings.TrimSpace(role)
	if role != "" && !proj.ValidName(role) {
		fmt.Fprintf(os.Stderr, "%s: error: invalid role\n", config.Prog())
		return
	}
	for _, r := range rows {
		if r.Kind != "live" || r.Session == nil {
			continue
		}
		if err := sess.SetIdentity(r.Session.Sock, target, role); err != nil {
			fmt.Fprintf(os.Stderr, "%s: error: %v\n", config.Prog(), err)
		}
	}
}

func init() {
	_ = filepath.Separator
}
