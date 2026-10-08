package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepairsInvalidNameAndUnquotedColon(t *testing.T) {
	raw := "---\nname: Make Bot UI\ndescription: Triggers: money resolver\n---\n# body\n"
	fixed := RepairSkillFrontmatter(raw, "make-bot-ui")
	if fixed == "" {
		t.Fatal("expected repair")
	}
	for _, want := range []string{"name: make-bot-ui\n", "description: \"Triggers: money resolver\"\n", "# body\n"} {
		if !strings.Contains(fixed, want) {
			t.Errorf("missing %q in %q", want, fixed)
		}
	}
	if RepairSkillFrontmatter(fixed, "make-bot-ui") != "" {
		t.Error("repair must be idempotent")
	}
}

func TestLeavesBlockScalarsAndValidNames(t *testing.T) {
	raw := "---\nname: poteto-mode\ndescription: >-\n  line: still a block\n---\n"
	if got := RepairSkillFrontmatter(raw, "poteto-mode"); got != "" {
		t.Errorf("expected no-op, got %q", got)
	}
}

func TestSlugifiesWhenFolderNameIsAlsoInvalid(t *testing.T) {
	raw := "---\nname: Poteto Mode\n---\n"
	fixed := RepairSkillFrontmatter(raw, "Poteto Mode")
	if !strings.Contains(fixed, "name: poteto-mode\n") {
		t.Errorf("got %q", fixed)
	}
}

func TestPiSyncRepairsNamesCollapsesAliasesAndIdenticalProjectShadows(t *testing.T) {
	home := realTempHome(t)
	agents := filepath.Join(home, ".agents/skills")
	mustMkdir(t, agents)
	writeRawSkill(t, agents, "make-bot-ui", "name: Make Bot UI\ndescription: Triggers: webhook ui", "# bot")
	same := "name: design-swiftui-interfaces\ndescription: SwiftUI interfaces"
	writeRawSkill(t, agents, "design-swiftui-interfaces", same, "# swift")
	writeRawSkill(t, agents, "swiftui-interface-design", same, "# swift")
	writeRawSkill(t, agents, "but", "name: but\ndescription: git butler", "# but")

	proj := filepath.Join(home, "viclass")
	mustMkdir(t, filepath.Join(proj, ".git"))
	projSkills := filepath.Join(proj, ".agents/skills")
	writeRawSkill(t, projSkills, "but", "name: but\ndescription: git butler", "# but")
	writeRawSkill(t, projSkills, "viclass-only", "name: viclass-only\ndescription: local", "# local")
	// Same name, different tree: must stay a real project directory.
	writeRawSkill(t, projSkills, "make-bot-ui", "name: make-bot-ui\ndescription: project specific", "# different")

	plan, err := PlanSyncSkills(ScopeUser, proj, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	var repair, alias, shadow bool
	for _, a := range plan.Actions {
		switch {
		case a.Kind == "repair_skill_frontmatter" && a.SkillKey == "make-bot-ui":
			repair = true
		case a.Kind == "collapse_skill_alias" && a.SkillKey == "design-swiftui-interfaces":
			alias = true
		case a.Kind == "collapse_skill_alias" && a.SkillKey == "but" && strings.Contains(a.From, "viclass"):
			shadow = true
		}
	}
	if !repair {
		t.Errorf("missing name repair: %+v", plan.Actions)
	}
	if !alias {
		t.Errorf("missing alias collapse: %+v", plan.Actions)
	}
	if !shadow {
		t.Errorf("missing project shadow relink: %+v", plan.Actions)
	}

	if _, err := ApplyPlan(plan, proj, ScopeUser, NewDecisions(ConflictSkip)); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(filepath.Join(agents, "make-bot-ui/SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	repaired := string(b)
	if !strings.Contains(repaired, "name: make-bot-ui\n") {
		t.Errorf("name not repaired: %s", repaired)
	}
	if !strings.Contains(repaired, "description: \"Triggers: webhook ui\"\n") {
		t.Errorf("description not quoted: %s", repaired)
	}

	same1 := filepath.Join(agents, "design-swiftui-interfaces")
	same2 := filepath.Join(agents, "swiftui-interface-design")
	aliasLink, aliasTarget := same2, same1
	if !isSymlink(aliasLink) {
		t.Fatalf("duplicate skill dir %s should be a symlink", aliasLink)
	}
	r1, _ := filepath.EvalSymlinks(aliasLink)
	r2, _ := filepath.EvalSymlinks(aliasTarget)
	if r1 != r2 {
		t.Errorf("alias resolves to %s, want %s", r1, r2)
	}

	projBut := filepath.Join(projSkills, "but")
	if !isSymlink(projBut) {
		t.Fatal("identical project shadow should alias the user skill")
	}
	rb, _ := filepath.EvalSymlinks(projBut)
	ub, _ := filepath.EvalSymlinks(filepath.Join(agents, "but"))
	if rb != ub {
		t.Errorf("project but → %s, want %s", rb, ub)
	}
	if isSymlink(filepath.Join(projSkills, "viclass-only")) {
		t.Error("project-only skill must stay a real directory")
	}
	if isSymlink(filepath.Join(projSkills, "make-bot-ui")) {
		t.Error("different project tree must not be replaced")
	}
}
