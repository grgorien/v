package config

import (
	"os"
	"path/filepath"
	"time"
)

var (
	ProjectRoots   = []string{"~/.wrk", "~/.tmp", "~/.x"} // global set up can be done later (maybe onboarding setup)
	ProjectDepth   = 3
	NvimBin        = "nvim"
	FzfBin         = "fzf"
	DefaultRole    = "dev"
	UnmanagedRole  = "-"
	MaxLoadFiles   = 40
	MRUCap         = 64
	ProbeTimeout   = 150 * time.Millisecond
	SpawnTimeout   = 3 * time.Second
	KillGrace      = 2 * time.Second
	FastFailWindow = 300 * time.Millisecond
	FastFailLimit  = 3
)

// MaxSockPath is the maximum length of a unix socket path (sun_path is 108
// bytes including the NUL terminator).
const MaxSockPath = 107

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	return os.Getenv("HOME")
}

// StateDir returns $XDG_STATE_HOME/v, else $HOME/.local/state/v.
func StateDir() string {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "v")
	}
	return filepath.Join(homeDir(), ".local", "state", "v")
}

// SockDir returns StateDir()/sockets.
func SockDir() string { return filepath.Join(StateDir(), "sockets") }

// LogDir returns StateDir()/logs.
func LogDir() string { return filepath.Join(StateDir(), "logs") }

// MRUPath returns StateDir()/mru.
func MRUPath() string { return filepath.Join(StateDir(), "mru") }

// Prog returns the basename of the running program.
func Prog() string { return filepath.Base(os.Args[0]) }
