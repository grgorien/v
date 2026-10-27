// Identifies and kills headless Neovim servers.
package spawn

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"v/internal/config"
	"v/internal/nvrpc"
	"v/internal/proj"
	"v/internal/sess"
)

// ErrExists is returned when a live session already holds the name.
var ErrExists = errors.New("session already exists")

// Spec describes a server to spawn.
type Spec struct {
	Project     string
	Role        string
	Root        string
	Files       []string
	Terminal    bool
	Startuptime bool
}

// Name returns the socket name for the spec.
func (s Spec) Name() string { return proj.JoinName(s.Project, s.Role) }

// SockPath returns the managed socket path for a session name.
func SockPath(name string) string {
	return filepath.Join(config.SockDir(), name+".sock")
}

// CheckSockPath rejects paths longer than the unix socket limit.
func CheckSockPath(path string) error {
	if len(path) > config.MaxSockPath {
		return fmt.Errorf("socket path too long (%d > %d): %s", len(path), config.MaxSockPath, path)
	}
	return nil
}

func quoteVim(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func lastLines(b []byte, n int) string {
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func openLog(name string) (*os.File, error) {
	if err := os.MkdirAll(config.LogDir(), 0o700); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(config.LogDir(), name+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

func clearEnv() []string {
	var out []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "NVIM=") || strings.HasPrefix(e, "NVIM_LISTEN_ADDRESS=") {
			continue
		}
		out = append(out, e)
	}
	return out
}

// Start launches a headless server and waits until it answers.
func Start(spec Spec) (sock string, pid int, err error) {
	if !proj.ValidName(spec.Project) {
		return "", 0, fmt.Errorf("invalid project %q", spec.Project)
	}
	if !proj.ValidName(spec.Role) {
		return "", 0, fmt.Errorf("invalid role %q", spec.Role)
	}
	if strings.ContainsAny(spec.Root, "\n") {
		return "", 0, errors.New("root contains a newline")
	}
	if strings.TrimSpace(spec.Root) == "" {
		return "", 0, errors.New("empty root")
	}
	name := spec.Name()
	sock = SockPath(name)
	if err := CheckSockPath(sock); err != nil {
		return "", 0, err
	}
	if err := os.MkdirAll(config.SockDir(), 0o700); err != nil {
		return "", 0, err
	}
	if _, statErr := os.Lstat(sock); statErr == nil {
		_, perr := sess.Probe(sock, config.ProbeTimeout)
		if perr == nil || nvrpc.IsTimeout(perr) {
			return "", 0, ErrExists
		}
		if nvrpc.IsRefused(perr) || errors.Is(perr, os.ErrNotExist) {
			if rerr := os.Remove(sock); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
				return "", 0, rerr
			}
		} else {
			return "", 0, ErrExists
		}
	}
	logFile, err := openLog(name)
	if err != nil {
		return "", 0, err
	}
	defer logFile.Close()
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return "", 0, err
	}
	defer devNull.Close()

	args := []string{
		"--headless",
		"--listen", sock,
		"--cmd", "let g:v_project = " + quoteVim(spec.Project),
		"--cmd", "let g:v_role = " + quoteVim(spec.Role),
		"--cmd", "let g:v_root = " + quoteVim(spec.Root),
	}
	if spec.Startuptime {
		args = append(args, "--startuptime", filepath.Join(config.LogDir(), name+".startup"))
	}
	// ASSUMPTION(A1): +terminal starts a shell job on a headless server.
	if spec.Terminal {
		args = append(args, "+terminal")
	} else {
		args = append(args, spec.Files...)
	}
	cmd := exec.Command(config.NvimBin, args...)
	cmd.Dir = spec.Root
	cmd.Stdin = devNull
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = clearEnv()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return "", 0, err
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	pid = cmd.Process.Pid
	deadline := time.Now().Add(config.SpawnTimeout)
	for {
		select {
		case <-done:
			b, _ := os.ReadFile(filepath.Join(config.LogDir(), name+".log"))
			return "", 0, fmt.Errorf("server exited during startup: %s", lastLines(b, 20))
		default:
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			b, _ := os.ReadFile(filepath.Join(config.LogDir(), name+".log"))
			return "", 0, fmt.Errorf("server did not become ready: %s", lastLines(b, 20))
		}
		if _, perr := sess.Probe(sock, 100*time.Millisecond); perr == nil {
			return sock, pid, nil
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	return true
}

func waitGone(pid int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if !pidAlive(pid) {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return !pidAlive(pid)
}

func removeIfManaged(sock string) {
	dir := filepath.Dir(filepath.Clean(sock))
	if dir == filepath.Clean(config.SockDir()) {
		_ = os.Remove(sock)
	}
}

// Kill force-quits a server: RPC qa!, SIGTERM, then SIGKILL.
func Kill(sock string, pid int) error {
	if cl, err := nvrpc.Dial(sock, config.ProbeTimeout); err == nil {
		_ = cl.Command("qa!", config.ProbeTimeout)
		_ = cl.Close()
	}
	if pid <= 0 {
		if pr, err := sess.Probe(sock, config.ProbeTimeout); err == nil {
			pid = pr.Pid
		}
	}
	if pid > 0 && waitGone(pid, config.KillGrace) {
		removeIfManaged(sock)
		return nil
	}
	if pid > 0 {
		_ = syscall.Kill(pid, syscall.SIGTERM)
		if waitGone(pid, config.KillGrace) {
			removeIfManaged(sock)
			return nil
		}
		_ = syscall.Kill(pid, syscall.SIGKILL)
		_ = waitGone(pid, config.KillGrace)
	}
	removeIfManaged(sock)
	return nil
}
