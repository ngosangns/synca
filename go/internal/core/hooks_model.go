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
	"regexp"
	"sort"
	"strings"
)

// HooksVersion is the schema version of ~/.agents/hooks.json.
const HooksVersion = 1

// Hook events (Claude Code PascalCase names are the canonical taxonomy).
const (
	EventSessionStart       = "SessionStart"
	EventUserPromptSubmit   = "UserPromptSubmit"
	EventPreToolUse         = "PreToolUse"
	EventPostToolUse        = "PostToolUse"
	EventStop               = "Stop"
	EventSessionEnd         = "SessionEnd"
	EventPreCompact         = "PreCompact"
	EventPostCompact        = "PostCompact"
	EventNotification       = "Notification"
	EventPermissionRequest  = "PermissionRequest"
	EventSubagentStart      = "SubagentStart"
	EventSubagentStop       = "SubagentStop"
	EventPostToolUseFailure = "PostToolUseFailure"
)

var hookEvents = []string{
	EventSessionStart, EventUserPromptSubmit, EventPreToolUse, EventPostToolUse, EventStop,
	EventSessionEnd, EventPreCompact, EventPostCompact, EventNotification, EventPermissionRequest,
	EventSubagentStart, EventSubagentStop, EventPostToolUseFailure,
}

var toolScopedEvents = map[string]bool{
	EventPreToolUse: true, EventPostToolUse: true, EventPostToolUseFailure: true, EventPermissionRequest: true,
}

// Abstract tool classes.
var hookToolClasses = []string{"shell", "read", "write", "search", "web", "agent", "mcp"}

var hookIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// HookHandler is what runs. Only `command` exists in version 1.
type HookHandler struct {
	Type       string `json:"type,omitempty"`
	Command    string `json:"command"`
	TimeoutSec int    `json:"timeoutSec,omitempty"`
}

// Hook is one canonical hook. Its id is the key in HooksFile.Hooks.
type Hook struct {
	Description   string      `json:"description,omitempty"`
	Enabled       *bool       `json:"enabled,omitempty"`
	Event         string      `json:"event"`
	Tools         []string    `json:"tools,omitempty"`
	Handler       HookHandler `json:"handler"`
	OnFailure     string      `json:"onFailure,omitempty"`
	Agents        []string    `json:"agents,omitempty"`
	ExcludeAgents []string    `json:"excludeAgents,omitempty"`
}

// HooksFile is ~/.agents/hooks.json.
type HooksFile struct {
	Version int             `json:"version"`
	Hooks   map[string]Hook `json:"hooks"`
}

// HookEnabled reports the effective enabled flag (default true).
func (h Hook) HookEnabled() bool { return h.Enabled == nil || *h.Enabled }

// HubHooksPath is the canonical user hooks file.
func HubHooksPath() string { return filepath.Join(HomeDir(), ".agents", "hooks.json") }

// HubHookDir is where a hook's scripts live; `{{hook_dir}}` renders to it.
func HubHookDir(id string) string { return filepath.Join(HomeDir(), ".agents", "hooks", id) }

// HooksStateDir holds the trust store, lock file and backups. It is machine-local.
func HooksStateDir() string { return filepath.Join(HomeDir(), ".local", "state", "synca", "hooks") }

// HooksHubExists reports whether a hooks hub file is present.
func HooksHubExists() bool {
	_, err := os.Stat(HubHooksPath())
	return err == nil
}

// ParseHooksFile parses and validates a hooks file. Unknown fields are errors,
// so a typo never silently drops a safety property such as `onFailure`.
func ParseHooksFile(data []byte) (*HooksFile, error) {
	var f HooksFile
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("invalid hooks file: %v", err)
	}
	if dec.More() {
		return nil, errors.New("invalid hooks file: trailing data")
	}
	if f.Version == 0 {
		return nil, errors.New("invalid hooks file: missing \"version\"")
	}
	if f.Version > HooksVersion {
		return nil, fmt.Errorf("invalid hooks file: version %d is newer than this synca supports (%d); run: synca update", f.Version, HooksVersion)
	}
	if f.Hooks == nil {
		f.Hooks = map[string]Hook{}
	}
	ids := make([]string, 0, len(f.Hooks))
	for id := range f.Hooks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := ValidateHook(id, f.Hooks[id]); err != nil {
			return nil, err
		}
	}
	return &f, nil
}

// LoadHooksFile reads the hub. A missing file is an empty hub.
func LoadHooksFile() (*HooksFile, error) {
	data, err := os.ReadFile(HubHooksPath())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &HooksFile{Version: HooksVersion, Hooks: map[string]Hook{}}, nil
		}
		return nil, err
	}
	return ParseHooksFile(data)
}

func knownAgentName(s string) bool {
	for _, a := range AllAgents() {
		if a != AgentAgents && a.String() == s {
			return true
		}
	}
	return false
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// ValidateHook checks one hook against schema v1.
func ValidateHook(id string, h Hook) error {
	bad := func(format string, a ...any) error {
		return fmt.Errorf("invalid hook %q: %s", id, fmt.Sprintf(format, a...))
	}
	if !hookIDPattern.MatchString(id) {
		return fmt.Errorf("invalid hook id %q: use lowercase letters, digits and '-' (max 64)", id)
	}
	if !containsString(hookEvents, h.Event) {
		return bad("unknown event %q (use %s)", h.Event, strings.Join(hookEvents, "|"))
	}
	for _, t := range h.Tools {
		if !containsString(hookToolClasses, t) {
			return bad("unknown tool class %q (use %s)", t, strings.Join(hookToolClasses, "|"))
		}
	}
	if len(h.Tools) > 0 && !toolScopedEvents[h.Event] {
		return bad("tools only apply to %s, not %s", strings.Join(sortedBoolKeys(toolScopedEvents), "|"), h.Event)
	}
	if h.Handler.Type != "" && h.Handler.Type != "command" {
		return bad("handler.type %q is not supported (only \"command\")", h.Handler.Type)
	}
	if strings.TrimSpace(h.Handler.Command) == "" {
		return bad("handler.command is empty")
	}
	if h.Handler.TimeoutSec < 0 || h.Handler.TimeoutSec > 3600 {
		return bad("handler.timeoutSec must be 0..3600")
	}
	if h.OnFailure != "" && h.OnFailure != "continue" && h.OnFailure != "block" {
		return bad("onFailure must be continue or block")
	}
	for _, a := range append(append([]string{}, h.Agents...), h.ExcludeAgents...) {
		if !knownAgentName(a) {
			return bad("unknown agent %q", a)
		}
	}
	if secretInCommand(h.Handler.Command) {
		return bad("the command embeds a secret (token, key, password or Authorization header). Read it from the environment inside the script instead")
	}
	return nil
}

func sortedBoolKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func secretInCommand(cmd string) bool {
	return maskSecretsInString(cmd) != cmd
}

// HookHash is the trust key payload: the canonical definition plus every file
// under the hook's script directory, so editing a script needs a new approval.
func HookHash(id string, h Hook) string {
	sum := sha256.New()
	canon := h
	canon.Enabled = nil // toggling enabled never changes what runs
	b, _ := json.Marshal(struct {
		ID   string `json:"id"`
		Hook Hook   `json:"hook"`
	}{id, canon})
	sum.Write(b)
	dir := HubHookDir(id)
	var files []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	for _, p := range files {
		rel, _ := filepath.Rel(dir, p)
		content, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		sum.Write([]byte("\x00" + filepath.ToSlash(rel) + "\x00"))
		sum.Write(content)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// SaveHooksFile writes the hub atomically with stable formatting.
func SaveHooksFile(f *HooksFile) error {
	if f.Hooks == nil {
		f.Hooks = map[string]Hook{}
	}
	f.Version = HooksVersion
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(HubHooksPath(), append(data, '\n'), 0o644)
}

// HookAppliesTo reports whether the hook may be written for the agent.
func (h Hook) HookAppliesTo(a AgentKind) bool {
	name := a.String()
	if len(h.Agents) > 0 && !containsString(h.Agents, name) {
		return false
	}
	return !containsString(h.ExcludeAgents, name)
}
