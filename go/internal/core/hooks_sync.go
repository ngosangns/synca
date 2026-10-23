package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
)

// HookAction is one step of a hooks plan. Kind is a snake_case tag:
// write_hook, update_hook, remove_hook, adopt_hook, skip_hook_same,
// skip_hook_unsupported, conflict_hook.
type HookAction struct {
	Kind   string      `json:"kind"`
	Hook   string      `json:"hook,omitempty"`
	Agent  string      `json:"agent,omitempty"`
	Path   string      `json:"path,omitempty"`
	Event  string      `json:"event,omitempty"`
	Reason string      `json:"reason,omitempty"`
	Lossy  []HookLossy `json:"lossy,omitempty"`
}

// HooksPlan is what `sync hooks` would do.
type HooksPlan struct {
	DryRun  bool         `json:"dryRun"`
	Scope   string       `json:"scope"`
	Actions []HookAction `json:"actions"`
	// Untrusted counts hooks that were skipped because no human approved them.
	Untrusted int `json:"untrusted"`

	edits []*hookFileEdit
}

// hookFileEdit is a computed, not yet applied, change to one agent file.
type hookFileEdit struct {
	agent      AgentKind
	path       string
	existed    bool
	preHash    string
	newData    []byte // nil with deleteFile false = lock-only change
	deleteFile bool
	own        bool
	fileHash   string
	lockSet    map[string]HookLockEntry
	lockDel    []string
}

func bytesHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func readOptional(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return data, true, nil
}

// PlanSyncHooks diffs the hub against the agent files. It never writes.
// Untrusted or changed-but-unapproved hooks are reported and skipped.
func PlanSyncHooks(filter []AgentKind, policy ConflictPolicy) (*HooksPlan, error) {
	hub, err := LoadHooksFile()
	if err != nil {
		return nil, err
	}
	trust, err := LoadTrust()
	if err != nil {
		return nil, err
	}
	lock, err := LoadHookLock()
	if err != nil {
		return nil, err
	}
	plan := &HooksPlan{Scope: ScopeUser.String(), Actions: []HookAction{}}
	ids := make([]string, 0, len(hub.Hooks))
	for id := range hub.Hooks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	untrusted := map[string]bool{}
	for _, ad := range hookAdapters() {
		if len(filter) > 0 && !agentIn(ad.agent, filter) {
			continue
		}
		var edit *hookFileEdit
		var err error
		if ad.ownFile {
			edit, err = planOwnFile(plan, ad, hub, ids, trust, lock, policy, untrusted)
		} else {
			edit, err = planSharedFile(plan, ad, hub, ids, trust, lock, policy, untrusted)
		}
		if err != nil {
			return nil, err
		}
		if edit != nil {
			plan.edits = append(plan.edits, edit)
		}
	}
	plan.Untrusted = len(untrusted)
	return plan, nil
}

// classify renders a hub hook for the adapter. It returns the rendered hook, or
// the action that explains why there is nothing to write.
func classify(plan *HooksPlan, ad hookAdapter, id string, h Hook, trust *TrustStore, untrusted map[string]bool) (rh *renderedHook, held bool) {
	if !h.HookEnabled() || !h.HookAppliesTo(ad.agent) {
		return nil, false
	}
	if !ad.present() {
		plan.Actions = append(plan.Actions, HookAction{Kind: "skip_hook_unsupported", Hook: id, Agent: ad.agent.String(), Reason: "agent_not_installed"})
		return nil, false
	}
	rendered, err := ad.render(id, h)
	if err != nil {
		plan.Actions = append(plan.Actions, HookAction{Kind: "skip_hook_unsupported", Hook: id, Agent: ad.agent.String(), Event: h.Event, Reason: err.Error()})
		return nil, false
	}
	if !trust.IsTrusted(ScopeUser.String(), id, HookHash(id, h)) {
		untrusted[id] = true
		plan.Actions = append(plan.Actions, HookAction{Kind: "conflict_hook", Hook: id, Agent: ad.agent.String(), Event: rendered.Event, Reason: "untrusted"})
		return nil, true
	}
	return rendered, false
}

func findItem(ev *hooksEvent, hash string) int {
	if ev == nil {
		return -1
	}
	for i, it := range ev.items {
		if compactHash(it) == hash {
			return i
		}
	}
	return -1
}

func firstCommand(raw []byte) string {
	var g struct {
		Hooks []struct {
			Command string `json:"command"`
		} `json:"hooks"`
	}
	if json.Unmarshal(raw, &g) != nil || len(g.Hooks) == 0 {
		return ""
	}
	return g.Hooks[0].Command
}

func findItemByCommand(ev *hooksEvent, cmd string) int {
	if ev == nil || cmd == "" {
		return -1
	}
	for i, it := range ev.items {
		if firstCommand(it) == cmd {
			return i
		}
	}
	return -1
}

func removeItem(doc *hooksDoc, event string, idx int, createdEvent bool) {
	ev := doc.find(event)
	if ev == nil || idx < 0 || idx >= len(ev.items) {
		return
	}
	ev.items = append(ev.items[:idx], ev.items[idx+1:]...)
	if len(ev.items) == 0 && createdEvent {
		doc.drop(event)
	}
}

func planSharedFile(plan *HooksPlan, ad hookAdapter, hub *HooksFile, ids []string, trust *TrustStore, lock *HookLock, policy ConflictPolicy, untrusted map[string]bool) (*hookFileEdit, error) {
	path := ad.path()
	agent := ad.agent.String()
	lf := lock.Files[path]
	var lockEntries map[string]HookLockEntry
	if lf != nil {
		lockEntries = lf.Entries
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
	if len(desired) == 0 && len(lockEntries) == 0 {
		return nil, nil
	}

	data, existed, err := readOptional(path)
	if err != nil {
		return nil, err
	}
	sh, err := readSharedHooks(data)
	if err != nil {
		plan.Actions = append(plan.Actions, HookAction{Kind: "conflict_hook", Agent: agent, Path: path, Reason: "parse_error: " + err.Error() + " (the file is left untouched)"})
		return nil, nil
	}
	doc := sh.doc
	edit := &hookFileEdit{agent: ad.agent, path: path, existed: existed, preHash: bytesHash(data), lockSet: map[string]HookLockEntry{}}
	changed := false

	order := make([]string, 0, len(desired))
	for _, id := range ids {
		if desired[id] != nil {
			order = append(order, id)
		}
	}
	for _, id := range order {
		rh := desired[id]
		le, inLock := lockEntries[id]
		act := HookAction{Hook: id, Agent: agent, Path: path, Event: rh.Event, Lossy: rh.Lossy}
		ev := doc.find(rh.Event)
		same := findItem(ev, rh.Hash)
		write := func(kind string) {
			if ev == nil {
				ev = &hooksEvent{name: rh.Event}
				doc.events = append(doc.events, ev)
				edit.lockSet[id] = HookLockEntry{Event: rh.Event, EntryHash: rh.Hash, Command: firstCommand(rh.Group), CreatedEvent: true}
			} else {
				edit.lockSet[id] = HookLockEntry{Event: rh.Event, EntryHash: rh.Hash, Command: firstCommand(rh.Group)}
			}
			ev.items = append(ev.items, append([]byte(nil), rh.Group...))
			act.Kind = kind
			changed = true
		}
		switch {
		case inLock && le.EntryHash == rh.Hash && le.Event == rh.Event:
			if same >= 0 {
				act.Kind = "skip_hook_same"
			} else {
				write("write_hook")
				act.Reason = "missing_from_file"
			}
		case inLock:
			oldEv := doc.find(le.Event)
			oldIdx := findItem(oldEv, le.EntryHash)
			switch {
			case same >= 0:
				edit.lockSet[id] = HookLockEntry{Event: rh.Event, EntryHash: rh.Hash, Command: firstCommand(rh.Group)}
				act.Kind = "adopt_hook"
			case oldIdx >= 0 && le.Event == rh.Event:
				oldEv.items[oldIdx] = append([]byte(nil), rh.Group...)
				edit.lockSet[id] = HookLockEntry{Event: rh.Event, EntryHash: rh.Hash, Command: firstCommand(rh.Group), CreatedEvent: le.CreatedEvent}
				act.Kind = "update_hook"
				changed = true
			case oldIdx >= 0:
				removeItem(doc, le.Event, oldIdx, le.CreatedEvent)
				ev = doc.find(rh.Event)
				write("update_hook")
			default:
				if mod := findItemByCommand(oldEv, le.Command); mod >= 0 {
					if policy == ConflictKeepSource {
						oldEv.items[mod] = append([]byte(nil), rh.Group...)
						edit.lockSet[id] = HookLockEntry{Event: rh.Event, EntryHash: rh.Hash, Command: firstCommand(rh.Group), CreatedEvent: le.CreatedEvent}
						act.Kind = "update_hook"
						act.Reason = "keep-source"
						changed = true
					} else {
						act.Kind = "conflict_hook"
						act.Reason = "modified_externally"
					}
				} else {
					write("write_hook")
					act.Reason = "missing_from_file"
				}
			}
		default:
			if same >= 0 {
				edit.lockSet[id] = HookLockEntry{Event: rh.Event, EntryHash: rh.Hash, Command: firstCommand(rh.Group)}
				act.Kind = "adopt_hook"
			} else {
				write("write_hook")
			}
		}
		plan.Actions = append(plan.Actions, act)
	}

	staleIDs := make([]string, 0)
	for id := range lockEntries {
		if desired[id] == nil && !held[id] {
			staleIDs = append(staleIDs, id)
		}
	}
	sort.Strings(staleIDs)
	for _, id := range staleIDs {
		le := lockEntries[id]
		act := HookAction{Kind: "remove_hook", Hook: id, Agent: agent, Path: path, Event: le.Event}
		ev := doc.find(le.Event)
		if idx := findItem(ev, le.EntryHash); idx >= 0 {
			removeItem(doc, le.Event, idx, le.CreatedEvent)
			changed = true
		} else {
			act.Reason = "already_absent"
		}
		edit.lockDel = append(edit.lockDel, id)
		plan.Actions = append(plan.Actions, act)
	}

	if changed {
		out, err := sh.spliceHooks(doc)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", path, err)
		}
		edit.newData = out
	}
	if !changed && len(edit.lockSet) == 0 && len(edit.lockDel) == 0 {
		return nil, nil
	}
	return edit, nil
}

func planOwnFile(plan *HooksPlan, ad hookAdapter, hub *HooksFile, ids []string, trust *TrustStore, lock *HookLock, policy ConflictPolicy, untrusted map[string]bool) (*hookFileEdit, error) {
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
	if len(desired) == 0 && len(lockEntries) == 0 {
		return nil, nil
	}
	data, existed, err := readOptional(path)
	if err != nil {
		return nil, err
	}

	// Carry over the previously written group of a held (changed, unapproved) hook.
	var oldDoc *hooksDoc
	if existed {
		sh, err := readSharedHooks(data)
		if err != nil {
			plan.Actions = append(plan.Actions, HookAction{Kind: "conflict_hook", Agent: agent, Path: path, Reason: "parse_error: " + err.Error() + " (the file is left untouched)"})
			return nil, nil
		}
		oldDoc = sh.doc
	}
	newDoc := &hooksDoc{}
	place := func(event string, group []byte) {
		ev := newDoc.find(event)
		if ev == nil {
			ev = &hooksEvent{name: event}
			newDoc.events = append(newDoc.events, ev)
		}
		ev.items = append(ev.items, group)
	}
	type placed struct {
		id    string
		entry HookLockEntry
	}
	var finalEntries []placed
	for _, id := range ids {
		switch {
		case desired[id] != nil:
			rh := desired[id]
			place(rh.Event, rh.Group)
			finalEntries = append(finalEntries, placed{id, HookLockEntry{Event: rh.Event, EntryHash: rh.Hash, Command: firstCommand(rh.Group)}})
		case held[id]:
			if le, ok := lockEntries[id]; ok && oldDoc != nil {
				if idx := findItem(oldDoc.find(le.Event), le.EntryHash); idx >= 0 {
					place(le.Event, oldDoc.find(le.Event).items[idx])
					finalEntries = append(finalEntries, placed{id, le})
				}
			}
		}
	}
	sort.SliceStable(newDoc.events, func(i, j int) bool { return eventRank(newDoc.events[i].name) < eventRank(newDoc.events[j].name) })

	var newData []byte
	if len(newDoc.events) > 0 {
		val, err := buildHooksValue(newDoc, "  ", "  ")
		if err != nil {
			return nil, err
		}
		newData = append(append([]byte("{\n  \"hooks\": "), val...), '\n', '}', '\n')
	}

	edit := &hookFileEdit{agent: ad.agent, path: path, existed: existed, preHash: bytesHash(data), own: true, lockSet: map[string]HookLockEntry{}}
	// Hand-edited file: refuse unless it already equals what we would write.
	if existed && !bytesEqual(data, newData) && lockFileHash != bytesHash(data) {
		if policy != ConflictKeepSource {
			plan.Actions = append(plan.Actions, HookAction{Kind: "conflict_hook", Agent: agent, Path: path, Reason: "modified_externally"})
			return nil, nil
		}
	}

	for _, p := range finalEntries {
		edit.lockSet[p.id] = p.entry
	}
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
		edit.deleteFile = existed
	case !bytesEqual(data, newData):
		edit.newData = newData
		edit.fileHash = bytesHash(newData)
	default:
		edit.fileHash = bytesHash(newData)
	}
	if newData == nil {
		edit.fileHash = ""
	}
	if existed == (newData != nil) && bytesEqual(data, newData) && sameEntries(lockEntries, edit.lockSet) && lockFileHash == edit.fileHash {
		return nil, nil
	}
	return edit, nil
}

func bytesEqual(a, b []byte) bool { return string(a) == string(b) }

func sameEntries(a, b map[string]HookLockEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		w, ok := b[k]
		if !ok || w.EntryHash != v.EntryHash || w.Event != v.Event {
			return false
		}
	}
	return true
}

func eventRank(name string) int {
	for i, e := range hookEvents {
		if e == name {
			return i
		}
	}
	return len(hookEvents)
}

// ApplyHooksPlan writes the computed edits: re-check the file, back it up, write
// atomically, then update the lock. It returns one log line per file.
func ApplyHooksPlan(plan *HooksPlan) ([]string, error) {
	var log []string
	if len(plan.edits) == 0 {
		return log, nil
	}
	lock, err := LoadHookLock()
	if err != nil {
		return nil, err
	}
	var firstErr error
	for _, e := range plan.edits {
		cur, existed, err := readOptional(e.path)
		if err != nil {
			firstErr = firstError(firstErr, err)
			log = append(log, fmt.Sprintf("failed %s: %v", e.path, err))
			continue
		}
		if bytesHash(cur) != e.preHash || existed != e.existed {
			firstErr = firstError(firstErr, fmt.Errorf("%s changed while synca was planning (file_changed_during_apply); run again", e.path))
			log = append(log, fmt.Sprintf("skipped %s: file_changed_during_apply", e.path))
			continue
		}
		if e.newData != nil || e.deleteFile {
			if bak, err := backupHookFile(e.path); err != nil {
				firstErr = firstError(firstErr, err)
				log = append(log, fmt.Sprintf("failed %s: backup: %v", e.path, err))
				continue
			} else if bak != "" {
				log = append(log, "backup "+bak)
			}
		}
		switch {
		case e.deleteFile:
			if err := os.Remove(e.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				firstErr = firstError(firstErr, err)
				log = append(log, fmt.Sprintf("failed %s: %v", e.path, err))
				continue
			}
			log = append(log, "removed "+e.path)
		case e.newData != nil:
			if err := atomicWriteFile(e.path, e.newData, 0o644); err != nil {
				firstErr = firstError(firstErr, err)
				log = append(log, fmt.Sprintf("failed %s: %v", e.path, err))
				continue
			}
			log = append(log, "wrote "+e.path)
		}
		lf := lock.file(e.path)
		for id, le := range e.lockSet {
			lf.Entries[id] = le
		}
		for _, id := range e.lockDel {
			delete(lf.Entries, id)
		}
		if e.own {
			lf.FileHash = e.fileHash
		}
		if len(lf.Entries) == 0 {
			delete(lock.Files, e.path)
		}
		if err := lock.save(); err != nil {
			firstErr = firstError(firstErr, err)
			log = append(log, fmt.Sprintf("failed to save the lock: %v", err))
		}
	}
	return log, firstErr
}

func firstError(cur, next error) error {
	if cur != nil {
		return cur
	}
	return next
}
