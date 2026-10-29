package core

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ngosangns/synca/go/internal/mcpregistry"
)

// PlanUpdateMcp re-reads the registry for servers whose origin an install
// recorded and plans config writes for the ones that changed. Env and header
// values come from the current hub config, so a secret is never re-asked for.
// A server that fails is noted and the rest still plan.
func PlanUpdateMcp(scope Scope, cwd string, keys []string, agentsFilter []AgentKind, version string) (*ManagePlan, error) {
	lockPath := McpLockPath(scope, cwd)
	if lockPath == "" {
		return nil, fmt.Errorf("no project root for project scope")
	}
	entries, err := readMcpLock(lockPath)
	if err != nil {
		return nil, err
	}
	hub := CanonicalMcpPath(scope, cwd)
	if hub == "" {
		return nil, fmt.Errorf("no project root for project scope")
	}

	var targets []string
	if len(keys) == 0 {
		for key, entry := range entries {
			if entry.Registry() {
				targets = append(targets, key)
			}
		}
		sort.Strings(targets)
	} else {
		for _, key := range keys {
			key = NormalizeKey(key)
			entry, ok := entries[key]
			if !ok || !entry.Registry() {
				return nil, fmt.Errorf("unknown mcp server %q (no recorded registry source; install it from the registry first)", key)
			}
			targets = append(targets, key)
		}
	}

	var writers []agentPath
	for _, r := range McpConfigPaths(scope, cwd) {
		if r.Agent == AgentAgents {
			continue
		}
		if agentsFilter != nil && !agentIn(r.Agent, agentsFilter) {
			continue
		}
		writers = append(writers, r)
	}

	plan := &ManagePlan{
		Scope:  scope.String(),
		DryRun: true,
		Notes:  []string{"update mcp from recorded registry sources"},
	}
	if len(targets) == 0 {
		plan.Notes = append(plan.Notes, "no mcp server has a recorded registry source")
		return plan, nil
	}

	client := mcpregistry.New(Version)
	ctx, cancel := context.WithTimeout(context.Background(), registryTimeout)
	defer cancel()

	servers, _ := ReadMcpServers(AgentAgents, hub)
	changed := 0
	for _, key := range targets {
		entry := entries[key]
		current, ok := servers[key]
		if !ok {
			plan.Notes = append(plan.Notes, fmt.Sprintf("missing: %s (not in hub %s)", key, hub))
			continue
		}
		rec, err := client.Info(ctx, entry.Source, version)
		if err != nil {
			plan.Notes = append(plan.Notes, fmt.Sprintf("failed: %s (%v)", key, err))
			continue
		}
		opts := McpRegistryOptions{
			Key:     key,
			Env:     current.Env,
			Headers: current.Headers,
			Enabled: current.Enabled,
			Args:    map[string]string{},
		}
		if isPackageVariant(entry.Variant) {
			opts.PackageType = entry.Variant
		} else {
			opts.Transport = entry.Variant
		}
		_, next, _, err := McpFromRegistry(&rec.Server, opts)
		if err != nil {
			plan.Notes = append(plan.Notes, fmt.Sprintf("failed: %s (%v)", key, err))
			continue
		}
		carryOver(next, &current)
		oldVersion, newVersion := entry.Version, strings.TrimSpace(rec.Server.Version)
		if newVersion == "" {
			newVersion = "unknown"
		}
		if McpFingerprint(next) == McpFingerprint(&current) {
			plan.Notes = append(plan.Notes, fmt.Sprintf("up to date: %s (%s)", key, newVersion))
			continue
		}
		changed++
		plan.Notes = append(plan.Notes, fmt.Sprintf("update: %s %s -> %s", key, displayVersion(oldVersion), newVersion))
		plan.Actions = append(plan.Actions, ManageAction{
			Kind: "upsert_mcp_hub", Hub: hub, Server: key, Mcp: next,
		})
		for _, w := range writers {
			plan.Actions = append(plan.Actions, ManageAction{
				Kind: "write_mcp_agent", Path: w.Path, Agent: w.Agent.String(), Server: key, Mcp: next,
			})
		}
		plan.Actions = append(plan.Actions, ManageAction{
			Kind:   "record_mcp_origin",
			Path:   lockPath,
			Server: key,
			McpOrigin: &McpLockEntry{
				Source:     entry.Source,
				SourceType: entry.SourceType,
				Version:    newVersion,
				Variant:    entry.Variant,
				EnvKeys:    next.EnvKeys,
				HeaderKeys: next.HeaderKeys,
			},
		})
	}
	if changed == 0 && len(plan.Actions) == 0 {
		plan.Notes = append(plan.Notes, "nothing to update")
	}
	return plan, nil
}

func isPackageVariant(variant string) bool {
	switch variant {
	case "npm", "pypi", "oci":
		return true
	}
	return false
}

// carryOver keeps env and header values the current config has that the
// registry does not declare, so an unchanged secret never reads as an update.
// Values the registry itself resolved win.
func carryOver(next, current *McpNormalized) {
	if len(current.Env) > 0 {
		env := map[string]string{}
		for k, v := range next.Env {
			env[k] = v
		}
		for k, v := range current.Env {
			if _, ok := env[k]; !ok {
				env[k] = v
			}
		}
		next.Env = env
		next.EnvKeys = sortedKeys(env)
	}
	if len(current.Headers) > 0 {
		headers := map[string]string{}
		for k, v := range next.Headers {
			headers[k] = v
		}
		for k, v := range current.Headers {
			if _, ok := headers[k]; !ok {
				headers[k] = v
			}
		}
		next.Headers = headers
		next.HeaderKeys = sortedKeys(headers)
	}
}

// displayVersion keeps the note readable when the lock has no version.
func displayVersion(v string) string {
	if strings.TrimSpace(v) == "" {
		return "unknown"
	}
	return v
}
