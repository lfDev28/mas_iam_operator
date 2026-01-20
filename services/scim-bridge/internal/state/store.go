package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Entry captures the persisted correlation and its status.
type Entry struct {
	MASID       string   `json:"masID"`
	ProfileID   string   `json:"profileID,omitempty"`
	Status      string   `json:"status,omitempty"`
	LastError   string   `json:"lastError,omitempty"`
	Username    string   `json:"username,omitempty"`
	LastApplied Snapshot `json:"lastApplied,omitempty"`
}

const (
	StatusOK    = "ok"
	StatusError = "error"
)

// Store tracks Keycloak ↔ MAS correlations.
type Store interface {
	Close(ctx context.Context) error
	Lookup(ctx context.Context, keycloakID string) (entry Entry, ok bool, err error)
	Save(ctx context.Context, keycloakID string, entry Entry) error
}

// Options controls backend-specific settings.
type Options struct {
	FilePath         string
	DefaultProfileID string
}

// NewStore selects an implementation based on configuration.
func NewStore(backend string, opts Options) (Store, error) {
	switch backend {
	case "", "memory":
		return &memoryStore{items: make(map[string]Entry), defaultProfileID: opts.DefaultProfileID}, nil
	case "filesystem":
		if opts.FilePath == "" {
			return nil, fmt.Errorf("state file path required for filesystem backend")
		}
		return newFilesystemStore(opts.FilePath, opts.DefaultProfileID)
	case "postgresql":
		return nil, fmt.Errorf("postgresql backend not implemented yet")
	default:
		return nil, fmt.Errorf("unknown backend %s", backend)
	}
}

// memoryStore keeps correlations in process memory only.
type memoryStore struct {
	mu               sync.RWMutex
	items            map[string]Entry
	defaultProfileID string
}

func (m *memoryStore) Close(ctx context.Context) error { return nil }

func (m *memoryStore) Lookup(ctx context.Context, keycloakID string) (Entry, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	val, ok := m.items[keycloakID]
	return val, ok, nil
}

func (m *memoryStore) Save(ctx context.Context, keycloakID string, entry Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[keycloakID] = normalizeEntry(entry, m.defaultProfileID)
	return nil
}

// filesystemStore persists correlations to a JSON file.
type filesystemStore struct {
	path             string
	mu               sync.RWMutex
	items            map[string]Entry
	defaultProfileID string
}

func newFilesystemStore(path string, defaultProfileID string) (*filesystemStore, error) {
	fs := &filesystemStore{path: path, items: make(map[string]Entry), defaultProfileID: defaultProfileID}
	if err := fs.load(); err != nil {
		return nil, err
	}
	return fs, nil
}

func (f *filesystemStore) Close(ctx context.Context) error { return nil }

func (f *filesystemStore) Lookup(ctx context.Context, keycloakID string) (Entry, bool, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	val, ok := f.items[keycloakID]
	return val, ok, nil
}

func (f *filesystemStore) Save(ctx context.Context, keycloakID string, entry Entry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items[keycloakID] = normalizeEntry(entry, f.defaultProfileID)
	return f.persistLocked()
}

func (f *filesystemStore) load() error {
	entries, err := LoadFromFile(f.path, f.defaultProfileID)
	if err != nil {
		return err
	}
	f.items = entries
	return nil
}

func (f *filesystemStore) persistLocked() error {
	return WriteToFile(f.path, f.items, f.defaultProfileID)
}

func normalizeEntry(entry Entry, defaultProfileID string) Entry {
	if entry.Status == "" {
		entry.Status = StatusOK
	}
	if entry.Status == StatusOK {
		entry.LastError = ""
	}
	if entry.ProfileID == "" {
		entry.ProfileID = defaultProfileID
	}
	entry.LastApplied = normalizeSnapshot(entry.LastApplied, defaultProfileID)
	return entry
}

// Snapshot captures the last applied SCIM fields we care about for diffing.
type Snapshot struct {
	UserName   string `json:"userName,omitempty"`
	GivenName  string `json:"givenName,omitempty"`
	FamilyName string `json:"familyName,omitempty"`
	Email      string `json:"email,omitempty"`
	HasEmail   bool   `json:"hasEmail,omitempty"`
	Active     bool   `json:"active"`
	HasActive  bool   `json:"hasActive,omitempty"`
	ProfileID  string `json:"profileID,omitempty"`
}

// IsZero reports whether a snapshot has been recorded.
func (s Snapshot) IsZero() bool {
	return !s.HasActive && s.UserName == "" && !s.HasEmail && s.GivenName == "" && s.FamilyName == ""
}

func normalizeSnapshot(s Snapshot, defaultProfileID string) Snapshot {
	if s.ProfileID == "" {
		s.ProfileID = defaultProfileID
	}
	return s
}

// LoadFromFile reads a state file from disk, upgrading legacy string-only maps.
func LoadFromFile(path string, defaultProfileID string) (map[string]Entry, error) {
	entries := make(map[string]Entry)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return entries, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		var legacy map[string]string
		if errLegacy := json.Unmarshal(data, &legacy); errLegacy != nil {
			return nil, err
		}
		for id, masID := range legacy {
			entries[id] = normalizeEntry(Entry{MASID: masID}, defaultProfileID)
		}
	} else {
		for id, entry := range entries {
			entries[id] = normalizeEntry(entry, defaultProfileID)
		}
	}
	return entries, nil
}

// WriteToFile persists entries to disk atomically.
func WriteToFile(path string, entries map[string]Entry, defaultProfileID string) error {
	normalized := make(map[string]Entry, len(entries))
	for id, entry := range entries {
		normalized[id] = normalizeEntry(entry, defaultProfileID)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".scim-state-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
