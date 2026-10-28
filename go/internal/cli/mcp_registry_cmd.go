package cli

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ngosangns/synca/go/internal/core"
	"github.com/ngosangns/synca/go/internal/mcpregistry"
)

// mcpBoolFlags is commonBool without "version": for mcp, --version takes a
// value (the registry version), so it must not be parsed as a boolean.
func mcpBoolFlags() map[string]bool {
	out := make(map[string]bool, len(commonBool))
	for k, v := range commonBool {
		if k == "version" {
			continue
		}
		out[k] = v
	}
	return out
}

func mcpRegistryErr(jsonOut bool, err error) error {
	var re *mcpregistry.Error
	if !errors.As(err, &re) {
		return asCommandErr(jsonOut, err)
	}
	switch re.Kind {
	case mcpregistry.KindUsage:
		return failUsage(jsonOut, re.Msg, nonEmpty(re.Hint, "see: synca mcp --help"))
	case mcpregistry.KindUnauthorized:
		return failCode(jsonOut, ExitUnauthorized, re.Msg, re.Hint)
	default:
		return failCode(jsonOut, ExitUnavailable, re.Msg, re.Hint)
	}
}

func runMcpSearch(f *flags, jsonOut bool) error {
	query := strings.TrimSpace(strings.Join(f.pos, " "))
	if query == "" {
		return failUsage(jsonOut, "usage: synca mcp search <query> [--limit N] [--cursor C] [--json]", "see: synca mcp --help")
	}
	return runMcpRegistryList(f, jsonOut, query)
}

func runMcpBrowse(f *flags, jsonOut bool) error {
	if len(f.pos) > 0 {
		return failUsage(jsonOut, "usage: synca mcp browse [--limit N] [--cursor C] [--json]", "see: synca mcp --help")
	}
	return runMcpRegistryList(f, jsonOut, "")
}

func runMcpRegistryList(f *flags, jsonOut bool, query string) error {
	limit, err := intFlag(f, "limit", 0)
	if err != nil {
		return failUsage(jsonOut, err.Error(), "see: synca mcp --help")
	}
	cursor := f.str("cursor", "")
	ctx, cancel := registryCtx()
	defer cancel()
	client := mcpregistry.New(core.Version)
	opts := mcpregistry.ListOptions{Cursor: cursor, Limit: limit}
	var res *mcpregistry.ListResponse
	if query != "" {
		res, err = client.Search(ctx, query, opts)
	} else {
		res, err = client.List(ctx, opts)
	}
	if err != nil {
		return mcpRegistryErr(jsonOut, err)
	}
	servers := make([]mcpRegistrySummary, 0, len(res.Servers))
	for _, rec := range res.Servers {
		servers = append(servers, mcpRegistrySummaryOf(rec))
	}
	if jsonOut {
		if query != "" {
			return writeJSON(mcpRegistrySearchDocument{
				Query: query, Cursor: cursor, Count: len(servers),
				NextCursor: res.Metadata.NextCursor, Servers: servers,
			})
		}
		return writeJSON(mcpRegistryBrowseDocument{
			Cursor: cursor, Count: len(servers),
			NextCursor: res.Metadata.NextCursor, Servers: servers,
		})
	}
	if query != "" {
		fmt.Printf("Search %q (%d server(s))\n", query, len(servers))
	} else {
		fmt.Printf("Browse latest (%d server(s))\n", len(servers))
	}
	printMcpRegistrySummaries(servers)
	printMcpRegistryNext(res.Metadata.NextCursor, query != "")
	return nil
}

func printMcpRegistrySummaries(servers []mcpRegistrySummary) {
	for _, s := range servers {
		line := s.Name
		if s.Version != "" {
			line += "  " + s.Version
		}
		if s.Title != "" {
			line += "  " + s.Title
		}
		fmt.Printf("  %s\n", line)
	}
}

func printMcpRegistryNext(next string, search bool) {
	if next == "" {
		return
	}
	cmd := "synca mcp browse"
	if search {
		cmd = "synca mcp search <query>"
	}
	fmt.Printf("next: %s --cursor %s\n", cmd, quoteArg(next))
}

func runMcpInfo(f *flags, jsonOut bool) error {
	if len(f.pos) != 1 {
		return failUsage(jsonOut, "usage: synca mcp info <name> [--version V] [--json]", "see: synca mcp --help")
	}
	ctx, cancel := registryCtx()
	defer cancel()
	rec, err := mcpregistry.New(core.Version).Info(ctx, f.pos[0], f.str("version", ""))
	if err != nil {
		return mcpRegistryErr(jsonOut, err)
	}
	doc := mcpRegistryInfoOf(*rec)
	if jsonOut {
		return writeJSON(mcpRegistryInfoDocument{Server: doc})
	}
	fmt.Printf("%s  %s", doc.Name, doc.Version)
	if doc.Title != "" {
		fmt.Printf("  %s", doc.Title)
	}
	fmt.Println()
	if doc.Description != "" {
		fmt.Printf("  %s\n", doc.Description)
	}
	if doc.RepositoryURL != "" {
		fmt.Printf("  repository %s\n", doc.RepositoryURL)
	}
	if doc.WebsiteURL != "" {
		fmt.Printf("  website %s\n", doc.WebsiteURL)
	}
	for _, p := range doc.Packages {
		fmt.Printf("  package %s %s\n", p.RegistryType, p.Identifier)
		if p.Version != "" {
			fmt.Printf("    version %s\n", p.Version)
		}
		if len(p.EnvKeys) > 0 {
			fmt.Printf("    env %s\n", strings.Join(p.EnvKeys, ", "))
		}
	}
	for _, r := range doc.Remotes {
		fmt.Printf("  remote %s %s\n", r.Type, r.URL)
		if len(r.Variables) > 0 {
			fmt.Printf("    variables %s\n", strings.Join(r.Variables, ", "))
		}
	}
	return nil
}

func runMcpInstall(f *flags, jsonOut bool) error {
	if len(f.pos) != 1 {
		return failUsage(jsonOut, "usage: synca mcp install <name> [--version V] [--name KEY] [--package npm|pypi|oci] [--transport stdio|http|sse] [--env KEY=VAL]... [--arg NAME=VAL]... [--dry-run]", "see: synca mcp --help")
	}
	scope, err := scopeOf(f)
	if err != nil {
		return failUsage(jsonOut, err.Error(), "see: synca mcp --help")
	}
	filter, err := agentsFilter(f)
	if err != nil {
		return asCommandErr(jsonOut, err)
	}
	env, _, err := parseEnvPairs(f.lists["env"])
	if err != nil {
		return failUsage(jsonOut, err.Error(), "see: synca mcp --help")
	}
	args, err := parseArgPairs(f.lists["arg"])
	if err != nil {
		return failUsage(jsonOut, err.Error(), "see: synca mcp --help")
	}
	ctx, cancel := registryCtx()
	defer cancel()
	rec, err := mcpregistry.New(core.Version).Info(ctx, f.pos[0], f.str("version", ""))
	if err != nil {
		return mcpRegistryErr(jsonOut, err)
	}
	key, norm, notes, err := core.McpFromRegistry(&rec.Server, core.McpRegistryOptions{
		Key:         f.str("name", ""),
		PackageType: f.str("package", ""),
		Transport:   f.str("transport", ""),
		Env:         env,
		Args:        args,
		Enabled:     f.boolVal("enabled"),
	})
	if err != nil {
		return failUsage(jsonOut, err.Error(), "see: synca mcp --help")
	}
	dry := f.bools["dry-run"]
	if !f.bools["force"] {
		for _, e := range core.ScanMcp(scope, cwdOf(f)) {
			if e.Key != key {
				continue
			}
			if !dry {
				return failUsage(jsonOut,
					fmt.Sprintf("mcp %q already exists; pass --force to overwrite or install under another --name", key),
					"see: synca mcp --help")
			}
			notes = append(notes, fmt.Sprintf("mcp %q already exists; apply needs --force to overwrite", key))
			break
		}
	}
	plan, log, err := core.AddMcp(scope, cwdOf(f), key, norm, filter, dry)
	if err != nil {
		return applyFailed(jsonOut, log, err)
	}
	plan.Notes = append(plan.Notes, notes...)
	return finishManage(plan, log, dry, jsonOut)
}

func parseArgPairs(pairs []string) (map[string]string, error) {
	out := map[string]string{}
	for _, pair := range pairs {
		key, val, ok := strings.Cut(pair, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("invalid --arg %s (use NAME=VALUE)", quoteArg(pair))
		}
		out[key] = val
	}
	return out, nil
}

type mcpRegistrySummary struct {
	Name            string   `json:"name"`
	Title           string   `json:"title"`
	Description     string   `json:"description"`
	Version         string   `json:"version"`
	Status          string   `json:"status"`
	IsLatest        bool     `json:"isLatest"`
	StatusChangedAt string   `json:"statusChangedAt"`
	PublishedAt     string   `json:"publishedAt"`
	UpdatedAt       string   `json:"updatedAt"`
	RepositoryURL   string   `json:"repositoryUrl"`
	WebsiteURL      string   `json:"websiteUrl"`
	PackageTypes    []string `json:"packageTypes"`
	RemoteTypes     []string `json:"remoteTypes"`
}

type mcpRegistrySearchDocument struct {
	Query      string               `json:"query"`
	Cursor     string               `json:"cursor"`
	Count      int                  `json:"count"`
	NextCursor string               `json:"nextCursor"`
	Servers    []mcpRegistrySummary `json:"servers"`
}

type mcpRegistryBrowseDocument struct {
	Cursor     string               `json:"cursor"`
	Count      int                  `json:"count"`
	NextCursor string               `json:"nextCursor"`
	Servers    []mcpRegistrySummary `json:"servers"`
}

type mcpRegistryOfficialInfo struct {
	Status          string `json:"status"`
	IsLatest        bool   `json:"isLatest"`
	StatusChangedAt string `json:"statusChangedAt"`
	PublishedAt     string `json:"publishedAt"`
	UpdatedAt       string `json:"updatedAt"`
}

type mcpRegistryPackageInfo struct {
	RegistryType    string   `json:"registryType"`
	RegistryBaseURL string   `json:"registryBaseUrl"`
	Identifier      string   `json:"identifier"`
	Version         string   `json:"version"`
	RuntimeHint     string   `json:"runtimeHint"`
	Transport       string   `json:"transport"`
	EnvKeys         []string `json:"envKeys"`
}

type mcpRegistryRemoteInfo struct {
	Type        string   `json:"type"`
	URL         string   `json:"url"`
	HeaderNames []string `json:"headerNames"`
	Variables   []string `json:"variables"`
}

type mcpRegistryInfoServer struct {
	mcpRegistrySummary
	Packages []mcpRegistryPackageInfo `json:"packages"`
	Remotes  []mcpRegistryRemoteInfo  `json:"remotes"`
	Official *mcpRegistryOfficialInfo `json:"official"`
}

type mcpRegistryInfoDocument struct {
	Server mcpRegistryInfoServer `json:"server"`
}

func mcpRegistrySummaryOf(rec mcpregistry.ServerRecord) mcpRegistrySummary {
	s := rec.Server
	out := mcpRegistrySummary{
		Name:          s.Name,
		Title:         s.Title,
		Description:   s.Description,
		Version:       s.Version,
		RepositoryURL: mcpRegistryRepositoryURL(&s),
		WebsiteURL:    core.MaskURL(s.WebsiteURL),
		PackageTypes:  mcpRegistryPackageTypes(&s),
		RemoteTypes:   mcpRegistryRemoteTypes(&s),
	}
	if o := rec.Meta.Official; o != nil {
		out.Status = o.Status
		out.IsLatest = o.IsLatest
		out.StatusChangedAt = o.StatusChangedAt
		out.PublishedAt = o.PublishedAt
		out.UpdatedAt = o.UpdatedAt
	}
	return out
}

func mcpRegistryInfoOf(rec mcpregistry.ServerRecord) mcpRegistryInfoServer {
	s := rec.Server
	out := mcpRegistryInfoServer{
		mcpRegistrySummary: mcpRegistrySummaryOf(rec),
		Packages:           []mcpRegistryPackageInfo{},
		Remotes:            []mcpRegistryRemoteInfo{},
	}
	for _, p := range s.Packages {
		keys := make([]string, 0, len(p.EnvironmentVariables))
		for _, e := range p.EnvironmentVariables {
			keys = append(keys, e.Name)
		}
		sort.Strings(keys)
		out.Packages = append(out.Packages, mcpRegistryPackageInfo{
			RegistryType:    p.RegistryType,
			RegistryBaseURL: core.MaskURL(p.RegistryBaseURL),
			Identifier:      p.Identifier,
			Version:         p.Version,
			RuntimeHint:     p.RuntimeHint,
			Transport:       p.Transport.Type,
			EnvKeys:         keys,
		})
	}
	for _, r := range s.Remotes {
		headers := make([]string, 0, len(r.Headers))
		for _, h := range r.Headers {
			headers = append(headers, h.Name)
		}
		vars := sortedStringKeys(r.Variables)
		out.Remotes = append(out.Remotes, mcpRegistryRemoteInfo{
			Type:        r.Type,
			URL:         core.MaskURL(r.URL),
			HeaderNames: headers,
			Variables:   vars,
		})
	}
	if o := rec.Meta.Official; o != nil {
		out.Official = &mcpRegistryOfficialInfo{
			Status:          o.Status,
			IsLatest:        o.IsLatest,
			StatusChangedAt: o.StatusChangedAt,
			PublishedAt:     o.PublishedAt,
			UpdatedAt:       o.UpdatedAt,
		}
	}
	return out
}

func mcpRegistryRepositoryURL(s *mcpregistry.Server) string {
	if s.Repository == nil {
		return ""
	}
	return core.MaskURL(s.Repository.URL)
}

func sortedStringKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func mcpRegistryPackageTypes(s *mcpregistry.Server) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, p := range s.Packages {
		t := strings.ToLower(strings.TrimSpace(p.RegistryType))
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
}

func mcpRegistryRemoteTypes(s *mcpregistry.Server) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, r := range s.Remotes {
		t := strings.ToLower(strings.TrimSpace(r.Type))
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
}
