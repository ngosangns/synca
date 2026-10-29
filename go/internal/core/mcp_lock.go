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
)

// The MCP lock records where a server was installed from. It is synca's own
// file, unlike the skills lock it does not share a format with another tool:
//
//	user scope     $XDG_STATE_HOME/synca/mcp-lock.json, else ~/.agents/.mcp-lock.json
//	project scope  <project root>/.mcp-lock.json
//
// Only names are stored; env and header values stay in the agent configs.
const (
	mcpLockName    = ".mcp-lock.json"
	mcpLockVersion = 1
)

// McpLockEntry is what the lock records for one server.
type McpLockEntry struct {
	Source      string   `json:"source,omitempty"`
	SourceType  string   `json:"sourceType,omitempty"`
	Version     string   `json:"version,omitempty"`
	Variant     string   `json:"variant,omitempty"`
	EnvKeys     []string `json:"envKeys,omitempty"`
	HeaderKeys  []string `json:"headerKeys,omitempty"`
	InstalledAt string   `json:"installedAt,omitempty"`
	UpdatedAt   string   `json:"updatedAt,omitempty"`
}

// Registry reports whether the entry points at the MCP registry, which is the
// only source mcp update can re-read.
func (e McpLockEntry) Registry() bool {
	return e.SourceType == "registry" && e.Source != ""
}

// McpLockPath is the lock file for a scope, or "" when a project has no root.
func McpLockPath(scope Scope, cwd string) string {
	if scope == ScopeUser {
		if x := os.Getenv("XDG_STATE_HOME"); x != "" {
			return filepath.Join(x, "synca", "mcp-lock.json")
		}
		return filepath.Join(HomeDir(), ".agents", mcpLockName)
	}
	if r := ProjectRoot(cwd); r != "" {
		return filepath.Join(r, mcpLockName)
	}
	return ""
}

func readMcpLock(path string) (map[string]McpLockEntry, error) {
	out := map[string]McpLockEntry{}
	if path == "" {
		return out, nil
	}
	text, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(text)) == "" {
		return out, nil
	}
	var doc struct {
		Mcps map[string]McpLockEntry `json:"mcps"`
	}
	if err := json.Unmarshal(text, &doc); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	for key, entry := range doc.Mcps {
		out[NormalizeKey(key)] = entry
	}
	return out, nil
}

func writeMcpLock(path string, entries map[string]McpLockEntry) error {
	if path == "" {
		return nil
	}
	if parent := filepath.Dir(path); parent != "" {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return err
		}
	}
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sorted := make(map[string]McpLockEntry, len(entries))
	for _, k := range keys {
		sorted[k] = entries[k]
	}
	doc := struct {
		Version int                     `json:"version"`
		Mcps    map[string]McpLockEntry `json:"mcps"`
	}{Version: mcpLockVersion, Mcps: sorted}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWriteFile(path, data, 0o644)
}

// ReadMcpLock returns the recorded origins keyed by normalized server key.
func ReadMcpLock(path string) (map[string]McpLockEntry, error) {
	return readMcpLock(path)
}

// RecordMcpOrigin writes entry for key. The first install time is kept.
func RecordMcpOrigin(path, key string, entry McpLockEntry) error {
	entries, err := readMcpLock(path)
	if err != nil {
		return err
	}
	key = NormalizeKey(key)
	now := nowUTC()
	if old, ok := entries[key]; ok && old.InstalledAt != "" {
		entry.InstalledAt = old.InstalledAt
	} else if entry.InstalledAt == "" {
		entry.InstalledAt = now
	}
	entry.UpdatedAt = now
	entries[key] = entry
	return writeMcpLock(path, entries)
}

// ForgetMcpOrigin removes key. A server that is not recorded is not an error
// and does not rewrite the file.
func ForgetMcpOrigin(path, key string) (bool, error) {
	entries, err := readMcpLock(path)
	if err != nil {
		return false, err
	}
	key = NormalizeKey(key)
	if _, ok := entries[key]; !ok {
		return false, nil
	}
	delete(entries, key)
	return true, writeMcpLock(path, entries)
}
