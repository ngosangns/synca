package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// atomicWriteFile writes data next to path, fsyncs and renames it into place.
// A symlinked path (dotfile managers) is resolved and the real file replaced, so
// the link survives. An existing file keeps its mode.
func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	real := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		real = resolved
	}
	if st, err := os.Stat(real); err == nil {
		mode = st.Mode().Perm()
	}
	dir := filepath.Dir(real)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".synca-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	cleanup := func() { _ = os.Remove(name) }
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Chmod(name, mode); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(name, real); err != nil {
		cleanup()
		return err
	}
	return nil
}

const hookBackupKeep = 20

// backupHookFile copies path into the state dir before a write. A missing file
// has nothing to back up.
func backupHookFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	abs, _ := filepath.Abs(path)
	rel := strings.TrimPrefix(filepath.ToSlash(abs), "/")
	base := filepath.Join(HooksStateDir(), "backups")
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	dest := filepath.Join(base, stamp, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(dest, data, 0o600); err != nil {
		return "", err
	}
	pruneHookBackups(base, rel)
	return dest, nil
}

func pruneHookBackups(base, rel string) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	var stamps []string
	for _, e := range entries {
		if e.IsDir() {
			if _, err := os.Stat(filepath.Join(base, e.Name(), filepath.FromSlash(rel))); err == nil {
				stamps = append(stamps, e.Name())
			}
		}
	}
	sort.Strings(stamps)
	for len(stamps) > hookBackupKeep {
		_ = os.RemoveAll(filepath.Join(base, stamps[0], filepath.FromSlash(rel)))
		stamps = stamps[1:]
	}
}

// ---------- Trust store ----------

// TrustStore records which (scope, id, hash) a human approved. It lives in the
// machine-local state dir with mode 0600 and is never part of a repository.
type TrustStore struct {
	Version int                   `json:"version"`
	Trusted map[string]TrustEntry `json:"trusted"`
}

type TrustEntry struct {
	Hash       string `json:"hash"`
	ApprovedAt string `json:"approvedAt"`
}

func trustPath() string { return filepath.Join(HooksStateDir(), "trust.json") }

func trustKey(scope, id string) string { return scope + ":" + id }

// LoadTrust reads the store. A missing file is empty.
func LoadTrust() (*TrustStore, error) {
	t := &TrustStore{Version: 1, Trusted: map[string]TrustEntry{}}
	data, err := os.ReadFile(trustPath())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return t, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, t); err != nil {
		return nil, fmt.Errorf("trust store %s is not valid JSON: %v", trustPath(), err)
	}
	if t.Trusted == nil {
		t.Trusted = map[string]TrustEntry{}
	}
	return t, nil
}

func (t *TrustStore) save() error {
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(trustPath(), append(data, '\n'), 0o600)
}

// IsTrusted reports whether this exact hook definition was approved.
func (t *TrustStore) IsTrusted(scope, id, hash string) bool {
	e, ok := t.Trusted[trustKey(scope, id)]
	return ok && e.Hash == hash
}

// Approve records the approval. Callers must have shown the hook to a human.
func (t *TrustStore) Approve(scope, id, hash string) error {
	t.Trusted[trustKey(scope, id)] = TrustEntry{Hash: hash, ApprovedAt: time.Now().UTC().Format(time.RFC3339)}
	return t.save()
}

// Revoke removes an approval. It reports whether one existed.
func (t *TrustStore) Revoke(scope, id string) (bool, error) {
	if _, ok := t.Trusted[trustKey(scope, id)]; !ok {
		return false, nil
	}
	delete(t.Trusted, trustKey(scope, id))
	return true, t.save()
}

// ---------- Lock file ----------

// HookLock records what synca wrote, so a later run can update or remove only
// its own entries and leave foreign ones untouched.
type HookLock struct {
	Version int                      `json:"version"`
	Files   map[string]*HookLockFile `json:"files"`
}

type HookLockFile struct {
	// FileHash is the hash of an own-file target (Grok) as synca last wrote it.
	FileHash string                   `json:"fileHash,omitempty"`
	Entries  map[string]HookLockEntry `json:"entries"`
}

type HookLockEntry struct {
	Event        string `json:"event"`
	EntryHash    string `json:"entryHash"`
	Command      string `json:"command,omitempty"`
	CreatedEvent bool   `json:"createdEvent,omitempty"`
}

func lockPath() string { return filepath.Join(HooksStateDir(), "lock.json") }

// LoadHookLock reads the lock. A missing file is empty.
func LoadHookLock() (*HookLock, error) {
	l := &HookLock{Version: 1, Files: map[string]*HookLockFile{}}
	data, err := os.ReadFile(lockPath())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return l, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, l); err != nil {
		return nil, fmt.Errorf("hooks lock %s is not valid JSON: %v", lockPath(), err)
	}
	if l.Files == nil {
		l.Files = map[string]*HookLockFile{}
	}
	return l, nil
}

func (l *HookLock) save() error {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(lockPath(), append(data, '\n'), 0o600)
}

func (l *HookLock) file(path string) *HookLockFile {
	f := l.Files[path]
	if f == nil {
		f = &HookLockFile{Entries: map[string]HookLockEntry{}}
		l.Files[path] = f
	}
	if f.Entries == nil {
		f.Entries = map[string]HookLockEntry{}
	}
	return f
}
