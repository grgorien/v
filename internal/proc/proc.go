// Linux read /proc for socket and process discovery.
package proc

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// UnixSock is a unix socket from /proc/net/unix.
type UnixSock struct {
	Inode     uint64
	Path      string
	Listening bool
}

// TCPListener is a listening TCP socket from /proc/net/tcp.
type TCPListener struct {
	Port  int
	Inode uint64
}

func fields(line string) []string {
	return strings.Fields(line)
}

// ParseNetUnix parses /proc/net/unix content.
func ParseNetUnix(r io.Reader) ([]UnixSock, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	first := true
	var out []UnixSock
	for sc.Scan() {
		if first {
			first = false
			continue
		}
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := fields(line)
		if len(f) < 7 {
			continue
		}
		flags, err := strconv.ParseUint(f[3], 16, 64)
		if err != nil {
			continue
		}
		inode, err := strconv.ParseUint(f[6], 10, 64)
		if err != nil {
			continue
		}
		path := ""
		if len(f) > 7 {
			path = strings.Join(f[7:], " ")
		}
		out = append(out, UnixSock{
			Inode:     inode,
			Path:      path,
			Listening: flags&0x10000 != 0,
		})
	}
	return out, sc.Err()
}

// ParseNetTCP parses /proc/net/tcp or tcp6 content, keeping LISTEN rows.
func ParseNetTCP(r io.Reader) ([]TCPListener, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	first := true
	var out []TCPListener
	for sc.Scan() {
		if first {
			first = false
			continue
		}
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := fields(line)
		if len(f) < 10 {
			continue
		}
		if f[3] != "0A" {
			continue
		}
		addrPort := f[1]
		idx := strings.LastIndex(addrPort, ":")
		if idx < 0 {
			continue
		}
		port, err := strconv.ParseUint(addrPort[idx+1:], 16, 32)
		if err != nil {
			continue
		}
		inode, err := strconv.ParseUint(f[9], 10, 64)
		if err != nil {
			continue
		}
		out = append(out, TCPListener{Port: int(port), Inode: inode})
	}
	return out, sc.Err()
}

// ParseStatPPid extracts the parent pid from a /proc/PID/stat line.
func ParseStatPPid(stat string) (int, error) {
	idx := strings.LastIndex(stat, ")")
	if idx < 0 || idx+1 >= len(stat) {
		return 0, fmt.Errorf("proc: malformed stat %q", stat)
	}
	rest := strings.Fields(stat[idx+1:])
	if len(rest) < 2 {
		return 0, fmt.Errorf("proc: malformed stat %q", stat)
	}
	ppid, err := strconv.Atoi(rest[1])
	if err != nil {
		return 0, fmt.Errorf("proc: bad ppid in %q: %w", stat, err)
	}
	return ppid, nil
}

// Snapshot is a point-in-time view of /proc.
type Snapshot struct {
	Unix   []UnixSock
	TCP    []TCPListener
	Owners map[uint64][]int
	Comm   map[int]string
	PPid   map[int]int
}

func readNetFile(root, name string) ([]byte, error) {
	return os.ReadFile(filepath.Join(root, "net", name))
}

// Take reads a snapshot relative to root (normally "/proc").
func Take(root string) (*Snapshot, error) {
	s := &Snapshot{
		Owners: map[uint64][]int{},
		Comm:   map[int]string{},
		PPid:   map[int]int{},
	}
	readOK := false
	if b, err := readNetFile(root, "unix"); err == nil {
		if u, err := ParseNetUnix(strings.NewReader(string(b))); err == nil {
			s.Unix = u
			readOK = true
		}
	}
	var tcp []TCPListener
	if b, err := readNetFile(root, "tcp"); err == nil {
		if t, err := ParseNetTCP(strings.NewReader(string(b))); err == nil {
			tcp = append(tcp, t...)
		}
	}
	if b, err := readNetFile(root, "tcp6"); err == nil {
		if t, err := ParseNetTCP(strings.NewReader(string(b))); err == nil {
			tcp = append(tcp, t...)
		}
	}
	s.TCP = tcp
	entries, err := os.ReadDir(root)
	if err != nil {
		if readOK || len(tcp) > 0 {
			return s, nil
		}
		return s, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(root, e.Name(), "comm")); err == nil {
			s.Comm[pid] = strings.TrimRight(string(b), "\n")
		}
		if b, err := os.ReadFile(filepath.Join(root, e.Name(), "stat")); err == nil {
			if pp, err := ParseStatPPid(string(b)); err == nil {
				s.PPid[pid] = pp
			}
		}
		fdDir := filepath.Join(root, e.Name(), "fd")
		fdEntries, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fdEntries {
			target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			if strings.HasPrefix(target, "socket:[") && strings.HasSuffix(target, "]") {
				inode, err := strconv.ParseUint(target[len("socket:["):len(target)-1], 10, 64)
				if err != nil {
					continue
				}
				s.Owners[inode] = append(s.Owners[inode], pid)
			}
		}
	}
	return s, nil
}

// NvimSockets returns listening unix socket paths owned by nvim processes,
// mapped to the owning pid.
func (s *Snapshot) NvimSockets() map[string]int {
	out := map[string]int{}
	for _, u := range s.Unix {
		if !u.Listening || u.Path == "" || strings.HasPrefix(u.Path, "@") {
			continue
		}
		pids := s.Owners[u.Inode]
		for _, pid := range pids {
			if s.Comm[pid] == "nvim" {
				out[u.Path] = pid
				break
			}
		}
	}
	return out
}

// Descendants returns pid and all of its descendants.
func (s *Snapshot) Descendants(pid int) []int {
	children := map[int][]int{}
	for p, pp := range s.PPid {
		children[pp] = append(children[pp], p)
	}
	var out []int
	stack := []int{pid}
	seen := map[int]bool{}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[cur] {
			continue
		}
		seen[cur] = true
		out = append(out, cur)
		stack = append(stack, children[cur]...)
	}
	sort.Ints(out)
	return out
}

// PortsFor returns sorted unique listening TCP ports for pid and descendants.
func (s *Snapshot) PortsFor(pid int) []int {
	fam := map[int]bool{}
	for _, d := range s.Descendants(pid) {
		fam[d] = true
	}
	seen := map[int]bool{}
	var out []int
	for _, t := range s.TCP {
		if seen[t.Port] {
			continue
		}
		for _, owner := range s.Owners[t.Inode] {
			if fam[owner] {
				seen[t.Port] = true
				out = append(out, t.Port)
				break
			}
		}
	}
	sort.Ints(out)
	return out
}
