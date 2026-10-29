package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ngosangns/synca/go/internal/mcpclient"
)

// The probe cache records what `synca mcp probe` measured per server, so
// `mcp list` and the desktop app can show a real context-cost estimate
// without reconnecting:
//
//	user scope     $XDG_STATE_HOME/synca/mcp-probe.json, else ~/.agents/.mcp-probe.json
//	project scope  <project root>/.mcp-probe.json
//
// Each entry stores the fingerprint of the config it probed; a cached number
// is only served while the config still hashes to the same value. Values are
// measurements only — no env, header or URL secrets are stored.
const (
	mcpProbeName    = ".mcp-probe.json"
	mcpProbeVersion = 1
	// probeWorkers bounds how many servers are contacted at once.
	probeWorkers = 4
	// DefaultProbeTimeout bounds one server when --timeout is not given.
	DefaultProbeTimeout = 15 * time.Second
)

// McpProbeEntry is one cached probe result.
type McpProbeEntry struct {
	OK            bool   `json:"ok"`
	Error         string `json:"error,omitempty"`
	Transport     string `json:"transport,omitempty"`
	ServerName    string `json:"server_name,omitempty"`
	ServerVersion string `json:"server_version,omitempty"`
	Protocol      string `json:"protocol,omitempty"`
	Tools         int    `json:"tools"`
	Resources     int    `json:"resources"`
	Prompts       int    `json:"prompts"`
	Tokens        int    `json:"tokens"`
	ProbedAt      string `json:"probed_at"`
	Fingerprint   string `json:"fingerprint"`
}

// McpProbeOutcome is what the probe command reports per server.
type McpProbeOutcome struct {
	Key       string `json:"key"`
	OK        bool   `json:"ok"`
	Skipped   bool   `json:"skipped,omitempty"`
	Error     string `json:"error,omitempty"`
	Transport string `json:"transport,omitempty"`
	Server    string `json:"server,omitempty"`
	Version   string `json:"version,omitempty"`
	Protocol  string `json:"protocol,omitempty"`
	Tools     int    `json:"tools"`
	Resources int    `json:"resources"`
	Prompts   int    `json:"prompts"`
	Tokens    int    `json:"tokens"`
}

// McpProbeCachePath is the probe cache file for a scope, or "" when a project
// has no root.
func McpProbeCachePath(scope Scope, cwd string) string {
	if scope == ScopeUser {
		if x := os.Getenv("XDG_STATE_HOME"); x != "" {
			return filepath.Join(x, "synca", "mcp-probe.json")
		}
		return filepath.Join(HomeDir(), ".agents", mcpProbeName)
	}
	if r := ProjectRoot(cwd); r != "" {
		return filepath.Join(r, mcpProbeName)
	}
	return ""
}

func readMcpProbeCache(path string) (map[string]McpProbeEntry, error) {
	out := map[string]McpProbeEntry{}
	if path == "" {
		return out, nil
	}
	text, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if len(text) == 0 {
		return out, nil
	}
	var doc struct {
		Version int                      `json:"version"`
		Mcps    map[string]McpProbeEntry `json:"mcps"`
	}
	if err := json.Unmarshal(text, &doc); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	for key, entry := range doc.Mcps {
		out[NormalizeKey(key)] = entry
	}
	return out, nil
}

func writeMcpProbeCache(path string, entries map[string]McpProbeEntry) error {
	if path == "" {
		return nil
	}
	if parent := filepath.Dir(path); parent != "" {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return err
		}
	}
	doc := struct {
		Version int                      `json:"version"`
		Mcps    map[string]McpProbeEntry `json:"mcps"`
	}{Version: mcpProbeVersion, Mcps: entries}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWriteFile(path, data, 0o644)
}

// ReadMcpProbeCache returns the cached probe results keyed by server key.
func ReadMcpProbeCache(path string) (map[string]McpProbeEntry, error) {
	return readMcpProbeCache(path)
}

// mcpConfigFingerprint hashes the normalized config so cached results are
// dropped the moment the config changes. Marshal is deterministic: the struct
// fields are ordered and Go sorts map keys.
func mcpConfigFingerprint(n *McpNormalized) string {
	data, err := json.Marshal(n)
	if err != nil {
		return ""
	}
	return HashBytes(data)
}

// preferredMcpPresence picks the config a probe uses: the hub ("agents")
// first, else the first agent that reported one. This mirrors which copy the
// desktop app reads.
func preferredMcpPresence(entry McpEntry) *McpPresence {
	for i := range entry.Presence {
		if entry.Presence[i].Agent == "agents" {
			return &entry.Presence[i]
		}
	}
	if len(entry.Presence) > 0 {
		return &entry.Presence[0]
	}
	return nil
}

// ProbeMcps connects to the named servers (all of them when names is empty),
// measures each one, and stores the results in the probe cache. Per-server
// failures are reported in the outcome, not as an error; the error return is
// for scanning or cache-writing trouble. Disabled servers are skipped with a
// note and stay uncached.
func ProbeMcps(scope Scope, cwd string, names []string, timeout time.Duration) ([]McpProbeOutcome, error) {
	if timeout <= 0 {
		timeout = DefaultProbeTimeout
	}
	inv := ScanAll(scope, cwd)
	entries := inv.Mcps
	if len(names) > 0 {
		want := map[string]bool{}
		for _, name := range names {
			want[NormalizeKey(name)] = true
		}
		entries = nil
		for _, entry := range inv.Mcps {
			if want[NormalizeKey(entry.Key)] {
				entries = append(entries, entry)
				delete(want, NormalizeKey(entry.Key))
			}
		}
		var missing []string
		for _, name := range names {
			if want[NormalizeKey(name)] {
				missing = append(missing, name)
			}
		}
		for _, name := range missing {
			entries = append(entries, McpEntry{Key: name})
		}
	}

	outcomes := make([]McpProbeOutcome, len(entries))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < probeWorkers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				outcomes[index] = probeOne(entries[index], timeout)
			}
		}()
	}
	for index := range entries {
		jobs <- index
	}
	close(jobs)
	wg.Wait()

	cache, err := readMcpProbeCache(McpProbeCachePath(scope, cwd))
	if err != nil {
		cache = map[string]McpProbeEntry{}
	}
	now := nowUTC()
	for index, outcome := range outcomes {
		if outcome.Skipped {
			continue
		}
		presence := preferredMcpPresence(entries[index])
		if presence == nil || presence.Normalized == nil {
			continue
		}
		entry := McpProbeEntry{
			OK:            outcome.OK,
			Error:         outcome.Error,
			Transport:     outcome.Transport,
			ServerName:    outcome.Server,
			ServerVersion: outcome.Version,
			Protocol:      outcome.Protocol,
			Tools:         outcome.Tools,
			Resources:     outcome.Resources,
			Prompts:       outcome.Prompts,
			Tokens:        outcome.Tokens,
			ProbedAt:      now,
			Fingerprint:   mcpConfigFingerprint(presence.Normalized),
		}
		cache[NormalizeKey(outcome.Key)] = entry
	}
	if err := writeMcpProbeCache(McpProbeCachePath(scope, cwd), cache); err != nil {
		return outcomes, err
	}
	return outcomes, nil
}

// probeOne measures one server. A missing config or a disabled server is a
// skipped outcome; anything else becomes ok or a failure.
func probeOne(entry McpEntry, timeout time.Duration) McpProbeOutcome {
	outcome := McpProbeOutcome{Key: entry.Key}
	presence := preferredMcpPresence(entry)
	if presence == nil || presence.Normalized == nil {
		outcome.Skipped = true
		outcome.Error = "no MCP config found for this key in this scope"
		return outcome
	}
	norm := presence.Normalized
	outcome.Transport = norm.Transport
	if norm.Enabled != nil && !*norm.Enabled {
		outcome.Skipped = true
		outcome.Error = "server is disabled"
		return outcome
	}
	spec := mcpclient.Spec{
		Transport: norm.Transport,
		Headers:   norm.Headers,
		Env:       norm.Env,
		Timeout:   timeout,
		UserAgent: "synca/" + Version,
		Version:   Version,
	}
	if norm.Transport == "stdio" {
		spec.Command = append(append([]string{}, norm.Command...), norm.Args...)
		if len(spec.Command) == 0 {
			outcome.Error = "the server has no command to run"
			return outcome
		}
	} else if norm.URL != nil {
		spec.URL = *norm.URL
	}
	ctx := context.Background()
	res := mcpclient.Probe(ctx, spec)
	outcome.OK = res.OK
	outcome.Error = res.Err
	outcome.Server = res.ServerName
	outcome.Version = res.ServerVersion
	outcome.Protocol = res.Protocol
	outcome.Tools = res.Tools
	outcome.Resources = res.Resources
	outcome.Prompts = res.Prompts
	outcome.Tokens = res.Tokens
	return outcome
}

// MergeMcpProbe copies cached probe results into entries. A cached number is
// applied only while its fingerprint still matches the current config;
// everything else stays nil so readers fall back to the config estimate.
func MergeMcpProbe(entries []McpEntry, cache map[string]McpProbeEntry) {
	if len(entries) == 0 || len(cache) == 0 {
		return
	}
	for i := range entries {
		presence := preferredMcpPresence(entries[i])
		if presence == nil || presence.Normalized == nil {
			continue
		}
		cached, ok := cache[NormalizeKey(entries[i].Key)]
		if !ok || cached.Fingerprint != mcpConfigFingerprint(presence.Normalized) {
			continue
		}
		tokens := cached.Tokens
		probedAt := cached.ProbedAt
		okFlag := cached.OK
		entries[i].Tokens = &tokens
		entries[i].ProbedAt = &probedAt
		entries[i].ProbeOK = &okFlag
		if !cached.OK && cached.Error != "" {
			message := cached.Error
			entries[i].ProbeError = &message
		}
	}
}
