package core

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func nowUTC() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

type updateGroup struct {
	url, ref, source, typ string
	keys                  []string
}

// remoteLocks lists the lock entries synca can fetch again, sorted by key.
// names limits the list when it is not empty.
func remoteLocks(scope Scope, cwd string, names []string) (keys []string, locks map[string]LockEntry, missing []string) {
	locks = ReadLock(scope, cwd)
	if len(names) == 0 {
		for k, e := range locks {
			if e.Remote() {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		return keys, locks, nil
	}
	seen := map[string]bool{}
	for _, n := range names {
		k := NormalizeKey(n)
		if seen[k] {
			continue
		}
		seen[k] = true
		if e, ok := locks[k]; ok && e.Remote() {
			keys = append(keys, k)
		} else {
			missing = append(missing, n)
		}
	}
	return keys, locks, missing
}

func gitURLFor(e LockEntry) string {
	if e.SourceURL != "" {
		return e.SourceURL
	}
	if e.SourceType == "github" && e.Source != "" {
		return "https://github.com/" + e.Source + ".git"
	}
	return ""
}

// PlanUpdateSkills clones each source once, compares the skill folder with the
// version the lock file records, and plans a reinstall for every skill that
// changed. With apply set the plan is applied before the clones are removed.
// names limits the check to those skills.
func PlanUpdateSkills(scope Scope, cwd string, names []string, agentsFilter []AgentKind, apply bool) (*ManagePlan, []string, error) {
	if CanonicalSkillsDir(scope, cwd) == "" {
		return nil, nil, fmt.Errorf("no project root for project scope")
	}
	keys, locks, missing := remoteLocks(scope, cwd, names)
	plan := &ManagePlan{Scope: scope.String(), DryRun: !apply, Notes: []string{}}
	for _, n := range missing {
		return nil, nil, fmt.Errorf("no skill named %s with a remote origin", n)
	}
	if len(keys) == 0 {
		plan.Notes = append(plan.Notes, "no skills with a remote origin")
		return plan, nil, nil
	}

	installed := map[string]bool{}
	for _, e := range ScanSkills(scope, cwd) {
		installed[e.Key] = true
	}

	groups := map[string]*updateGroup{}
	var order []string
	for _, k := range keys {
		e := locks[k]
		if !installed[k] {
			plan.Notes = append(plan.Notes, fmt.Sprintf("skipped: %s (not installed; run skills restore)", k))
			continue
		}
		if e.SourceType != "github" && e.SourceType != "git" {
			plan.Notes = append(plan.Notes, fmt.Sprintf("skipped: %s (%s sources cannot be checked)", k, e.SourceType))
			continue
		}
		url := gitURLFor(e)
		if url == "" || e.SkillPath == "" || (e.FolderHash == "" && e.ComputedHash == "") {
			plan.Notes = append(plan.Notes, fmt.Sprintf("skipped: %s (the lock file has no version to compare)", k))
			continue
		}
		id := url + "\n" + e.Ref
		g := groups[id]
		if g == nil {
			g = &updateGroup{url: url, ref: e.Ref, source: e.Source, typ: e.SourceType}
			groups[id] = g
			order = append(order, id)
		}
		g.keys = append(g.keys, k)
	}

	var cleanups []func()
	defer func() {
		for _, c := range cleanups {
			c()
		}
	}()
	var failures []error
	for _, id := range order {
		g := groups[id]
		if g.typ == "github" && g.ref == "" && !registryOff() {
			rest, subs, notes, fails := updateViaRegistry(scope, cwd, g, locks, agentsFilter, &cleanups)
			plan.Notes = append(plan.Notes, notes...)
			plan.Actions = append(plan.Actions, subs...)
			failures = append(failures, fails...)
			g.keys = rest
			if len(g.keys) == 0 {
				continue
			}
		}
		repo, err := CloneRepo(g.url, g.ref)
		if err != nil {
			for _, k := range g.keys {
				plan.Notes = append(plan.Notes, fmt.Sprintf("skipped: %s (could not fetch %s)", k, g.source))
			}
			continue
		}
		cleanups = append(cleanups, repo.Cleanup)
		for _, k := range g.keys {
			e := locks[k]
			folder := filepath.ToSlash(filepath.Dir(e.SkillPath))
			if folder == "." {
				folder = ""
			}
			dir := filepath.Join(repo.Root, filepath.FromSlash(folder))
			if st, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil || st.IsDir() {
				// The folder moved. Look the skill up by name.
				skills, derr := DiscoverSkills(repo.Root)
				found := (*DiscoveredSkill)(nil)
				if derr == nil {
					found = findDiscovered(skills, k)
				}
				if found == nil {
					plan.Notes = append(plan.Notes, fmt.Sprintf("skipped: %s (no longer in %s)", k, g.source))
					continue
				}
				dir = found.dir
				folder = found.Path
			}
			var changed bool
			if e.FolderHash != "" && isTreeID(e.FolderHash) {
				changed = repo.treeHash(folder) != e.FolderHash
			} else {
				cur, herr := ComputeFolderHash(dir)
				if herr != nil {
					failures = append(failures, fmt.Errorf("hash %s: %w", k, herr))
					continue
				}
				changed = cur != e.ComputedHash
				if e.FolderHash != "" {
					changed = true
				}
			}
			if !changed {
				plan.Notes = append(plan.Notes, fmt.Sprintf("up to date: %s", k))
				continue
			}
			spec := SourceSpec{URL: g.url, Source: g.source, Type: g.typ, Ref: g.ref}
			origin := repo.originOf(spec, dir)
			origin.PluginName = e.PluginName
			sub, err := PlanInstallSkill(scope, cwd, dir, agentsFilter, origin)
			if err != nil {
				failures = append(failures, fmt.Errorf("plan %s: %w", k, err))
				continue
			}
			if sub.Actions[0].SkillKey != k {
				plan.Notes = append(plan.Notes, fmt.Sprintf("skipped: %s (the upstream skill is now named %s)", k, sub.Actions[0].SkillKey))
				continue
			}
			plan.Notes = append(plan.Notes, fmt.Sprintf("update: %s", k))
			plan.Actions = append(plan.Actions, sub.Actions...)
		}
	}
	var log []string
	if apply && len(plan.Actions) > 0 {
		l, err := ApplyManagePlan(plan, nil)
		log = l
		if err != nil {
			failures = append(failures, err)
		}
	}
	if len(failures) > 0 {
		return plan, log, joinErrors(failures)
	}
	return plan, log, nil
}

// PlanRestoreSkills reinstalls every skill the lock file lists as remote and
// that is missing from the canonical directory. names limits the list. A name
// that is not a remote skill is ignored. With apply set the plan is
// applied before the clones are removed.
func PlanRestoreSkills(scope Scope, cwd string, names []string, agentsFilter []AgentKind, apply bool) (*ManagePlan, []string, error) {
	if CanonicalSkillsDir(scope, cwd) == "" {
		return nil, nil, fmt.Errorf("no project root for project scope")
	}
	keys, locks, _ := remoteLocks(scope, cwd, names)
	plan := &ManagePlan{Scope: scope.String(), DryRun: !apply, Notes: []string{}}
	if len(keys) == 0 {
		plan.Notes = append(plan.Notes, "no skills with a remote origin")
		return plan, nil, nil
	}
	canonical := CanonicalSkillsDir(scope, cwd)
	var cleanups []func()
	defer func() {
		for _, c := range cleanups {
			c()
		}
	}()
	repos := map[string]*Repo{}
	var failures []error
	for _, k := range keys {
		e := locks[k]
		if _, err := os.Lstat(filepath.Join(canonical, k)); err == nil {
			plan.Notes = append(plan.Notes, fmt.Sprintf("present: %s", k))
			continue
		}
		if e.SourceType != "github" && e.SourceType != "git" {
			plan.Notes = append(plan.Notes, fmt.Sprintf("skipped: %s (%s sources cannot be restored)", k, e.SourceType))
			continue
		}
		url := gitURLFor(e)
		if url == "" {
			plan.Notes = append(plan.Notes, fmt.Sprintf("skipped: %s (the lock file has no URL)", k))
			continue
		}
		if e.SourceType == "github" && e.Ref == "" && !registryOff() {
			if res := restoreViaRegistry(e, k); res != nil {
				cleanups = append(cleanups, res.Cleanup)
				res.Origin.PluginName = e.PluginName
				sub, err := PlanInstallSkill(scope, cwd, res.Dir, agentsFilter, res.Origin)
				if err == nil && sub.Actions[0].SkillKey == k {
					plan.Notes = append(plan.Notes, fmt.Sprintf("restore: %s", k))
					plan.Actions = append(plan.Actions, sub.Actions...)
					continue
				}
			}
		}
		id := url + "\n" + e.Ref
		repo := repos[id]
		if repo == nil {
			r, err := CloneRepo(url, e.Ref)
			if err != nil {
				plan.Notes = append(plan.Notes, fmt.Sprintf("skipped: %s (could not fetch %s)", k, e.Source))
				continue
			}
			repo = r
			repos[id] = r
			cleanups = append(cleanups, r.Cleanup)
		}
		dir := ""
		if e.SkillPath != "" {
			folder := filepath.Join(repo.Root, filepath.FromSlash(filepath.ToSlash(filepath.Dir(e.SkillPath))))
			if st, err := os.Stat(filepath.Join(folder, "SKILL.md")); err == nil && !st.IsDir() {
				dir = folder
			}
		}
		if dir == "" {
			if d, err := repo.pick(k); err == nil {
				dir = d
			}
		}
		if dir == "" {
			plan.Notes = append(plan.Notes, fmt.Sprintf("skipped: %s (no longer in %s)", k, e.Source))
			continue
		}
		spec := SourceSpec{URL: url, Source: e.Source, Type: e.SourceType, Ref: e.Ref}
		origin := repo.originOf(spec, dir)
		origin.PluginName = e.PluginName
		sub, err := PlanInstallSkill(scope, cwd, dir, agentsFilter, origin)
		if err != nil {
			failures = append(failures, fmt.Errorf("plan %s: %w", k, err))
			continue
		}
		if sub.Actions[0].SkillKey != k {
			plan.Notes = append(plan.Notes, fmt.Sprintf("skipped: %s (the upstream skill is now named %s)", k, sub.Actions[0].SkillKey))
			continue
		}
		plan.Notes = append(plan.Notes, fmt.Sprintf("restore: %s", k))
		plan.Actions = append(plan.Actions, sub.Actions...)
	}
	var log []string
	if apply && len(plan.Actions) > 0 {
		l, err := ApplyManagePlan(plan, nil)
		log = l
		if err != nil {
			failures = append(failures, err)
		}
	}
	if len(failures) > 0 {
		return plan, log, joinErrors(failures)
	}
	return plan, log, nil
}

func joinErrors(errs []error) error {
	if len(errs) == 1 {
		return errs[0]
	}
	msg := fmt.Sprintf("%d problems:", len(errs))
	for _, e := range errs {
		msg += "\n" + e.Error()
	}
	return fmt.Errorf("%s", msg)
}

// InitSkill writes <dir>/<name>/SKILL.md with a frontmatter stub the author
// fills in. It refuses to overwrite an existing SKILL.md.
func InitSkill(name, dir string) (string, error) {
	slug := SkillSlug(name)
	if slug == "" || slug != NormalizeKey(name) && SkillSlug(slug) != slug {
		return "", fmt.Errorf("invalid skill name %q (use letters, digits and dashes)", name)
	}
	if dir == "" {
		dir = "."
	}
	target := filepath.Join(dir, slug)
	md := filepath.Join(target, "SKILL.md")
	if _, err := os.Stat(md); err == nil {
		return "", fmt.Errorf("%s already exists", md)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", err
	}
	body := fmt.Sprintf("---\nname: %s\ndescription: Say what this skill does and when to use it.\n---\n\n# %s\n\nWrite the instructions here.\n", slug, slug)
	if err := atomicWriteFile(md, []byte(body), 0o644); err != nil {
		return "", err
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return target, nil
	}
	return abs, nil
}

// githubFolder is the skill folder a lock entry points at ("" is the root).
func githubFolder(e LockEntry) string {
	folder := path.Dir(e.SkillPath)
	if folder == "." {
		return ""
	}
	return folder
}

// updateViaRegistry checks the skills of one GitHub repository with a single
// tree request and downloads the changed ones from skills.sh. It returns the
// keys it could not decide, which the caller checks with a clone.
func updateViaRegistry(scope Scope, cwd string, g *updateGroup, locks map[string]LockEntry, agentsFilter []AgentKind, cleanups *[]func()) (rest []string, actions []ManageAction, notes []string, failures []error) {
	owner, repo, ok := githubOwnerRepo(g.source)
	if !ok {
		return g.keys, nil, nil, nil
	}
	c := newRegistry()
	ctx, cancel := registryContext()
	tree, err := c.GitHubTree(ctx, owner, repo)
	cancel()
	if err != nil || tree.Truncated {
		return g.keys, nil, nil, nil
	}
	project := scope == ScopeProject
	for _, k := range g.keys {
		e := locks[k]
		folder := githubFolder(e)
		sha, found := tree.FolderSHA(folder)
		if !found {
			rest = append(rest, k)
			continue
		}
		if !project && isTreeID(e.FolderHash) && sha == e.FolderHash {
			notes = append(notes, fmt.Sprintf("up to date: %s", k))
			continue
		}
		res, err := fetchFolder(c, owner, repo, SkillSlug(k), folder, tree)
		if err != nil {
			rest = append(rest, k)
			continue
		}
		*cleanups = append(*cleanups, res.Cleanup)
		if project {
			cur, herr := ComputeFolderHash(res.Dir)
			if herr == nil && cur == e.ComputedHash {
				notes = append(notes, fmt.Sprintf("up to date: %s", k))
				continue
			}
		}
		res.Origin.PluginName = e.PluginName
		sub, err := PlanInstallSkill(scope, cwd, res.Dir, agentsFilter, res.Origin)
		if err != nil {
			failures = append(failures, fmt.Errorf("plan %s: %w", k, err))
			continue
		}
		if sub.Actions[0].SkillKey != k {
			rest = append(rest, k)
			continue
		}
		notes = append(notes, fmt.Sprintf("update: %s", k))
		actions = append(actions, sub.Actions...)
	}
	return rest, actions, notes, failures
}

// restoreViaRegistry downloads one skill from skills.sh by the folder the lock
// file recorded. It returns nil when the caller should clone.
func restoreViaRegistry(e LockEntry, key string) *Resolved {
	owner, repo, ok := githubOwnerRepo(e.Source)
	if !ok || e.SkillPath == "" {
		return nil
	}
	c := newRegistry()
	ctx, cancel := registryContext()
	tree, err := c.GitHubTree(ctx, owner, repo)
	cancel()
	if err != nil {
		return nil
	}
	res, err := fetchFolder(c, owner, repo, SkillSlug(key), githubFolder(e), tree)
	if err != nil {
		return nil
	}
	return res
}

// RegistrySyncResult is the registry step of a sync.
type RegistrySyncResult struct {
	// Actions are one fetch_skill per skill that is restored or updated.
	Actions []SyncAction
	// Warnings are problems that did not stop the step: an unreachable source,
	// a skill that moved. The sync of local data goes on.
	Warnings []string
	Log      []string
}

// SyncFromRegistry brings the remote skills in the lock file up to date with
// their source, through skills.sh when it can and Git otherwise. It restores
// the ones missing on disk. With update set it also reinstalls the ones whose
// source changed. key limits it to one skill. With apply unset it only plans.
// The cross-agent sync that follows links what this step installed.
func SyncFromRegistry(scope Scope, cwd string, agentsFilter []AgentKind, key string, update, apply bool) (*RegistrySyncResult, error) {
	var names []string
	if key != "" {
		names = []string{key}
	}
	res := &RegistrySyncResult{}
	reasons := map[string]string{}
	collect := func(plan *ManagePlan, log []string, verb string) {
		res.Log = append(res.Log, log...)
		for _, n := range plan.Notes {
			if strings.HasPrefix(n, "skipped: ") {
				res.Warnings = append(res.Warnings, n)
			}
		}
		for _, a := range plan.Actions {
			if a.Kind == "copy_skill" {
				reasons[a.SkillKey] = verb
				res.Actions = append(res.Actions, SyncAction{Kind: "fetch_skill", SkillKey: a.SkillKey, To: a.To, Reason: verb})
			}
		}
	}
	restore, rlog, err := PlanRestoreSkills(scope, cwd, names, agentsFilter, apply)
	if restore != nil {
		collect(restore, rlog, "restore")
	}
	if err != nil {
		if restore == nil {
			return nil, err
		}
		res.Warnings = append(res.Warnings, err.Error())
	}
	if update {
		upd, ulog, err := PlanUpdateSkills(scope, cwd, names, agentsFilter, apply)
		if upd != nil {
			collect(upd, ulog, "update")
		}
		if err != nil {
			if upd == nil {
				// A named skill that is not remote: the restore step already ignored it.
				if key == "" {
					return nil, err
				}
			} else {
				res.Warnings = append(res.Warnings, err.Error())
			}
		}
	}
	return res, nil
}
