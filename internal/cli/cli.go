package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"v/internal/config"
	"v/internal/hub"
	"v/internal/mru"
	"v/internal/nvrpc"
	"v/internal/pick"
	"v/internal/proj"
	"v/internal/sess"
	"v/internal/spawn"
)

// Opts holds parsed command-line options.
type Opts struct {
	Help, List, JSON, Kill, Jump, Attach, Detached, Startuptime bool
	Name, Project, Role, Load, To                               string
	Move                                                        []string
	Args                                                        []string
}

func errf(format string, a ...any) error {
	return fmt.Errorf(format, a...)
}

// Parse parses args (without the program name).
func Parse(args []string) (*Opts, error) {
	o := &Opts{}
	i := 0
	noMore := false
	valueFor := func(flag string, inline string, hasInline bool) (string, error) {
		if hasInline {
			return inline, nil
		}
		if i+1 >= len(args) {
			return "", errf("%s requires a value", flag)
		}
		i++
		return args[i], nil
	}
	for ; i < len(args); i++ {
		a := args[i]
		if noMore || a == "-" || !strings.HasPrefix(a, "-") {
			o.Args = append(o.Args, a)
			continue
		}
		if a == "--" {
			noMore = true
			continue
		}
		flag := a
		inline := ""
		hasInline := false
		if strings.HasPrefix(a, "--") {
			if eq := strings.IndexByte(a, '='); eq >= 0 {
				flag = a[:eq]
				inline = a[eq+1:]
				hasInline = true
			}
		}
		switch flag {
		case "-h", "--help":
			o.Help = true
		case "-l", "--list":
			o.List = true
		case "--json":
			o.JSON = true
		case "-k", "--kill":
			o.Kill = true
		case "-j", "--jump":
			o.Jump = true
		case "-a", "--attach":
			o.Attach = true
		case "--detached":
			o.Detached = true
		case "--startuptime":
			o.Startuptime = true
		case "-n", "--name":
			v, err := valueFor(flag, inline, hasInline)
			if err != nil {
				return nil, err
			}
			if v == "" {
				return nil, errf("%s requires a value", flag)
			}
			o.Name = v
		case "-p", "--project":
			v, err := valueFor(flag, inline, hasInline)
			if err != nil {
				return nil, err
			}
			o.Project = v
		case "-r", "--role":
			v, err := valueFor(flag, inline, hasInline)
			if err != nil {
				return nil, err
			}
			o.Role = v
		case "--load":
			v, err := valueFor(flag, inline, hasInline)
			if err != nil {
				return nil, err
			}
			if v == "" {
				return nil, errf("--load requires a pattern")
			}
			o.Load = v
		case "-t", "--to":
			v, err := valueFor(flag, inline, hasInline)
			if err != nil {
				return nil, err
			}
			o.To = v
		case "--mv":
			a1, err := valueFor(flag, inline, hasInline)
			if err != nil {
				return nil, err
			}
			if i+1 >= len(args) {
				return nil, errf("--mv requires two values")
			}
			i++
			o.Move = []string{a1, args[i]}
		default:
			return nil, errf("unknown option '%s'", a)
		}
	}
	return o, nil
}

func usage() string {
	return `usage: v [command] [options] [dir|files...]

	commands:
	(no args)              hub: pick a session and attach
	-j, --jump             hub with live and dormant rows
	-a, --attach           hub with live rows only
	-l, --list             list sessions (add --json for JSON)
	-k, --kill             pick sessions and force-quit them
	-n, --name NAME        new session named NAME (project or project.role)
	-p, --project P        project for a new session
	-r, --role R           role for a new session
	--load PATTERN [DIR]   new session with matching files preloaded
	-t, --to X             inside neovim: send files to session X
	--mv NAME TARGET       move a session to project or project.role
	--detached             create without attaching; print the socket path
	--startuptime          write a startup log under the state log dir
	-h, --help             this text
	`
}

func needTool(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// Main runs the CLI and returns the process exit code.
func Main(args []string) int {
	o, err := Parse(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: error: %v\n", config.Prog(), err)
		return 1
	}
	if o.Help {
		fmt.Fprint(os.Stdout, usage())
		return 0
	}
	insideSock, inside := sess.InsideNvim()
	if inside {
		_ = insideSock
		return runInside(o)
	}
	if !needTool(config.NvimBin) {
		fmt.Fprintf(os.Stderr, "%s: error: missing required tools: %s\n", config.Prog(), config.NvimBin)
		return 1
	}
	switch {
	case o.List:
		return runList(o)
	case o.Kill:
		return runKill(o)
	case len(o.Move) == 2:
		return runMove(o)
	case o.Jump || o.Attach || (o.Name == "" && o.Project == "" && o.Role == "" && o.Load == "" && len(o.Args) == 0):
		if !needTool(config.FzfBin) {
			fmt.Fprintf(os.Stderr, "%s: error: missing required tools: %s\n", config.Prog(), config.FzfBin)
			return 1
		}
		return hub.Run(hub.Options{LiveOnly: o.Attach})
	}
	return runCreate(o)
}

func runList(o *Opts) int {
	sessions, _ := sess.DiscoverAll(config.ProbeTimeout)
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Name < sessions[j].Name })
	if o.JSON {
		if sessions == nil {
			sessions = []sess.Session{}
		}
		for i := range sessions {
			if sessions[i].Ports == nil {
				sessions[i].Ports = []int{}
			}
		}
		b, err := json.MarshalIndent(sessions, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: error: %v\n", config.Prog(), err)
			return 1
		}
		fmt.Fprintln(os.Stdout, string(b))
		return 0
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	for _, s := range sessions {
		kind := "live"
		if s.Busy {
			kind = "busy"
		}
		ports := "-"
		if len(s.Ports) > 0 {
			parts := make([]string, len(s.Ports))
			for i, p := range s.Ports {
				parts[i] = strconv.Itoa(p)
			}
			ports = strings.Join(parts, ",")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
		kind, s.Name, ports, s.Cwd, s.Sock, s.Project, s.Role)
	}
	_ = tw.Flush()
	return 0
}

func runKill(o *Opts) int {
	if !needTool(config.FzfBin) {
		fmt.Fprintf(os.Stderr, "%s: error: missing required tools: %s\n", config.Prog(), config.FzfBin)
		return 1
	}
	sessions, _ := sess.DiscoverAll(config.ProbeTimeout)
	var live []sess.Session
	for _, s := range sessions {
		if !s.Busy {
			live = append(live, s)
		}
	}
	rows := pick.BuildRows(live, nil, nil, "", true)
	if len(rows) == 0 {
		fmt.Fprintf(os.Stderr, "%s: error: no live neovim sockets found\n", config.Prog())
		return 1
	}
	res, err := pick.Run(rows, "", true, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: error: %v\n", config.Prog(), err)
		return 1
	}
	if len(res.Rows) == 0 {
		fmt.Fprintf(os.Stderr, "%s: error: nothing selected\n", config.Prog())
		return 1
	}
	for _, r := range res.Rows {
		if r.Session == nil {
			continue
		}
		if err := spawn.Kill(r.Session.Sock, r.Session.Pid); err != nil {
			fmt.Fprintf(os.Stderr, "%s: error: %v\n", config.Prog(), err)
			return 1
		}
	}
	return 0
}

func runMove(o *Opts) int {
	target := o.Move[1]
	project := proj.CleanPart(target)
	role := ""
	if i := strings.IndexByte(target, '.'); i >= 0 {
		project = proj.CleanPart(target[:i])
		role = proj.CleanPart(target[i+1:])
	}
	if !proj.ValidName(project) {
		fmt.Fprintf(os.Stderr, "%s: error: invalid project\n", config.Prog())
		return 1
	}
	if role != "" && !proj.ValidName(role) {
		fmt.Fprintf(os.Stderr, "%s: error: invalid role\n", config.Prog())
		return 1
	}
	sessions, _ := sess.DiscoverAll(config.ProbeTimeout)
	s := sess.Find(sessions, o.Move[0])
	if s == nil {
		fmt.Fprintf(os.Stderr, "%s: error: no session named %q\n", config.Prog(), o.Move[0])
		return 1
	}
	if err := sess.SetIdentity(s.Sock, project, role); err != nil {
		fmt.Fprintf(os.Stderr, "%s: error: %v\n", config.Prog(), err)
		return 1
	}
	return 0
}

func splitNameOrParts(o *Opts, startDir string) (project, role string, explicit bool) {
	project = ""
	role = ""
	explicit = false
	if o.Name != "" {
		explicit = true
		if i := strings.IndexByte(o.Name, '.'); i >= 0 {
			project = proj.CleanPart(o.Name[:i])
			role = proj.CleanPart(o.Name[i+1:])
		} else {
			project = proj.CleanPart(o.Name)
		}
	}
	if o.Project != "" {
		explicit = true
		project = proj.CleanPart(o.Project)
	}
	if o.Role != "" {
		explicit = true
		role = proj.CleanPart(o.Role)
	}
	if project == "" {
		project = proj.CleanPart(proj.ProjectName(startDir))
	}
	if role == "" {
		role = config.DefaultRole
	}
	return
}

// sockIsLive reports whether sock is held by a live or busy server.
func sockIsLive(sock string) bool {
	if _, err := os.Lstat(sock); err != nil {
		return false
	}
	_, perr := sess.Probe(sock, config.ProbeTimeout)
	if perr == nil {
		return true
	}
	if nvrpc.IsTimeout(perr) {
		return true
	}
	return false
}

func runCreate(o *Opts) int {
	startDir := ""
	var files []string
	dirCount := 0
	for _, a := range o.Args {
		if info, err := os.Stat(a); err == nil && info.IsDir() {
			dirCount++
			startDir = a
		}
	}
	if dirCount == 0 {
		startDir, _ = os.Getwd()
	}
	if startDir == "" {
		startDir, _ = os.Getwd()
	}
	if st, err := os.Stat(startDir); err != nil || !st.IsDir() {
		fmt.Fprintf(os.Stderr, "%s: error: '%s': no such directory\n", config.Prog(), startDir)
		return 1
	}
	if dirCount > 1 {
		for _, a := range o.Args {
			if info, err := os.Stat(a); err == nil && info.IsDir() {
				if a != startDir {
					fmt.Fprintf(os.Stderr, "%s: error: '%s' is a directory\n", config.Prog(), a)
					return 1
				}
			}
		}
	}
	for _, a := range o.Args {
		if info, err := os.Stat(a); err == nil && info.IsDir() {
			continue
		}
		files = append(files, sess.ParseFileTarget(a).Path)
	}
	project, role, explicit := splitNameOrParts(o, startDir)
	terminal := len(files) == 0 && o.Load == ""
	if o.Load != "" {
		matches := proj.FindLoadFiles(startDir, o.Load, config.MaxLoadFiles)
		if len(matches) == 0 {
			fmt.Fprintf(os.Stderr, "%s: warning: no files matched '%s'\n", config.Prog(), o.Load)
			terminal = true
		} else {
			files = nil
			for _, m := range matches {
				files = append(files, filepath.Join(startDir, strings.TrimPrefix(m, "./")))
			}
			terminal = false
		}
	}
	name := proj.JoinName(project, role)
	if !explicit {
		baseProject := project
		for i := 2; ; i++ {
			live := sockIsLive(spawn.SockPath(name))
			if !live {
				break
			}
			project = fmt.Sprintf("%s-%d", baseProject, i)
			name = proj.JoinName(project, role)
		}
	}
	if err := spawn.CheckSockPath(spawn.SockPath(name)); err != nil {
		fmt.Fprintf(os.Stderr, "%s: error: %v\n", config.Prog(), err)
		return 1
	}
	sock, _, err := spawn.Start(spawn.Spec{
		Project:     project,
		Role:        role,
		Root:        startDir,
		Files:       files,
		Terminal:    terminal,
		Startuptime: o.Startuptime,
	})
	if err != nil {
		if errors.Is(err, spawn.ErrExists) {
			fmt.Fprintf(os.Stderr, "%s: error: session '%s' already exists (%s)\n", config.Prog(), name, spawn.SockPath(name))
			return 1
		}
		fmt.Fprintf(os.Stderr, "%s: error: %v\n", config.Prog(), err)
		return 1
	}
	if o.Detached {
		fmt.Fprintln(os.Stdout, sock)
		return 0
	}
	if !needTool(config.FzfBin) {
		fmt.Fprintf(os.Stderr, "%s: error: missing required tools: %s\n", config.Prog(), config.FzfBin)
		return 1
	}
	return hub.Run(hub.Options{Initial: name})
}

func runInside(o *Opts) int {
	switch {
	case o.Attach || o.Jump:
		fmt.Fprintf(os.Stderr, "%s: error: cannot attach: already inside neovim\n", config.Prog())
		return 1
	case o.Kill:
		return runKill(o)
	case o.List:
		return runList(o)
	case len(o.Move) == 2:
		return runMove(o)
	}
	if len(o.Args) == 0 {
		return 0
	}
	target := ""
	if o.To != "" {
		if found := resolveTarget(o.To); found != "" {
			target = found
		}
	}
	if target == "" {
		if s, ok := sess.InsideNvim(); ok {
			target = s
		}
	}
	if target == "" {
		fmt.Fprintf(os.Stderr, "%s: error: no target session\n", config.Prog())
		return 1
	}
	var files []sess.FileTarget
	for _, a := range o.Args {
		ft := sess.ParseFileTarget(a)
		abs, err := filepath.Abs(ft.Path)
		if err != nil {
			abs = ft.Path
		}
		ft.Path = abs
		files = append(files, ft)
	}
	if err := sess.OpenFiles(target, files); err != nil {
		fmt.Fprintf(os.Stderr, "%s: error: %v\n", config.Prog(), err)
		return 1
	}
	return 0
}

func resolveTarget(to string) string {
	sessions, _ := sess.DiscoverAll(config.ProbeTimeout)
	for _, s := range sessions {
		if s.Name == to || s.Sock == to {
			return s.Sock
		}
	}
	cwd, _ := os.Getwd()
	projName := proj.CleanPart(proj.ProjectName(cwd))
	want := proj.JoinName(projName, proj.CleanPart(to))
	for _, s := range sessions {
		if s.Name == want {
			return s.Sock
		}
	}
	return ""
}

var _ = mru.Load
