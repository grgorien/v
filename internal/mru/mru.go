// Maintains a most-recently-used list of session names.
package mru

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"v/internal/config"
)

// Load returns the MRU list, newest first. Any error yields an empty slice.
func Load() []string {
	b, err := os.ReadFile(config.MRUPath())
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

// Touch moves name to the front, deduplicating, and caps the list.
func Touch(name string) error {
	if name == "" {
		return nil
	}
	cur := Load()
	out := []string{name}
	for _, n := range cur {
		if n == name {
			continue
		}
		out = append(out, n)
	}
	if len(out) > config.MRUCap {
		out = out[:config.MRUCap]
	}
	dir := filepath.Dir(config.MRUPath())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(dir, "mru.tmp."+strconv.Itoa(os.Getpid()))
	if err := os.WriteFile(tmp, []byte(strings.Join(out, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, config.MRUPath())
}
