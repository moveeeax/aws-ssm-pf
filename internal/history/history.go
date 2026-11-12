// Package history persists recently used targets and their port mappings.
package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// maxEntries caps how many recent tunnels are retained.
const maxEntries = 20

// Entry records one previously established tunnel.
type Entry struct {
	Target     string    `json:"target"`
	Name       string    `json:"name,omitempty"`
	Host       string    `json:"host,omitempty"` // remote host behind the target; empty for the target itself
	RemotePort int       `json:"remote_port"`
	LocalPort  int       `json:"local_port"`
	Profile    string    `json:"profile,omitempty"`
	Region     string    `json:"region,omitempty"`
	LastUsed   time.Time `json:"last_used"`
}

// Store is an ordered, most-recent-first list of tunnels backed by a JSON file.
type Store struct {
	Path    string  `json:"-"`
	Entries []Entry `json:"entries"`
}

// DefaultPath returns the location of the history file under
// os.UserConfigDir: $XDG_CONFIG_HOME or ~/.config on Unix,
// ~/Library/Application Support on macOS, %AppData% on Windows.
func DefaultPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "ssm-pf", "history.json")
}

// Load reads the store at path. A missing file yields an empty store, not an
// error. A present-but-unreadable-or-corrupt file (partial write from a crash,
// manual edit, concurrent writer) also yields an empty, usable store: the
// returned error reports the problem, but the *Store is never nil, so a caller
// that only wants best-effort history can ignore the error and keep going
// rather than fail the whole command over a cache file.
func Load(path string) (*Store, error) {
	s := &Store{Path: path}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return &Store{Path: path}, err
	}
	if len(raw) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(raw, s); err != nil {
		// Don't trust s: an unmarshal error can still have partially populated
		// it. Start clean instead of risking a half-decoded store.
		return &Store{Path: path}, err
	}
	s.Path = path
	return s, nil
}

// Add records e as the most recent entry, deduplicating by target, host and
// remote port, and capping the list length.
func (s *Store) Add(e Entry) {
	filtered := s.Entries[:0:0]
	for _, existing := range s.Entries {
		if existing.Target == e.Target && existing.Host == e.Host && existing.RemotePort == e.RemotePort {
			continue
		}
		filtered = append(filtered, existing)
	}
	s.Entries = append([]Entry{e}, filtered...)
	if len(s.Entries) > maxEntries {
		s.Entries = s.Entries[:maxEntries]
	}
}

// Recent returns the most recently used entry, or false when the store is empty.
func (s *Store) Recent() (Entry, bool) {
	if len(s.Entries) == 0 {
		return Entry{}, false
	}
	return s.Entries[0], true
}

// Save writes the store back to its path, creating parent directories.
//
// The write goes through a temp file plus rename rather than a direct
// WriteFile: a direct write that's interrupted midway (crash, power loss,
// disk full) leaves truncated JSON behind, and every subsequent invocation
// would then have to recover from a corrupt history file. Renaming into place
// is atomic on the same filesystem, so a reader never observes a partial file.
func (s *Store) Save() error {
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".history-*.json.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.Path)
}
