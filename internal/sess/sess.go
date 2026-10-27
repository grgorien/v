// Neovim sessions discovered over msgpack-rpc.
package sess

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"v/internal/config"
	"v/internal/nvrpc"
	"v/internal/proc"
	"v/internal/proj"
)

// Session describes a live Neovim server.
type Session struct {
	Name        string `json:"name"`
	Sock        string `json:"sock"`
	Pid         int    `json:"pid"`
	Managed     bool   `json:"managed"`
	Project     string `json:"project"`
	Role        string `json:"role"`
	Root        string `json:"root"`
	Cwd         string `json:"cwd"`
	Ports       []int  `json:"ports"`
	Modified    int    `json:"modified"`
	JobsRunning int    `json:"jobs_running"`
	JobsExited  int    `json:"jobs_exited"`
	Buffers     int    `json:"buffers"`
	UIs         int    `json:"uis"`
	Busy        bool   `json:"busy"`
}

// ProbeResult is the JSON returned by the probe Lua chunk.
type ProbeResult struct {
	Cwd      string `json:"cwd"`
	Pid      int    `json:"pid"`
	Project  string `json:"project"`
	Role     string `json:"role"`
	Root     string `json:"root"`
	Modified int    `json:"modified"`
	Running  int    `json:"running"`
	Exited   int    `json:"exited"`
	Buffers  int    `json:"buffers"`
	UIs      int    `json:"uis"`
}

// FileTarget is a file argument with optional line and column.
type FileTarget struct {
	Path string
	Line int
	Col  int
}

const probeLua = `local mod, run, exited, bufs = 0, 0, 0, 0
for _, b in ipairs(vim.api.nvim_list_bufs()) do
if vim.bo[b].buflisted then bufs = bufs + 1 end
if vim.bo[b].buftype == 'terminal' then
local id = vim.b[b].terminal_job_id
if id and vim.fn.jobwait({ id }, 0)[1] == -1 then
run = run + 1
else
exited = exited + 1
end
elseif vim.bo[b].modified then
mod = mod + 1
end
end
return vim.json.encode({
	cwd = vim.fn.getcwd(),
	pid = vim.fn.getpid(),
	project = vim.g.v_project,
	role = vim.g.v_role,
	root = vim.g.v_root,
	modified = mod,
	running = run,
	exited = exited,
	buffers = bufs,
	uis = #vim.api.nvim_list_uis(),
})`

// NameFromSock derives a session name from a socket path: the basename with
// the ".sock" suffix stripped when the socket is inside SockDir.
func NameFromSock(sock string) string {
	base := filepath.Base(sock)
	dir := filepath.Dir(filepath.Clean(sock))
	if dir == filepath.Clean(config.SockDir()) {
		return strings.TrimSuffix(base, ".sock")
	}
	return base
}

// ResolveIdentity determines project, role and root for a session.
func ResolveIdentity(name string, managed bool, varProject, varRole, varRoot, cwd string) (project, role, root string) {
	project, role, root = varProject, varRole, varRoot
	if project == "" && managed {
		p, r := proj.SplitName(name)
		project = p
		if role == "" {
			role = r
		}
	}
	if project == "" && !managed && cwd != "" {
		project = proj.ProjectName(cwd)
	}
	if role == "" {
		if managed {
			_, r := proj.SplitName(name)
			role = r
		} else {
			role = config.UnmanagedRole
		}
	}
	if root == "" {
		if cwd != "" {
			root = cwd
		}
	}
	return project, role, root
}

// Probe queries the server at sock, returning its state.
func Probe(sock string, timeout time.Duration) (*ProbeResult, error) {
	cl, err := nvrpc.Dial(sock, timeout)
	if err != nil {
		return nil, err
	}
	defer cl.Close()
	res, err := cl.ExecLua(probeLua, []any{}, timeout)
	if err != nil {
		return nil, err
	}
	s, ok := res.(string)
	if !ok {
		return nil, fmt.Errorf("sess: probe returned %T not string", res)
	}
	var pr ProbeResult
	if err := json.Unmarshal([]byte(s), &pr); err != nil {
		return nil, fmt.Errorf("sess: probe json: %w", err)
	}
	return &pr, nil
}

func insideSockDir(p string) bool {
	dir := filepath.Dir(filepath.Clean(p))
	return dir == filepath.Clean(config.SockDir())
}

// DiscoverAll enumerates live sessions. Managed sockets are probed directly;
// unmanaged sockets only when /proc proves an nvim process owns them.
func DiscoverAll(timeout time.Duration) ([]Session, error) {
	snap, snapErr := proc.Take("/proc")
	if snapErr != nil {
		snap = &proc.Snapshot{
			Owners: map[uint64][]int{},
			Comm:   map[int]string{},
			PPid:   map[int]int{},
		}
	}
	type candidate struct {
		sock    string
		managed bool
		pid     int
	}
	seen := map[string]bool{}
	var cands []candidate
	entries, err := os.ReadDir(config.SockDir())
	if err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".sock") {
				continue
			}
			p := filepath.Join(config.SockDir(), e.Name())
			info, ierr := os.Stat(p)
			if ierr != nil || info.Mode()&os.ModeSocket == 0 {
				continue
			}
			clean := filepath.Clean(p)
			if seen[clean] {
				continue
			}
			seen[clean] = true
			pid := 0
			if snapErr == nil {
				if inode := findSocketInode(snap, clean); inode != 0 {
					if pids := snap.Owners[inode]; len(pids) > 0 {
						pid = pids[0]
					}
				}
			}
			cands = append(cands, candidate{sock: clean, managed: true, pid: pid})
		}
	}
	if snapErr == nil {
		for p, pid := range snap.NvimSockets() {
			clean := filepath.Clean(p)
			if seen[clean] {
				continue
			}
			seen[clean] = true
			cands = append(cands, candidate{sock: clean, managed: insideSockDir(clean), pid: pid})
		}
	}
	results := make([]*Session, len(cands))
	var wg sync.WaitGroup
	for i := range cands {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := cands[i]
			pr, err := Probe(c.sock, timeout)
			if err != nil {
				if nvrpc.IsRefused(err) || errors.Is(err, os.ErrNotExist) {
					if c.managed {
						_ = os.Remove(c.sock)
					}
					return
				}
				if nvrpc.IsTimeout(err) {
					s := &Session{
						Name:    NameFromSock(c.sock),
						Sock:    c.sock,
						Pid:     c.pid,
						Managed: c.managed,
						Busy:    true,
					}
					s.Project, s.Role, s.Root = ResolveIdentity(s.Name, c.managed, "", "", "", "")
					results[i] = s
					return
				}
				return
			}
			name := NameFromSock(c.sock)
			s := &Session{
				Name:        name,
				Sock:        c.sock,
				Pid:         pr.Pid,
				Managed:     c.managed,
				Cwd:         pr.Cwd,
				Modified:    pr.Modified,
				JobsRunning: pr.Running,
				JobsExited:  pr.Exited,
				Buffers:     pr.Buffers,
				UIs:         pr.UIs,
			}
			s.Project, s.Role, s.Root = ResolveIdentity(name, c.managed, pr.Project, pr.Role, pr.Root, pr.Cwd)
			if snapErr == nil && pr.Pid > 0 {
				s.Ports = snap.PortsFor(pr.Pid)
			}
			if s.Ports == nil {
				s.Ports = []int{}
			}
			results[i] = s
		}(i)
	}
	wg.Wait()
	var out []Session
	for _, r := range results {
		if r != nil {
			out = append(out, *r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Sock < out[j].Sock
	})
	return out, nil
}

func findSocketInode(snap *proc.Snapshot, p string) uint64 {
	for _, u := range snap.Unix {
		if u.Path == p && u.Listening {
			return u.Inode
		}
	}
	return 0
}

// SetIdentity sets g:v_project and (when role is non-empty) g:v_role.
func SetIdentity(sock, project, role string) error {
	if !proj.ValidName(project) {
		return fmt.Errorf("invalid project %q", project)
	}
	if role != "" && !proj.ValidName(role) {
		return fmt.Errorf("invalid role %q", role)
	}
	cl, err := nvrpc.Dial(sock, config.ProbeTimeout)
	if err != nil {
		return err
	}
	defer cl.Close()
	if _, err := cl.Call("nvim_set_var", []any{"v_project", project}, config.ProbeTimeout); err != nil {
		return err
	}
	if role != "" {
		if _, err := cl.Call("nvim_set_var", []any{"v_role", role}, config.ProbeTimeout); err != nil {
			return err
		}
	}
	return nil
}

// ParseFileTarget parses an argument possibly suffixed with :line or
// :line:col. If the whole argument is an existing path, it is used as is.
func ParseFileTarget(arg string) FileTarget {
	if _, err := os.Stat(arg); err == nil {
		return FileTarget{Path: arg}
	}
	line, col := 0, 0
	path := arg
	// Try :line:col first.
	if i := strings.LastIndexByte(path, ':'); i > 0 {
		rest := path[i+1:]
		if isDigits(rest) {
			if j := strings.LastIndexByte(path[:i], ':'); j > 0 {
				mid := path[j+1 : i]
				if isDigits(mid) {
					if l, err := strconv.Atoi(mid); err == nil {
						if c, err := strconv.Atoi(rest); err == nil {
							path = path[:j]
							line = l
							col = c
						}
					}
				}
			}
		}
	}
	if line == 0 {
		if i := strings.LastIndexByte(path, ':'); i > 0 {
			rest := path[i+1:]
			if isDigits(rest) {
				if l, err := strconv.Atoi(rest); err == nil {
					path = path[:i]
					line = l
				}
			}
		}
	}
	return FileTarget{Path: path, Line: line, Col: col}
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

const openFilesLua = `local path, line, col = ...
vim.cmd.edit(vim.fn.fnameescape(path))
if line and line > 0 then
pcall(vim.api.nvim_win_set_cursor, 0, { line, math.max((col or 1) - 1, 0) })
end
return 1`

// OpenFiles opens files in the session at sock.
func OpenFiles(sock string, files []FileTarget) error {
	if len(files) == 0 {
		return nil
	}
	cl, err := nvrpc.Dial(sock, config.ProbeTimeout)
	if err != nil {
		return err
	}
	defer cl.Close()
	for _, f := range files {
		if _, err := cl.ExecLua(openFilesLua, []any{f.Path, f.Line, f.Col}, config.ProbeTimeout); err != nil {
			return err
		}
	}
	return nil
}

// Find returns the session with the given name, or nil.
func Find(sessions []Session, name string) *Session {
	for i := range sessions {
		if sessions[i].Name == name {
			return &sessions[i]
		}
	}
	return nil
}

// InsideNvim returns $NVIM and whether it is set.
func InsideNvim() (sock string, ok bool) {
	s := os.Getenv("NVIM")
	return s, s != ""
}
