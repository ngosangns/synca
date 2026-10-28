package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Lock files record where a skill came from. synca reads and writes the two
// files the skills CLI (npx skills) keeps, so the two tools see one history:
//
//	user scope     $XDG_STATE_HOME/skills/.skill-lock.json, else ~/.agents/.skill-lock.json (version 3)
//	project scope  <project root>/skills-lock.json (version 1)
//
// Fields synca does not know are kept byte for byte when it rewrites a file.
const (
	globalLockName  = ".skill-lock.json"
	projectLockName = "skills-lock.json"
	globalLockVer   = 3
	projectLockVer  = 1
)

// LockEntry is what the lock file records for one skill.
type LockEntry struct {
	Source     string `json:"source,omitempty"`
	SourceType string `json:"sourceType,omitempty"`
	SourceURL  string `json:"sourceUrl,omitempty"`
	Ref        string `json:"ref,omitempty"`
	SkillPath  string `json:"skillPath,omitempty"`
	// FolderHash is the Git tree id of the skill folder (user lock, skillFolderHash).
	FolderHash string `json:"skillFolderHash,omitempty"`
	// ComputedHash is the content hash of the folder (project lock, computedHash).
	ComputedHash string `json:"computedHash,omitempty"`
	PluginName   string `json:"pluginName,omitempty"`
}

// Remote reports whether the entry points at a place synca can fetch from.
func (e LockEntry) Remote() bool {
	return e.SourceType != "" && e.SourceType != "local"
}

// LockPath is the lock file for a scope, or "" when a project has no root.
func LockPath(scope Scope, cwd string) string {
	if scope == ScopeUser {
		if x := os.Getenv("XDG_STATE_HOME"); x != "" {
			return filepath.Join(x, "skills", globalLockName)
		}
		return filepath.Join(HomeDir(), ".agents", globalLockName)
	}
	if r := ProjectRoot(cwd); r != "" {
		return filepath.Join(r, projectLockName)
	}
	return ""
}

type lockFile struct {
	path   string
	top    map[string]json.RawMessage
	skills map[string]json.RawMessage
}

func readLock(path string) (*lockFile, error) {
	lf := &lockFile{path: path, top: map[string]json.RawMessage{}, skills: map[string]json.RawMessage{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return lf, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &lf.top); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	if raw, ok := lf.top["skills"]; ok {
		if err := json.Unmarshal(raw, &lf.skills); err != nil {
			return nil, fmt.Errorf("%s: skills is not an object: %w", path, err)
		}
	}
	return lf, nil
}

// ReadLock returns the entries keyed by NormalizeKey of the skill name. A
// missing or unreadable file gives an empty map: origin is then "local".
func ReadLock(scope Scope, cwd string) map[string]LockEntry {
	out := map[string]LockEntry{}
	path := LockPath(scope, cwd)
	if path == "" {
		return out
	}
	lf, err := readLock(path)
	if err != nil {
		return out
	}
	for name, raw := range lf.skills {
		var e LockEntry
		if json.Unmarshal(raw, &e) == nil {
			out[NormalizeKey(name)] = e
		}
	}
	return out
}

func (lf *lockFile) version() int {
	if lf.path != "" && filepath.Base(lf.path) == projectLockName {
		return projectLockVer
	}
	return globalLockVer
}

func (lf *lockFile) save() error {
	skills, err := json.Marshal(lf.skills)
	if err != nil {
		return err
	}
	// Map keys marshal sorted, which matches how both tools write the file.
	lf.top["skills"] = skills
	if _, ok := lf.top["version"]; !ok {
		v, _ := json.Marshal(lf.version())
		lf.top["version"] = v
	}
	if lf.version() == globalLockVer {
		if _, ok := lf.top["dismissed"]; !ok {
			lf.top["dismissed"] = json.RawMessage("{}")
		}
	}
	data, err := json.Marshal(lf.top)
	if err != nil {
		return err
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, data, "", "  "); err != nil {
		return err
	}
	if lf.version() == projectLockVer {
		pretty.WriteByte('\n')
	}
	return atomicWriteFile(lf.path, pretty.Bytes(), 0o644)
}

// findName returns the lock key whose normalized form is key.
func (lf *lockFile) findName(key string) (string, bool) {
	names := make([]string, 0, len(lf.skills))
	for n := range lf.skills {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if NormalizeKey(n) == key {
			return n, true
		}
	}
	return "", false
}

// RecordOrigin writes entry for the skill key. Unknown fields already on the
// entry are kept. installedAt stays from the first install.
func RecordOrigin(lockPath, key string, entry LockEntry) error {
	lf, err := readLock(lockPath)
	if err != nil {
		return err
	}
	name, found := lf.findName(key)
	if !found {
		name = key
	}
	fields := map[string]json.RawMessage{}
	if raw, ok := lf.skills[name]; ok {
		if err := json.Unmarshal(raw, &fields); err != nil {
			fields = map[string]json.RawMessage{}
		}
	}
	project := lf.version() == projectLockVer
	set := func(k, v string) {
		if v == "" {
			delete(fields, k)
			return
		}
		b, _ := json.Marshal(v)
		fields[k] = b
	}
	set("source", entry.Source)
	set("sourceType", entry.SourceType)
	set("sourceUrl", entry.SourceURL)
	set("ref", entry.Ref)
	set("skillPath", entry.SkillPath)
	set("pluginName", entry.PluginName)
	if project {
		set("computedHash", entry.ComputedHash)
		delete(fields, "skillFolderHash")
	} else {
		set("skillFolderHash", entry.FolderHash)
		now, _ := json.Marshal(nowUTC())
		if _, ok := fields["installedAt"]; !ok {
			fields["installedAt"] = now
		}
		fields["updatedAt"] = now
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	lf.skills[name] = raw
	return lf.save()
}

// ForgetOrigin removes the skill from the lock file. A skill that is not there
// is not an error and does not rewrite the file.
func ForgetOrigin(lockPath, key string) (bool, error) {
	lf, err := readLock(lockPath)
	if err != nil {
		return false, err
	}
	name, found := lf.findName(key)
	if !found {
		return false, nil
	}
	delete(lf.skills, name)
	return true, lf.save()
}

// ComputeFolderHash is the content hash the skills CLI keeps in a project lock:
// SHA-256 over each file's slash path and bytes, files sorted by path, without
// .git and node_modules.
func ComputeFolderHash(dir string) (string, error) {
	type file struct {
		rel  string
		data []byte
	}
	var files []file
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		files = append(files, file{filepath.ToSlash(rel), data})
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.rel))
		h.Write(f.data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func isTreeID(s string) bool {
	if len(s) != 40 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil && strings.ToLower(s) == s
}
