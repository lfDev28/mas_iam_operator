package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Entry captures the persisted correlation and its status.
//
// The retry fields (Attempts, NextRetryAt, ErrorClass, Parked, DesiredHash)
// and CreatedByBridge were added after state.json files were already in the
// field. They are all optional: an old status=error entry loads with zero
// values, which reads as "retry now", so upgrading the bridge un-sticks
// entries the old code had given up on.
type Entry struct {
	MASID       string   `json:"masID"`
	ProfileID   string   `json:"profileID,omitempty"`
	Status      string   `json:"status,omitempty"`
	LastError   string   `json:"lastError,omitempty"`
	Username    string   `json:"username,omitempty"`
	LastApplied Snapshot `json:"lastApplied,omitempty"`

	// Attempts counts consecutive failures of the same error class.
	Attempts int `json:"attempts,omitempty"`
	// NextRetryAt is the earliest time the bridge retries a failed entry.
	NextRetryAt time.Time `json:"nextRetryAt,omitzero"`
	// ErrorClass is the class of the last failure (see ErrorClass* consts).
	ErrorClass string `json:"errorClass,omitempty"`
	// Parked entries are not retried until DesiredHash changes (the
	// Keycloak user, the target profile or the bridge config changed) or an
	// operator runs scim-bridge-cleanup --reset-state.
	Parked bool `json:"parked,omitempty"`
	// DesiredHash fingerprints the desired MAS state at the last failure.
	DesiredHash string `json:"desiredHash,omitempty"`
	// CreatedByBridge is true when this bridge POSTed the MAS user, or
	// adopted one already carrying this Keycloak ID as its externalId.
	// Cleanup only ever deletes MAS users with this flag set. Entries
	// written before the flag existed read as false: the safe direction.
	CreatedByBridge bool `json:"createdByBridge,omitempty"`
}

// Error classes recorded on failed entries.
const (
	// ErrorClassTransient covers 5xx, 429, 403, transport errors, timeouts
	// and anything else a retry can plausibly fix. Retried with backoff and
	// never parked.
	ErrorClassTransient = "transient"
	// ErrorClassPermanent covers 400/404/422 and unresolvable 409s. Parked
	// after repeated failures for the same desired state.
	ErrorClassPermanent = "permanent"
	// ErrorClassConflict: a MAS user with this userName exists but is not
	// owned by this bridge. Parked at once; never adopted.
	ErrorClassConflict = "conflict"
	// ErrorClassProfileMismatch: the user's masProfile label now maps to a
	// different MAS profile than the one the MAS user was created in.
	ErrorClassProfileMismatch = "profile-mismatch"
)

// DryRunIDPrefix marks MAS IDs that dry-run builds before v0.1.16 wrote into
// state. Those users never existed in MAS and are treated as not created.
const DryRunIDPrefix = "dryrun-"

// HasMASUser reports whether the entry points at a real MAS resource.
func (e Entry) HasMASUser() bool {
	return e.MASID != "" && !strings.HasPrefix(e.MASID, DryRunIDPrefix)
}

// ClearRetry drops the failure bookkeeping, e.g. after a success.
func (e *Entry) ClearRetry() {
	e.LastError = ""
	e.Attempts = 0
	e.NextRetryAt = time.Time{}
	e.ErrorClass = ""
	e.Parked = false
	e.DesiredHash = ""
}

const (
	StatusOK    = "ok"
	StatusError = "error"
	// StatusDeactivated tombstones a user that was deactivated in MAS after
	// leaving the scoped set; it makes deactivation fire once and is cleared
	// when the user re-enters scope and syncs successfully.
	StatusDeactivated = "deactivated"
)

// ReadOnly wraps a store so Save is a no-op. Dry-run uses it: a dry run must
// not leave anything behind that a later live run would trust.
func ReadOnly(s Store) Store { return readOnlyStore{s} }

type readOnlyStore struct{ Store }

func (readOnlyStore) Save(context.Context, string, Entry) error { return nil }

// Store tracks Keycloak ↔ MAS correlations.
type Store interface {
	Close(ctx context.Context) error
	Lookup(ctx context.Context, keycloakID string) (entry Entry, ok bool, err error)
	Save(ctx context.Context, keycloakID string, entry Entry) error
	// List returns a copy of every tracked correlation keyed by Keycloak ID.
	List(ctx context.Context) (map[string]Entry, error)
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

func (m *memoryStore) List(ctx context.Context) (map[string]Entry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return copyEntries(m.items), nil
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

func (f *filesystemStore) List(ctx context.Context) (map[string]Entry, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return copyEntries(f.items), nil
}

func copyEntries(items map[string]Entry) map[string]Entry {
	out := make(map[string]Entry, len(items))
	for id, entry := range items {
		out[id] = entry
	}
	return out
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
	if entry.Status != StatusError {
		entry.ClearRetry()
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
