package history

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAddDedupAndOrder(t *testing.T) {
	s := &Store{}
	s.Add(Entry{Target: "i-1", RemotePort: 5432, LocalPort: 15432})
	s.Add(Entry{Target: "i-2", RemotePort: 6379, LocalPort: 16379})
	// Re-add i-1 with a new local port: should move to front and dedup.
	s.Add(Entry{Target: "i-1", RemotePort: 5432, LocalPort: 25432})

	if len(s.Entries) != 2 {
		t.Fatalf("want 2 entries after dedup, got %d", len(s.Entries))
	}
	front, _ := s.Recent()
	if front.Target != "i-1" || front.LocalPort != 25432 {
		t.Errorf("front entry = %+v, want i-1 with local 25432", front)
	}
}

func TestAddDistinctRemotePortsKept(t *testing.T) {
	s := &Store{}
	s.Add(Entry{Target: "i-1", RemotePort: 5432})
	s.Add(Entry{Target: "i-1", RemotePort: 6379})
	if len(s.Entries) != 2 {
		t.Fatalf("distinct remote ports on same target should coexist, got %d", len(s.Entries))
	}
}

func TestCap(t *testing.T) {
	s := &Store{}
	for i := 0; i < maxEntries+5; i++ {
		s.Add(Entry{Target: "i", RemotePort: i})
	}
	if len(s.Entries) != maxEntries {
		t.Fatalf("want cap %d, got %d", maxEntries, len(s.Entries))
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "history.json")
	s := &Store{Path: path}
	s.Add(Entry{Target: "i-9", Name: "db", RemotePort: 5432, LocalPort: 15432, Profile: "prod", LastUsed: time.Unix(1_700_000_000, 0)})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, ok := loaded.Recent()
	if !ok || got.Target != "i-9" || got.Name != "db" || got.Profile != "prod" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if loaded.Path != path {
		t.Errorf("Path not restored: %q", loaded.Path)
	}
}

func TestLoadMissingFile(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if _, ok := s.Recent(); ok {
		t.Error("empty store should have no recent entry")
	}
}
