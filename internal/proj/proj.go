// Implements project discovery, naming, and pattern matching.
package proj

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Repo is a discovered git repository.
type Repo struct {
	Name string
	Path string
}

// Sanitize replaces every rune outside A-Za-z0-9_.- with '_'.
func Sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, c := range s {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') || c == '_' || c == '.' || c == '-' {
			b.WriteRune(c)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// ValidName reports whether s matches [A-Za-z0-9_-]+.
func ValidName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}

// CleanPart sanitizes s and replaces dots with underscores.
func CleanPart(s string) string {
	return strings.ReplaceAll(Sanitize(s), ".", "_")
}

// SplitName splits a socket name at the first dot into project and role.
func SplitName(name string) (project, role string) {
	if i := strings.IndexByte(name, '.'); i >= 0 {
		return name[:i], name[i+1:]
	}
	return name, "dev"
}

// JoinName joins project and role; the default role is omitted.
func JoinName(project, role string) string {
	if role == "" || role == "dev" {
		return project
	}
	return project + "." + role
}

// ExpandHome expands a leading ~/ using $HOME.
func ExpandHome(p string) string {
	if p == "~" {
		return homeDir()
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(homeDir(), p[2:])
	}
	return p
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	return os.Getenv("HOME")
}

// GitRoot walks upward from dir to the first directory containing a .git
// entry (file or directory). Returns "" if none.
func GitRoot(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	cur := abs
	for {
		if _, err := os.Lstat(filepath.Join(cur, ".git")); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}

// ProjectName is the basename of the git root containing dir, else the
// basename of dir.
func ProjectName(dir string) string {
	if root := GitRoot(dir); root != "" {
		return filepath.Base(root)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return filepath.Base(dir)
	}
	return filepath.Base(abs)
}

// DiscoverRepos finds git repositories whose .git entry sits 1 to depth
// levels below a root (parity with `find -mindepth 1 -maxdepth depth -name .git`).
//
// WalkDir visits a directory before descending, so the pruning check must
// compare the *current* directory's level against depth (not depth-1):
// a directory at level L can contain a .git child at level L+1, so we may
// only skip descent once L >= depth.
func DiscoverRepos(roots []string, depth int) []Repo {
	if depth < 1 {
		return nil
	}
	seen := map[string]bool{}
	var out []Repo
	for _, root := range roots {
		root = ExpandHome(root)
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		root = filepath.Clean(root)
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				if d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			if p != root && d.Name() == ".git" {
				return filepath.SkipDir
			}
			rel, rerr := filepath.Rel(root, p)
			if rerr != nil {
				return nil
			}
			level := 0
			if rel != "." {
				level = len(strings.Split(rel, string(os.PathSeparator)))
			}
			if p == root {
				// Continue to children; they are at level 1.
				return nil
			}
			// Inspect this directory at level L (1..depth+?): if L <= depth,
			// a .git here is at depth L and counts. If L >= depth, do not
			// descend (children would be at level L+1, and any .git in them
			// would be at depth L+1 > depth).
			if level <= depth {
				if _, err := os.Lstat(filepath.Join(p, ".git")); err == nil {
					if !seen[p] {
						seen[p] = true
						out = append(out, Repo{Name: ProjectName(p), Path: p})
					}
				}
			}
			if level >= depth {
				return filepath.SkipDir
			}
			return nil
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// MatchPattern reports whether rel matches pattern. A pattern without '/'
// matches the basename. A '**' segment matches zero or more segments.
func MatchPattern(pattern, rel string) bool {
	if !strings.Contains(pattern, "/") {
		ok, err := path.Match(pattern, path.Base(rel))
		return err == nil && ok
	}
	ps := strings.Split(pattern, "/")
	rs := strings.Split(rel, "/")
	return matchSegments(ps, rs)
}

func matchSegments(pat, rel []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			rest := pat[1:]
			if len(rest) == 0 {
				return true
			}
			for i := 0; i <= len(rel); i++ {
				if matchSegments(rest, rel[i:]) {
					return true
				}
			}
			return false
		}
		if len(rel) == 0 {
			return false
		}
		ok, err := path.Match(pat[0], rel[0])
		if err != nil || !ok {
			return false
		}
		pat = pat[1:]
		rel = rel[1:]
	}
	return len(rel) == 0
}

// FindLoadFiles walks startDir collecting regular files (excluding .git
// directories) whose relative path matches pattern, up to max. Results are
// returned as "./rel" in lexical walk order.
func FindLoadFiles(startDir, pattern string, max int) []string {
	var out []string
	if max <= 0 {
		return out
	}
	_ = filepath.WalkDir(startDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" && p != startDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, rerr := filepath.Rel(startDir, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if MatchPattern(pattern, rel) {
			out = append(out, "./"+rel)
			if len(out) >= max {
				return filepath.SkipAll
			}
		}
		return nil
	})
	return out
}
