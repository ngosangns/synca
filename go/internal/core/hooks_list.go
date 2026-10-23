package core

import "sort"

// HookAgentState is where one hook stands for one agent.
type HookAgentState struct {
	Agent string `json:"agent"`
	State string `json:"state"`
}

// HookInfo is one row of `synca hooks list`.
type HookInfo struct {
	ID          string           `json:"id"`
	Description string           `json:"description,omitempty"`
	Event       string           `json:"event"`
	Tools       []string         `json:"tools"`
	Command     string           `json:"command"`
	TimeoutSec  int              `json:"timeoutSec"`
	OnFailure   string           `json:"onFailure"`
	Enabled     bool             `json:"enabled"`
	Trusted     bool             `json:"trusted"`
	Agents      []HookAgentState `json:"agents"`
}

// ListHooks reports every hub hook with its trust state and, per agent, whether
// the agent file is in sync. It reads only; the states come from a dry plan.
func ListHooks(filter []AgentKind) ([]HookInfo, error) {
	hub, err := LoadHooksFile()
	if err != nil {
		return nil, err
	}
	trust, err := LoadTrust()
	if err != nil {
		return nil, err
	}
	plan, err := PlanSyncHooks(filter, ConflictSkip)
	if err != nil {
		return nil, err
	}
	state := map[string]map[string]string{}
	set := func(id, agent, s string) {
		if state[id] == nil {
			state[id] = map[string]string{}
		}
		state[id][agent] = s
	}
	for _, a := range plan.Actions {
		if a.Hook == "" {
			continue
		}
		switch a.Kind {
		case "skip_hook_same":
			set(a.Hook, a.Agent, "synced")
		case "write_hook", "update_hook", "adopt_hook":
			set(a.Hook, a.Agent, "pending")
		case "remove_hook":
			set(a.Hook, a.Agent, "pending_removal")
		case "skip_hook_unsupported":
			set(a.Hook, a.Agent, "unsupported: "+a.Reason)
		case "conflict_hook":
			set(a.Hook, a.Agent, "conflict: "+a.Reason)
		}
	}
	ids := make([]string, 0, len(hub.Hooks))
	for id := range hub.Hooks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]HookInfo, 0, len(ids))
	for _, id := range ids {
		h := hub.Hooks[id]
		info := HookInfo{
			ID: id, Description: h.Description, Event: h.Event, Tools: append([]string{}, h.Tools...),
			Command: h.Handler.Command, TimeoutSec: h.Handler.TimeoutSec, OnFailure: h.OnFailure,
			Enabled: h.HookEnabled(), Trusted: trust.IsTrusted(ScopeUser.String(), id, HookHash(id, h)),
			Agents: []HookAgentState{},
		}
		if info.OnFailure == "" {
			info.OnFailure = "continue"
		}
		for _, ad := range hookAdapters() {
			if len(filter) > 0 && !agentIn(ad.agent, filter) {
				continue
			}
			s, ok := state[id][ad.agent.String()]
			switch {
			case ok:
			case !h.HookEnabled():
				s = "disabled"
			case !h.HookAppliesTo(ad.agent):
				s = "excluded"
			default:
				s = "not_synced"
			}
			info.Agents = append(info.Agents, HookAgentState{Agent: ad.agent.String(), State: s})
		}
		out = append(out, info)
	}
	return out, nil
}

// AddHook writes a new hook to the hub. Unless replace is set an existing id is an error.
func AddHook(id string, h Hook, replace, dryRun bool) (string, error) {
	if err := ValidateHook(id, h); err != nil {
		return "", err
	}
	hub, err := LoadHooksFile()
	if err != nil {
		return "", err
	}
	_, exists := hub.Hooks[id]
	if exists && !replace {
		return "", &HookExistsError{ID: id}
	}
	change := "added"
	if exists {
		change = "replaced"
	}
	if dryRun {
		return change, nil
	}
	hub.Hooks[id] = h
	return change, SaveHooksFile(hub)
}

// HookExistsError reports an id collision on add.
type HookExistsError struct{ ID string }

func (e *HookExistsError) Error() string {
	return "hook " + e.ID + " already exists in the hub (pass --force to replace it; the new definition needs a new approval)"
}

// RemoveHook deletes a hook from the hub and drops its approval. The agent
// files are cleaned by the next `sync hooks`.
func RemoveHook(id string, dryRun bool) error {
	hub, err := LoadHooksFile()
	if err != nil {
		return err
	}
	if _, ok := hub.Hooks[id]; !ok {
		return &HookMissingError{ID: id}
	}
	if dryRun {
		return nil
	}
	delete(hub.Hooks, id)
	if err := SaveHooksFile(hub); err != nil {
		return err
	}
	trust, err := LoadTrust()
	if err != nil {
		return err
	}
	_, err = trust.Revoke(ScopeUser.String(), id)
	return err
}

// HookMissingError reports an unknown hook id.
type HookMissingError struct{ ID string }

func (e *HookMissingError) Error() string { return "no hook named " + e.ID + " in the hub" }

// TrustHook records a human approval of the hook's current definition. The CLI
// calls it only after showing the hook on a terminal.
func TrustHook(id string) (hash string, err error) {
	hub, err := LoadHooksFile()
	if err != nil {
		return "", err
	}
	h, ok := hub.Hooks[id]
	if !ok {
		return "", &HookMissingError{ID: id}
	}
	trust, err := LoadTrust()
	if err != nil {
		return "", err
	}
	hash = HookHash(id, h)
	return hash, trust.Approve(ScopeUser.String(), id, hash)
}

// UntrustHook withdraws an approval.
func UntrustHook(id string) (bool, error) {
	trust, err := LoadTrust()
	if err != nil {
		return false, err
	}
	return trust.Revoke(ScopeUser.String(), id)
}

// DescribeHook returns the lines a human must see before approving: what runs and when.
func DescribeHook(id string) ([]string, error) {
	hub, err := LoadHooksFile()
	if err != nil {
		return nil, err
	}
	h, ok := hub.Hooks[id]
	if !ok {
		return nil, &HookMissingError{ID: id}
	}
	lines := []string{
		"hook:    " + id,
		"event:   " + h.Event,
	}
	if len(h.Tools) > 0 {
		lines = append(lines, "tools:   "+joinComma(h.Tools))
	}
	lines = append(lines, "command: "+h.Handler.Command)
	if len(h.Agents) > 0 {
		lines = append(lines, "agents:  "+joinComma(h.Agents))
	}
	if len(h.ExcludeAgents) > 0 {
		lines = append(lines, "except:  "+joinComma(h.ExcludeAgents))
	}
	dir := HubHookDir(id)
	if dirExists(dir) {
		lines = append(lines, "scripts: "+dir+" (their content is part of the approval)")
	}
	return lines, nil
}

func joinComma(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ","
		}
		out += v
	}
	return out
}
