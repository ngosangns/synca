package core

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed opencode_hooks_plugin.js
var opencodePluginTemplate []byte

const (
	opencodePluginMarker = "synca-managed-plugin: opencode-hooks"
	opencodeDataBegin    = "/* synca:data:begin */"
	opencodeDataEnd      = "/* synca:data:end */"
	opencodePluginSchema = 1
	// opencodeRestartReason is attached when the plugin file actually changes.
	// OpenCode 1.x loads plugins at startup.
	opencodeRestartReason = "restart OpenCode to load the plugin"
)

// opencodeEvent is one canonical event expressed as an OpenCode 1.x plugin hook.
// Verified against anomalyco/opencode v1.18.34 (the release this writer targets).
type opencodeEvent struct {
	kind      string
	eventType string
	block     bool
	approx    string
}

func opencodeEvents() map[string]opencodeEvent {
	return map[string]opencodeEvent{
		EventSessionStart:      {kind: "event", eventType: "session.created", approx: "session.created fires when a session is created, not on every process start"},
		EventUserPromptSubmit:  {kind: "chat.message", approx: "chat.message runs when a user message is received; it is not a verified prompt gate"},
		EventPreToolUse:        {kind: "tool.execute.before", block: true},
		EventPostToolUse:       {kind: "tool.execute.after"},
		EventStop:              {kind: "event", eventType: "session.idle", approx: "session.idle is the closest Stop signal"},
		EventSessionEnd:        {kind: "event", eventType: "session.deleted", approx: "session.deleted fires when a session is deleted"},
		EventPreCompact:        {kind: "experimental.session.compacting", approx: "experimental.session.compacting customizes the compaction prompt; it is not a pre-compact command hook"},
		EventPostCompact:       {kind: "event", eventType: "session.compacted", approx: "session.compacted is the closest PostCompact signal"},
		EventPermissionRequest: {kind: "event", eventType: "permission.asked", approx: "permission.asked is observational; OpenCode 1.18 does not reliably block on permission.ask"},
	}
}

func opencodeNative(ev opencodeEvent) string {
	if ev.eventType != "" {
		return ev.kind + ":" + ev.eventType
	}
	return ev.kind
}

func opencodeAdapter(home string) hookAdapter {
	names := map[string]string{}
	for event, ev := range opencodeEvents() {
		names[event] = opencodeNative(ev)
	}
	return hookAdapter{
		agent:     AgentOpenCode,
		plugin:    true,
		path:      func() string { return openCodePluginPath(home) },
		present:   func() bool { return dirExists(filepath.Join(home, ".config", "opencode")) },
		eventName: names,
		tools: map[string][]string{
			"shell":  {"bash"},
			"read":   {"read"},
			"write":  {"edit", "write", "apply_patch"},
			"search": {"grep", "glob"},
			"web":    {"webfetch", "websearch"},
			"agent":  {"task"},
		},
		renderFn: renderOpenCodeHook,
	}
}

func openCodePluginPath(home string) string {
	return filepath.Join(home, ".config", "opencode", "plugins", "synca-hooks.js")
}

type opencodeHookRecord struct {
	ID             string   `json:"id"`
	DefinitionHash string   `json:"definitionHash"`
	Event          string   `json:"event"`
	Kind           string   `json:"kind"`
	EventType      string   `json:"eventType,omitempty"`
	ToolNames      []string `json:"toolNames,omitempty"`
	Classes        []string `json:"classes,omitempty"`
	Command        string   `json:"command"`
	TimeoutSec     int      `json:"timeoutSec,omitempty"`
	OnFailure      string   `json:"onFailure"`
	Block          bool     `json:"block"`
}

func renderOpenCodeHook(id string, h Hook) (*renderedHook, error) {
	ev, ok := opencodeEvents()[h.Event]
	if !ok {
		return nil, fmt.Errorf("event_unsupported")
	}
	names, lossy, ok := opencodeAdapter(HomeDir()).matcherFor(h.Tools)
	if !ok {
		return nil, fmt.Errorf("tool_unmapped")
	}
	cmd := strings.ReplaceAll(h.Handler.Command, "{{hook_dir}}", HubHookDir(id))
	if strings.Contains(cmd, "{{project_dir}}") {
		return nil, fmt.Errorf("scope_unsupported")
	}
	if ev.approx != "" {
		lossy = append(lossy, HookLossy{LossyEventApprox, "warn", ev.approx})
	}
	if h.OnFailure == "block" && !ev.block {
		lossy = append(lossy, HookLossy{LossyFailureModeWeakened, "warn",
			"opencode cannot block this event; exit code 2 still blocks tool.execute.before"})
	}
	onFailure := h.OnFailure
	if onFailure == "" {
		onFailure = "continue"
	}
	var toolNames []string
	if names != "" {
		toolNames = strings.Split(names, "|")
	}
	rec := opencodeHookRecord{
		ID: id, DefinitionHash: HookHash(id, h), Event: h.Event,
		Kind: ev.kind, EventType: ev.eventType, ToolNames: toolNames, Classes: append([]string{}, h.Tools...),
		Command: cmd, TimeoutSec: h.Handler.TimeoutSec, OnFailure: onFailure, Block: ev.block,
	}
	raw, err := encodeJSON(rec)
	if err != nil {
		return nil, err
	}
	return &renderedHook{ID: id, Event: opencodeNative(ev), Group: raw, Hash: compactHash(raw), Lossy: lossy}, nil
}

func jsDataLiteral(raw []byte) []byte {
	s := string(raw)
	s = strings.ReplaceAll(s, "*/", `*\/`)
	s = strings.ReplaceAll(s, "\u2028", `\u2028`)
	s = strings.ReplaceAll(s, "\u2029", `\u2029`)
	return []byte(s)
}

func renderOpenCodePlugin(records [][]byte) ([]byte, error) {
	begin := bytes.Index(opencodePluginTemplate, []byte(opencodeDataBegin))
	end := bytes.Index(opencodePluginTemplate, []byte(opencodeDataEnd))
	if begin < 0 || end < begin {
		return nil, errors.New("opencode plugin template is missing data markers")
	}
	var body []byte
	for i, rec := range records {
		if i > 0 {
			body = append(body, ',')
		}
		body = append(body, rec...)
	}
	literal := jsDataLiteral(append(append([]byte(`{"hooks":[`), body...), ']', '}'))
	header := fmt.Sprintf("// %s\n// plugin-schema: %d\n// synca-version: %s\n// template-sha256: %s\n// data-sha256: %s\n// DO NOT EDIT. Regenerated by `synca sync hooks`; hand edits are detected and reported.\n",
		opencodePluginMarker, opencodePluginSchema, Version, bytesHash(opencodePluginTemplate), bytesHash(literal))
	var out bytes.Buffer
	out.WriteString(header)
	out.Write(opencodePluginTemplate[:begin+len(opencodeDataBegin)])
	out.WriteByte(' ')
	out.Write(literal)
	out.WriteByte(' ')
	out.Write(opencodePluginTemplate[end:])
	if !bytes.HasSuffix(out.Bytes(), []byte("\n")) {
		out.WriteByte('\n')
	}
	return out.Bytes(), nil
}

func opencodeOwned(data []byte) bool {
	return bytes.Contains(data, []byte(opencodePluginMarker))
}

func parseOpenCodeRecords(file []byte) (map[string][]byte, error) {
	begin := bytes.Index(file, []byte(opencodeDataBegin))
	end := bytes.Index(file, []byte(opencodeDataEnd))
	if begin < 0 || end < begin {
		return nil, errors.New("missing data markers")
	}
	literal := bytes.TrimSpace(file[begin+len(opencodeDataBegin) : end])
	var doc struct {
		Hooks []json.RawMessage `json:"hooks"`
	}
	if err := json.Unmarshal(literal, &doc); err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, raw := range doc.Hooks {
		var id struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &id); err != nil || id.ID == "" {
			return nil, errors.New("hook record has no id")
		}
		compact := append([]byte(nil), raw...)
		if compacted, err := encodeJSON(json.RawMessage(raw)); err == nil {
			compact = compacted
		}
		out[id.ID] = compact
	}
	return out, nil
}

type pluginPlaced struct {
	id    string
	canon string
	raw   []byte
	entry HookLockEntry
}

func planPluginFile(plan *HooksPlan, ad hookAdapter, hub *HooksFile, ids []string, trust *TrustStore, lock *HookLock, policy ConflictPolicy, untrusted map[string]bool) (*hookFileEdit, error) {
	path := ad.path()
	agent := ad.agent.String()
	lf := lock.Files[path]
	var lockEntries map[string]HookLockEntry
	var lockFileHash string
	if lf != nil {
		lockEntries = lf.Entries
		lockFileHash = lf.FileHash
	}
	desired := map[string]*renderedHook{}
	held := map[string]bool{}
	for _, id := range ids {
		rh, isHeld := classify(plan, ad, id, hub.Hooks[id], trust, untrusted)
		if rh != nil {
			desired[id] = rh
		}
		if isHeld {
			held[id] = true
		}
	}
	data, existed, err := readOptional(path)
	if err != nil {
		return nil, err
	}
	owned := existed && opencodeOwned(data)
	if existed && !owned && (len(desired) > 0 || len(lockEntries) > 0) {
		if len(desired) == 0 {
			plan.Actions = append(plan.Actions, HookAction{Kind: "conflict_hook", Agent: agent, Path: path, Reason: "not_owned"})
		}
		for _, id := range ids {
			if desired[id] == nil {
				continue
			}
			plan.Actions = append(plan.Actions, HookAction{Kind: "conflict_hook", Hook: id, Agent: agent, Path: path, Event: desired[id].Event, Reason: "not_owned"})
		}
		return nil, nil
	}
	if len(desired) == 0 && len(lockEntries) == 0 && len(held) == 0 && !owned {
		return nil, nil
	}

	var parsed map[string][]byte
	if owned {
		parsed, err = parseOpenCodeRecords(data)
		if err != nil && policy != ConflictKeepSource {
			plan.Actions = append(plan.Actions, HookAction{Kind: "conflict_hook", Agent: agent, Path: path, Reason: "parse_error: " + err.Error() + " (the file is left untouched)"})
			return nil, nil
		}
	}

	var placed []pluginPlaced
	for _, id := range ids {
		switch {
		case desired[id] != nil:
			rh := desired[id]
			placed = append(placed, pluginPlaced{id: id, canon: hub.Hooks[id].Event, raw: rh.Group, entry: HookLockEntry{Event: rh.Event, EntryHash: rh.Hash, Command: commandOf(rh.Group)}})
		case held[id] && parsed[id] != nil:
			le := lockEntries[id]
			canon := hub.Hooks[id].Event
			placed = append(placed, pluginPlaced{id: id, canon: canon, raw: parsed[id], entry: le})
		}
	}
	sort.SliceStable(placed, func(i, j int) bool {
		if eventRank(placed[i].canon) != eventRank(placed[j].canon) {
			return eventRank(placed[i].canon) < eventRank(placed[j].canon)
		}
		return placed[i].id < placed[j].id
	})

	var newData []byte
	if len(placed) > 0 {
		recs := make([][]byte, len(placed))
		for i, p := range placed {
			recs[i] = p.raw
		}
		newData, err = renderOpenCodePlugin(recs)
		if err != nil {
			return nil, err
		}
	}

	edit := &hookFileEdit{agent: ad.agent, path: path, existed: existed, preHash: bytesHash(data), own: true, lockSet: map[string]HookLockEntry{}}
	if existed && owned && !bytesEqual(data, newData) && lockFileHash != bytesHash(data) && policy != ConflictKeepSource {
		plan.Actions = append(plan.Actions, HookAction{Kind: "conflict_hook", Agent: agent, Path: path, Reason: "modified_externally"})
		return nil, nil
	}
	for _, p := range placed {
		if desired[p.id] == nil {
			if p.entry.EntryHash != "" {
				edit.lockSet[p.id] = p.entry
			}
			continue
		}
		edit.lockSet[p.id] = p.entry
	}
	rewritten := newData != nil && !bytesEqual(data, newData)
	for _, id := range ids {
		rh := desired[id]
		if rh == nil {
			continue
		}
		le, inLock := lockEntries[id]
		act := HookAction{Hook: id, Agent: agent, Path: path, Event: rh.Event, Lossy: rh.Lossy}
		switch {
		case inLock && le.EntryHash == rh.Hash && le.Event == rh.Event && existed:
			act.Kind = "skip_hook_same"
		case inLock:
			act.Kind = "update_hook"
		default:
			act.Kind = "write_hook"
		}
		if rewritten && (act.Kind == "write_hook" || act.Kind == "update_hook") && act.Reason == "" {
			act.Reason = opencodeRestartReason
		}
		plan.Actions = append(plan.Actions, act)
	}
	var stale []string
	for id := range lockEntries {
		if desired[id] == nil && !held[id] {
			stale = append(stale, id)
		}
	}
	sort.Strings(stale)
	for _, id := range stale {
		plan.Actions = append(plan.Actions, HookAction{Kind: "remove_hook", Hook: id, Agent: agent, Path: path, Event: lockEntries[id].Event})
	}
	for id := range lockEntries {
		if _, kept := edit.lockSet[id]; !kept {
			edit.lockDel = append(edit.lockDel, id)
		}
	}
	sort.Strings(edit.lockDel)

	switch {
	case newData == nil:
		edit.deleteFile = existed && owned
	case !bytesEqual(data, newData):
		edit.newData = newData
		edit.fileHash = bytesHash(newData)
	default:
		edit.fileHash = bytesHash(newData)
	}
	if newData == nil {
		edit.fileHash = ""
	}
	if !edit.deleteFile && edit.newData == nil && sameEntries(lockEntries, edit.lockSet) && lockFileHash == edit.fileHash && bytesEqual(data, newData) {
		return nil, nil
	}
	if !existed && newData == nil && len(edit.lockDel) == 0 && len(edit.lockSet) == 0 {
		return nil, nil
	}
	return edit, nil
}

func commandOf(raw []byte) string {
	var rec struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(raw, &rec) != nil {
		return ""
	}
	return rec.Command
}
