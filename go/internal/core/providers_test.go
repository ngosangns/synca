package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProvidersTableIsValidAndMapsEverySyncaAgent(t *testing.T) {
	doc, err := ListProviders(ScopeUser, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if doc.Total != len(doc.Providers) || doc.Total < 70 {
		t.Fatalf("total %d / %d", doc.Total, len(doc.Providers))
	}
	managed := map[string]string{}
	seen := map[string]bool{}
	for i, p := range doc.Providers {
		if p.ID == "" || p.Name == "" || seen[p.ID] {
			t.Fatalf("bad or duplicate row %+v", p)
		}
		seen[p.ID] = true
		if i > 0 && lower(doc.Providers[i-1].Name) > lower(p.Name) {
			t.Errorf("not sorted: %s before %s", doc.Providers[i-1].Name, p.Name)
		}
		if p.SyncaAgent != nil {
			managed[*p.SyncaAgent] = p.ID
		}
		if p.Managed != (p.SyncaAgent != nil) {
			t.Errorf("%s: managed %v with agent %v", p.ID, p.Managed, p.SyncaAgent)
		}
	}
	// Every agent synca writes skills for is in the table, once.
	for _, a := range AllAgents() {
		if a == AgentAgents {
			continue
		}
		if managed[a.String()] == "" {
			t.Errorf("synca agent %q is not mapped to a skills.sh agent", a)
		}
	}
	if len(managed) != len(AllAgents())-1 {
		t.Errorf("managed %v", managed)
	}
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func TestProviderPathsMatchSyncaSkillRoots(t *testing.T) {
	home := realTempHome(t)
	for _, scope := range []Scope{ScopeUser, ScopeProject} {
		proj := filepath.Join(home, "proj")
		mustMkdir(t, filepath.Join(proj, ".git"))
		doc, err := ListProviders(scope, proj)
		if err != nil {
			t.Fatal(err)
		}
		byAgent := map[string]ProviderInfo{}
		for _, p := range doc.Providers {
			if p.SyncaAgent != nil {
				byAgent[*p.SyncaAgent] = p
			}
		}
		for _, root := range SkillRoots(scope, proj) {
			if root.Agent == AgentAgents {
				continue
			}
			p := byAgent[root.Agent.String()]
			if p.Path == nil || *p.Path != root.Path {
				var got string
				if p.Path != nil {
					got = *p.Path
				}
				t.Errorf("%s %s: providers says %q, SkillRoots says %q", scope, root.Agent, got, root.Path)
			}
		}
	}
}

func TestProviderPresenceAndSkillCount(t *testing.T) {
	home := realTempHome(t)
	mustWrite(t, filepath.Join(home, ".claude/skills/a/SKILL.md"), md("a", "A"))
	mustWrite(t, filepath.Join(home, ".claude/skills/b/SKILL.md"), md("b", "B"))
	mustWrite(t, filepath.Join(home, ".claude/skills/notaskill/readme.md"), "x")
	mustWrite(t, filepath.Join(home, ".claude/skills/.synca-stage-1/SKILL.md"), md("tmp", "T"))
	mustMkdir(t, filepath.Join(home, ".kiro/skills"))
	doc, err := ListProviders(ScopeUser, home)
	if err != nil {
		t.Fatal(err)
	}
	get := func(id string) ProviderInfo {
		for _, p := range doc.Providers {
			if p.ID == id {
				return p
			}
		}
		t.Fatalf("no provider %s", id)
		return ProviderInfo{}
	}
	if c := get("claude-code"); !c.Present || c.Skills != 2 || !c.Managed {
		t.Errorf("claude %+v", c)
	}
	if k := get("kiro-cli"); !k.Present || k.Skills != 0 {
		t.Errorf("kiro %+v", k)
	}
	if g := get("grok"); g.Present || g.Skills != 0 {
		t.Errorf("grok %+v", g)
	}
	if e := get("eve"); e.Path != nil || e.Present {
		t.Errorf("eve has no global dir: %+v", e)
	}
}

func TestProjectScopeOutsideARepoHasNoPaths(t *testing.T) {
	home := realTempHome(t)
	doc, err := ListProviders(ScopeProject, home)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range doc.Providers {
		if p.Path != nil || p.Present {
			t.Fatalf("%s has a path outside a repository: %v", p.ID, *p.Path)
		}
	}
	if doc.Scope != "project" {
		t.Errorf("scope %s", doc.Scope)
	}
}

func TestProvidersDocumentRoundTrips(t *testing.T) {
	doc, err := ListProviders(ScopeUser, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var back ProvidersDoc
	if err := json.Unmarshal(b, &back); err != nil || back.Total != doc.Total {
		t.Fatalf("round trip: %v", err)
	}
	if _, err := os.Stat("providers.json"); err != nil {
		t.Fatal(err)
	}
}
