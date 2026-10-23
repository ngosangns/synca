package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Lossy codes (subset of the design in docs/plans/hooks-sync.md §3.4).
const (
	LossyFailureModeWeakened = "failure_mode_weakened"
	LossyToolUnmapped        = "tool_unmapped"
	LossyOrderNotGuaranteed  = "order_not_guaranteed"
)

// HookLossy reports something the target agent cannot express faithfully.
type HookLossy struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Detail   string `json:"detail"`
}

// hookAdapter describes how one agent stores hooks. Everything that differs per
// agent is data here; the sync engine has no `if agent ==` branches.
type hookAdapter struct {
	agent     AgentKind
	ownFile   bool                // synca owns the whole file (Grok); otherwise a shared settings file
	path      func() string       // user-scope target
	present   func() bool         // is the agent installed (its config dir exists)
	eventName map[string]string   // canonical event -> native name; absent = unsupported
	tools     map[string][]string // tool class -> native tool names
	mcpRegex  string              // native regex matching MCP tools ("" = unmapped)
}

var plainMatcher = regexp.MustCompile(`^[A-Za-z0-9_\- ,|]+$`)

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func identityEvents(except ...string) map[string]string {
	m := map[string]string{}
	for _, e := range hookEvents {
		m[e] = e
	}
	for _, e := range except {
		delete(m, e)
	}
	return m
}

// hookAdapters returns the adapters synca implements. Kiro, OpenCode and Cursor
// are designed (docs/plans/hooks-sync.md) but not implemented yet.
func hookAdapters() []hookAdapter {
	h := HomeDir()
	devinEvents := map[string]string{
		EventSessionStart: EventSessionStart, EventUserPromptSubmit: EventUserPromptSubmit,
		EventPreToolUse: EventPreToolUse, EventPostToolUse: EventPostToolUse, EventStop: EventStop,
		EventSessionEnd: EventSessionEnd, EventPostCompact: "PostCompaction", EventPermissionRequest: EventPermissionRequest,
	}
	return []hookAdapter{
		{
			agent:     AgentClaude,
			path:      func() string { return filepath.Join(h, ".claude", "settings.json") },
			present:   func() bool { return dirExists(filepath.Join(h, ".claude")) },
			eventName: identityEvents(),
			tools: map[string][]string{
				"shell": {"Bash"}, "read": {"Read"}, "write": {"Edit", "Write", "MultiEdit"},
				"search": {"Grep", "Glob"}, "web": {"WebFetch", "WebSearch"}, "agent": {"Task"},
			},
			mcpRegex: "mcp__.*",
		},
		{
			agent:     AgentGrok,
			ownFile:   true,
			path:      func() string { return filepath.Join(h, ".grok", "hooks", "synca.json") },
			present:   func() bool { return dirExists(filepath.Join(h, ".grok")) },
			eventName: identityEvents(EventPermissionRequest),
			tools: map[string][]string{
				"shell": {"run_terminal_command"}, "read": {"read_file"},
				"write":  {"search_replace", "hashline_edit", "write"},
				"search": {"grep", "list_dir"}, "agent": {"spawn_subagent"},
			},
		},
		{
			agent:     AgentDevin,
			path:      func() string { return filepath.Join(h, ".config", "devin", "config.json") },
			present:   func() bool { return dirExists(filepath.Join(h, ".config", "devin")) },
			eventName: devinEvents,
			tools:     map[string][]string{"shell": {"exec"}, "write": {"edit"}},
			mcpRegex:  "mcp__.*",
		},
	}
}

type hookHandlerJSON struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

type hookGroupJSON struct {
	Matcher string            `json:"matcher,omitempty"`
	Hooks   []hookHandlerJSON `json:"hooks"`
}

// renderedHook is one hook translated for one agent.
type renderedHook struct {
	ID    string
	Event string // native event name
	Group []byte // compact JSON of the matcher group
	Hash  string
	Lossy []HookLossy
}

// encodeJSON marshals without HTML escaping so `&&` stays readable in settings files.
func encodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func compactHash(raw []byte) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return ""
	}
	sum := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(sum[:])
}

// supports reports whether the agent has a native event for the canonical one.
func (a hookAdapter) supports(event string) bool {
	_, ok := a.eventName[event]
	return ok
}

// matcherFor builds the native matcher for the tool classes. ok is false when
// no class maps: the hook is then skipped, because dropping the matcher would
// widen it to every tool.
func (a hookAdapter) matcherFor(classes []string) (matcher string, lossy []HookLossy, ok bool) {
	if len(classes) == 0 {
		return "", nil, true
	}
	var names []string
	regex := false
	for _, c := range classes {
		switch {
		case c == "mcp" && a.mcpRegex != "":
			names = append(names, a.mcpRegex)
			regex = true
		case len(a.tools[c]) > 0:
			names = append(names, a.tools[c]...)
		default:
			lossy = append(lossy, HookLossy{LossyToolUnmapped, "warn", fmt.Sprintf("tool class %q has no %s tool name", c, a.agent)})
		}
	}
	if len(names) == 0 {
		return "", lossy, false
	}
	m := strings.Join(names, "|")
	if regex || !plainMatcher.MatchString(m) {
		m = "^(?:" + m + ")$"
	}
	return m, lossy, true
}

// render translates a hook for this agent. It is pure.
func (a hookAdapter) render(id string, h Hook) (*renderedHook, error) {
	native, ok := a.eventName[h.Event]
	if !ok {
		return nil, fmt.Errorf("event_unsupported")
	}
	matcher, lossy, ok := a.matcherFor(h.Tools)
	if !ok {
		return nil, fmt.Errorf("tool_unmapped")
	}
	cmd := strings.ReplaceAll(h.Handler.Command, "{{hook_dir}}", HubHookDir(id))
	if strings.Contains(cmd, "{{project_dir}}") {
		return nil, fmt.Errorf("scope_unsupported")
	}
	if h.OnFailure == "block" {
		lossy = append(lossy, HookLossy{LossyFailureModeWeakened, "warn",
			fmt.Sprintf("%s has no verified way to make a failing hook block; exit code 2 from the script still blocks where %s supports it", a.agent, a.agent)})
	}
	group := hookGroupJSON{
		Matcher: matcher,
		Hooks:   []hookHandlerJSON{{Type: "command", Command: cmd, Timeout: h.Handler.TimeoutSec}},
	}
	raw, err := encodeJSON(group)
	if err != nil {
		return nil, err
	}
	return &renderedHook{ID: id, Event: native, Group: raw, Hash: compactHash(raw), Lossy: lossy}, nil
}
