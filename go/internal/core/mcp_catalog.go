package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// A catalog is a portable JSON file of MCP servers, the mcp counterpart of a
// skills.sh list: export writes the hub to it, import installs the whole set
// into the hub and agent configs. The per-server objects use the same shape
// agent configs do, so a catalog doubles as a mcpServers snippet.
//
//	env and header values are written verbatim. A catalog holds secrets;
//	keep it in a private repo.
const (
	McpCatalogVersion  = 1
	mcpCatalogMaxBytes = 8 << 20
	mcpCatalogTimeout  = 30 * time.Second
)

// McpCatalogOrigin carries the mcp-lock provenance through the catalog, so a
// re-import on another machine keeps `mcp update` working.
type McpCatalogOrigin struct {
	Source     string `json:"source,omitempty"`
	SourceType string `json:"sourceType,omitempty"`
	Version    string `json:"version,omitempty"`
}

// McpCatalogServer is one entry: the config plus an optional origin.
type McpCatalogServer struct {
	Config McpNormalized
	Origin *McpCatalogOrigin
}

func (s McpCatalogServer) MarshalJSON() ([]byte, error) {
	obj := NormalizedToJSON(&s.Config)
	if s.Origin != nil {
		obj["origin"] = s.Origin
	}
	return json.Marshal(obj)
}

func (s *McpCatalogServer) UnmarshalJSON(data []byte) error {
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}
	s.Config = *jsonToNormalized(cfg)
	if raw, ok := cfg["origin"]; ok {
		var origin McpCatalogOrigin
		if b, err := json.Marshal(raw); err == nil {
			if err := json.Unmarshal(b, &origin); err == nil {
				s.Origin = &origin
			}
		}
	}
	return nil
}

// McpCatalog is the whole file. Go's encoder sorts map keys, so output is
// stable for git diffs.
type McpCatalog struct {
	Version int                         `json:"version"`
	Servers map[string]McpCatalogServer `json:"servers"`
}

func (c *McpCatalog) UnmarshalJSON(data []byte) error {
	var doc struct {
		Version *int                       `json:"version"`
		Servers map[string]json.RawMessage `json:"servers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return err
	}
	version := McpCatalogVersion
	if doc.Version != nil {
		version = *doc.Version
	}
	if version > McpCatalogVersion {
		return fmt.Errorf("catalog version %d is newer than this synca understands (%d); update synca", version, McpCatalogVersion)
	}
	if len(doc.Servers) == 0 {
		return fmt.Errorf("catalog lists no servers")
	}
	c.Version = version
	c.Servers = make(map[string]McpCatalogServer, len(doc.Servers))
	for name, raw := range doc.Servers {
		var server McpCatalogServer
		if err := json.Unmarshal(raw, &server); err != nil {
			return fmt.Errorf("server %s: %w", name, err)
		}
		if err := validateMcpCatalogConfig(name, &server.Config); err != nil {
			return err
		}
		c.Servers[NormalizeKey(name)] = server
	}
	return nil
}

func validateMcpCatalogConfig(name string, norm *McpNormalized) error {
	switch norm.Transport {
	case "stdio":
		if len(norm.Command) == 0 {
			return fmt.Errorf("server %s: stdio config needs a command", name)
		}
	case "http", "sse":
		if norm.URL == nil || strings.TrimSpace(*norm.URL) == "" {
			return fmt.Errorf("server %s: %s config needs a url", name, norm.Transport)
		}
	default:
		return fmt.Errorf("server %s: unsupported transport %q (use stdio, http or sse)", name, norm.Transport)
	}
	return nil
}

// readOptionalMcpHub reads one MCP JSON file. A missing file is an empty map.
func readOptionalMcpHub(path string) (map[string]McpNormalized, error) {
	if path == "" {
		return map[string]McpNormalized{}, nil
	}
	servers, err := ReadMcpServers(AgentAgents, path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]McpNormalized{}, nil
		}
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return servers, nil
}

// catalogServerInstalled reports a catalog row whose origin is a registry install.
func catalogServerInstalled(origin *McpCatalogOrigin) bool {
	return origin != nil && origin.SourceType == "registry" && origin.Source != ""
}

// BuildMcpCatalog reads both homes into one catalog. A name in both files keeps
// the installed copy. Lock origins are attached either way. Missing files yield
// an empty catalog.
func BuildMcpCatalog(scope Scope, cwd string) (*McpCatalog, error) {
	installedPath := InstalledMcpPath(scope, cwd)
	localPath := LocalMcpPath(scope, cwd)
	if installedPath == "" || localPath == "" {
		return nil, fmt.Errorf("no project root for project scope")
	}
	local, err := readOptionalMcpHub(localPath)
	if err != nil {
		return nil, err
	}
	installed, err := readOptionalMcpHub(installedPath)
	if err != nil {
		return nil, err
	}
	servers := make(map[string]McpNormalized, len(local)+len(installed))
	for name, norm := range local {
		servers[name] = norm
	}
	for name, norm := range installed {
		servers[name] = norm
	}
	origins, _ := ReadMcpLock(McpLockPath(scope, cwd))
	out := &McpCatalog{Version: McpCatalogVersion, Servers: make(map[string]McpCatalogServer, len(servers))}
	for name, norm := range servers {
		key := NormalizeKey(name)
		server := McpCatalogServer{Config: norm}
		if o, ok := origins[key]; ok {
			server.Origin = &McpCatalogOrigin{Source: o.Source, SourceType: o.SourceType, Version: o.Version}
		}
		out.Servers[key] = server
	}
	return out, nil
}

// LoadMcpCatalog reads a catalog from a file path or an http(s) URL.
func LoadMcpCatalog(source string) (*McpCatalog, error) {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return nil, fmt.Errorf("usage: pass a catalog file or URL")
	}
	var (
		data []byte
		err  error
	)
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		data, err = fetchMcpCatalogURL(trimmed)
	} else {
		data, err = os.ReadFile(expandHome(trimmed, HomeDir()))
	}
	if err != nil {
		return nil, err
	}
	catalog := &McpCatalog{}
	if err := json.Unmarshal(data, catalog); err != nil {
		return nil, fmt.Errorf("%s is not a valid catalog: %w", source, err)
	}
	return catalog, nil
}

func fetchMcpCatalogURL(raw string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid catalog URL %q: %w", MaskURL(raw), err)
	}
	req.Header.Set("User-Agent", "synca/"+Version)
	client := &http.Client{Timeout: mcpCatalogTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %s", MaskURL(raw), resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, mcpCatalogMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > mcpCatalogMaxBytes {
		return nil, fmt.Errorf("catalog at %s is larger than %d MB", MaskURL(raw), mcpCatalogMaxBytes>>20)
	}
	return data, nil
}

// WriteMcpCatalogFile writes the catalog with mode 0600 — it holds secrets.
func WriteMcpCatalogFile(path string, catalog *McpCatalog) (string, error) {
	path = expandHome(strings.TrimSpace(path), HomeDir())
	if path == "" {
		return "", fmt.Errorf("usage: --out needs a file path")
	}
	data, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	if err := atomicWriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// McpCatalogDiff summarizes what import would do to one server.
type McpCatalogDiff struct {
	Key    string `json:"key"`
	Action string `json:"action"`
}

// PlanMcpCatalogImport builds one manage plan that upserts every catalog
// server into the hub and the selected agent configs. Servers already matching
// the hub are skipped, and a value-only env change is an update of keys, not a
// secret clobber: the fingerprint covers env names only.
func PlanMcpCatalogImport(scope Scope, cwd, source string, catalog *McpCatalog, agentsFilter []AgentKind) (*ManagePlan, []McpCatalogDiff, error) {
	installedPath := InstalledMcpPath(scope, cwd)
	localPath := LocalMcpPath(scope, cwd)
	if installedPath == "" || localPath == "" {
		return nil, nil, fmt.Errorf("no project root for project scope")
	}
	installedServers, err := readOptionalMcpHub(installedPath)
	if err != nil {
		return nil, nil, err
	}
	localServers, err := readOptionalMcpHub(localPath)
	if err != nil {
		return nil, nil, err
	}
	plan := &ManagePlan{
		Scope:  scope.String(),
		DryRun: true,
		Notes:  []string{fmt.Sprintf("import mcp catalog from %s", source)},
	}
	keys := make([]string, 0, len(catalog.Servers))
	for key := range catalog.Servers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var diffs []McpCatalogDiff
	counts := map[string]int{}
	lockPath := McpLockPath(scope, cwd)
	for _, key := range keys {
		server := catalog.Servers[key]
		norm := server.Config
		hub := localPath
		existing := localServers
		if catalogServerInstalled(server.Origin) {
			hub = installedPath
			existing = installedServers
		}
		action := "add"
		if old, ok := existing[key]; ok {
			if McpFingerprint(&old) == McpFingerprint(&norm) {
				action = "same"
			} else {
				action = "update"
			}
		}
		counts[action]++
		diffs = append(diffs, McpCatalogDiff{Key: key, Action: action})
		if action == "same" {
			continue
		}
		note := fmt.Sprintf("%s %s (%s)", action, key, norm.Transport)
		var extra []string
		if len(norm.EnvKeys) > 0 {
			extra = append(extra, "env "+strings.Join(norm.EnvKeys, ", "))
		}
		if len(norm.HeaderKeys) > 0 {
			extra = append(extra, "headers "+strings.Join(norm.HeaderKeys, ", "))
		}
		if len(extra) > 0 {
			note += ": " + strings.Join(extra, "; ")
		}
		plan.Notes = append(plan.Notes, note)
		payload := norm
		plan.Actions = append(plan.Actions,
			ManageAction{Kind: "upsert_mcp_hub", Hub: hub, Server: key, Mcp: &payload},
		)
		for _, r := range McpConfigPaths(scope, cwd) {
			if pathsEqual(r.Path, installedPath) || pathsEqual(r.Path, localPath) {
				continue
			}
			if agentsFilter != nil && !agentIn(r.Agent, agentsFilter) {
				continue
			}
			plan.Actions = append(plan.Actions, ManageAction{
				Kind: "write_mcp_agent", Path: r.Path, Agent: r.Agent.String(), Server: key, Mcp: &payload,
			})
		}
		if server.Origin != nil && lockPath != "" {
			plan.Actions = append(plan.Actions, ManageAction{
				Kind:   "record_mcp_origin",
				Path:   lockPath,
				Server: key,
				McpOrigin: &McpLockEntry{
					Source:     server.Origin.Source,
					SourceType: server.Origin.SourceType,
					Version:    server.Origin.Version,
				},
			})
		}
	}
	plan.Notes = append(plan.Notes, fmt.Sprintf(
		"%d server(s): %d add, %d update, %d unchanged",
		len(keys), counts["add"], counts["update"], counts["same"]))
	if len(plan.Actions) == 0 {
		plan.Notes = append(plan.Notes, "every server is up to date; nothing to import")
	} else {
		plan.Notes = append(plan.Notes, "env and header values are written verbatim; keep the catalog private")
	}
	return plan, diffs, nil
}
