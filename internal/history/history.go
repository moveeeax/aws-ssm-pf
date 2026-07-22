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

// DefaultPath returns the config-dir location of the history file, honoring
// XDG_CONFIG_HOME.
func DefaultPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "ssm-pf", "history.json")
}

// Load reads the store at path. A missing file yields an empty store, not an error.
func Load(path string) (*Store, error) {
	s := &Store{Path: path}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(raw, s); err != nil {
		return nil, err
	}
	s.Path = path
	return s, nil
}

// Add records e as the most recent entry, deduplicating by target+remote port
// and capping the list length.
func (s *Store) Add(e Entry) {
	filtered := s.Entries[:0:0]
	for _, existing := range s.Entries {
		if existing.Target == e.Target && existing.RemotePort == e.RemotePort {
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
func (s *Store) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.Path, raw, 0o600)
}
