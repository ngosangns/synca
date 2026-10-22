package cli

import (
	"errors"
	"strings"

	"github.com/ngosangns/synca/go/internal/core"
)

type syncConflict struct {
	Skill  bool   `json:"skill"`
	Key    string `json:"key"`
	Policy string `json:"policy"`
}

type syncDocument struct {
	DryRun     bool              `json:"dryRun"`
	Scope      string            `json:"scope"`
	OnConflict string            `json:"onConflict"`
	Actions    []core.SyncAction `json:"actions"`
	Conflicts  []syncConflict    `json:"conflicts"`
}

type syncAppliedDocument struct {
	syncDocument
	Applied []string `json:"applied"`
}

type manageDocument struct {
	DryRun  bool                `json:"dryRun"`
	Scope   string              `json:"scope"`
	Actions []core.ManageAction `json:"actions"`
	Notes   []string            `json:"notes"`
}

type manageAppliedDocument struct {
	manageDocument
	Applied []string `json:"applied"`
}

type providerDocument struct {
	DryRun   bool     `json:"dryRun"`
	Scope    string   `json:"scope"`
	Provider string   `json:"provider"`
	Lines    []string `json:"lines"`
}

func syncConflicts(plan *core.SyncPlan, policy string) []syncConflict {
	out := []syncConflict{}
	if plan == nil {
		return out
	}
	for _, action := range plan.Actions {
		switch action.Kind {
		case "conflict_skill":
			out = append(out, syncConflict{Skill: true, Key: action.SkillKey, Policy: policy})
		case "conflict_mcp":
			out = append(out, syncConflict{Skill: false, Key: action.Server, Policy: policy})
		}
	}
	return out
}

func writeSyncJSON(plan *core.SyncPlan, policy string, dry bool, applied []string) error {
	actions := plan.Actions
	if actions == nil {
		actions = []core.SyncAction{}
	}
	doc := syncDocument{
		DryRun:     dry,
		Scope:      plan.Scope,
		OnConflict: policy,
		Actions:    actions,
		Conflicts:  syncConflicts(plan, policy),
	}
	if dry {
		return writeJSON(doc)
	}
	if applied == nil {
		applied = []string{}
	}
	return writeJSON(syncAppliedDocument{syncDocument: doc, Applied: applied})
}

func writeManageJSON(plan *core.ManagePlan, dry bool, log []string) error {
	actions := plan.Actions
	if actions == nil {
		actions = []core.ManageAction{}
	}
	notes := plan.Notes
	if notes == nil {
		notes = []string{}
	}
	doc := manageDocument{DryRun: dry, Scope: plan.Scope, Actions: actions, Notes: notes}
	if dry {
		return writeJSON(doc)
	}
	if log == nil {
		log = []string{}
	}
	return writeJSON(manageAppliedDocument{manageDocument: doc, Applied: log})
}

func finishManage(plan *core.ManagePlan, log []string, dry, jsonOut bool) error {
	if jsonOut {
		return writeManageJSON(plan, dry, log)
	}
	printManagePlan(plan)
	return printPlanResult(log, dry, "")
}

// asCommandErr maps a core error onto a contract exit code.
func asCommandErr(jsonOut bool, err error) error {
	if err == nil {
		return nil
	}
	var exit *ExitError
	if errorsAsExit(err, &exit) {
		return err
	}
	msg := err.Error()
	code := ExitFailed
	hint := ""
	switch {
	case strings.HasPrefix(msg, "usage:"),
		strings.HasPrefix(msg, "unknown "),
		strings.HasPrefix(msg, "invalid "),
		strings.Contains(msg, "empty skill source"):
		code = ExitUsage
	case strings.Contains(msg, "no project root"),
		strings.Contains(msg, "not found"),
		strings.Contains(msg, "no SKILL.md"),
		strings.Contains(msg, "not a skill directory"),
		strings.Contains(msg, "HOME is not set"),
		strings.HasPrefix(msg, "no MCP server named "),
		strings.HasPrefix(msg, "no skill named "):
		code = ExitUnavailable
		if strings.Contains(msg, "no project root") {
			hint = "run inside a git repository or pass --cwd"
		}
	}
	return failCode(jsonOut, code, msg, hint)
}

func errorsAsExit(err error, target **ExitError) bool {
	return errors.As(err, target)
}
