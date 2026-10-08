package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ngosangns/synca/go/internal/core"
)

// ---------- state (port of state.rs) ----------

type page int

const (
	pageUser page = iota
	pageProject
)

func (p page) scope() core.Scope {
	if p == pageProject {
		return core.ScopeProject
	}
	return core.ScopeUser
}
func (p page) label() string {
	if p == pageProject {
		return "Project"
	}
	return "User"
}
func (p page) toggle() page {
	if p == pageUser {
		return pageProject
	}
	return pageUser
}

type section int

const (
	sectionSkills section = iota
	sectionMcps
)

func (s section) toggle() section {
	if s == sectionSkills {
		return sectionMcps
	}
	return sectionSkills
}
func (s section) label() string {
	if s == sectionMcps {
		return "MCPs"
	}
	return "Skills"
}

type pendingKind int

const (
	pendNone pendingKind = iota
	pendSyncConfirm
	pendResolveConflict
	pendUpdateInstall
	pendInstallSkillInput
	pendInstallSkillConfirm
	pendInstallMcpName
	pendInstallMcpTransport
	pendInstallMcpEndpoint
	pendInstallMcpConfirm
	pendDeleteSkillConfirm
	pendDeleteSkillPurgeConfirm
	pendDeleteMcpConfirm
)

type conflictItem struct {
	isMcp bool
	key   string
}

type pending struct {
	kind      pendingKind
	buffer    string
	remaining []conflictItem
	source    string
	key       string
	name      string
	transport string
	endpoint  string
}

type rect struct {
	x, y, w, h int
}

func (r rect) contains(col, row int) bool {
	return col >= r.x && col < r.x+r.w && row >= r.y && row < r.y+r.h
}

type layout struct {
	header, skills, mcps, detail, userTab, projectTab rect
}

type model struct {
	cwd            string
	page           page
	section        section
	userInv        core.Inventory
	projectInv     core.Inventory
	skillIdx       int
	mcpIdx         int
	skillScroll    int
	mcpScroll      int
	skillViewport  int
	mcpViewport    int
	detailScroll   int
	status         string
	help           bool
	pending        *pending
	lastPlan       *core.SyncPlan
	skillDecisions map[string]core.ConflictPolicy
	mcpDecisions   map[string]core.ConflictPolicy
	projectOK      bool
	updateMsg      string
	layout         layout
	width, height  int
	quitting       bool
}

func newModel(cwd string) model {
	m := model{
		cwd:            cwd,
		page:           pageUser,
		section:        sectionSkills,
		status:         "Ready.",
		skillDecisions: map[string]core.ConflictPolicy{},
		mcpDecisions:   map[string]core.ConflictPolicy{},
	}
	m.reload()
	return m
}

func (m *model) reload() {
	m.userInv = core.ScanAll(core.ScopeUser, m.cwd)
	m.projectInv = core.ScanAll(core.ScopeProject, m.cwd)
	m.projectOK = m.projectInv.ProjectRoot != nil
	m.clampIdx()
	m.detailScroll = 0
	m.syncScroll()
}

func (m *model) inv() *core.Inventory {
	if m.page == pageUser {
		return &m.userInv
	}
	return &m.projectInv
}

func (m *model) skills() []core.SkillEntry { return m.inv().Skills }
func (m *model) mcps() []core.McpEntry     { return m.inv().Mcps }

func (m *model) clampIdx() {
	if n := len(m.skills()); n == 0 {
		m.skillIdx = 0
	} else if m.skillIdx >= n {
		m.skillIdx = n - 1
	}
	if n := len(m.mcps()); n == 0 {
		m.mcpIdx = 0
	} else if m.mcpIdx >= n {
		m.mcpIdx = n - 1
	}
}

func ensureVisible(selected, offset, visible, length int) int {
	if length == 0 || visible == 0 {
		return 0
	}
	if selected > length-1 {
		selected = length - 1
	}
	maxOffset := length - visible
	if maxOffset < 0 {
		maxOffset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	if selected < offset {
		offset = selected
	} else if selected >= offset+visible {
		offset = selected + 1 - visible
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	return offset
}

func (m *model) syncScroll() {
	m.skillScroll = ensureVisible(m.skillIdx, m.skillScroll, max(m.skillViewport, 1), len(m.skills()))
	m.mcpScroll = ensureVisible(m.mcpIdx, m.mcpScroll, max(m.mcpViewport, 1), len(m.mcps()))
}

func (m *model) onContextChange() {
	m.clampIdx()
	m.detailScroll = 0
	m.syncScroll()
}

func (m *model) moveSel(delta int) {
	switch m.section {
	case sectionSkills:
		n := len(m.skills())
		if n == 0 {
			return
		}
		m.skillIdx = clamp(m.skillIdx+delta, 0, n-1)
	case sectionMcps:
		n := len(m.mcps())
		if n == 0 {
			return
		}
		m.mcpIdx = clamp(m.mcpIdx+delta, 0, n-1)
	}
	m.detailScroll = 0
	m.syncScroll()
}

func (m *model) pageSel(forward bool) {
	step := m.skillViewport
	if m.section == sectionMcps {
		step = m.mcpViewport
	}
	if step < 1 {
		step = 1
	}
	if !forward {
		step = -step
	}
	m.moveSel(step)
}

func (m *model) selectSkill(idx int) {
	n := len(m.skills())
	if n == 0 {
		return
	}
	m.section = sectionSkills
	m.skillIdx = min(idx, n-1)
	m.detailScroll = 0
	m.syncScroll()
}

func (m *model) selectMcp(idx int) {
	n := len(m.mcps())
	if n == 0 {
		return
	}
	m.section = sectionMcps
	m.mcpIdx = min(idx, n-1)
	m.detailScroll = 0
	m.syncScroll()
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---------- actions (port of actions.rs) ----------

func conflictsFromPlan(plan *core.SyncPlan) []conflictItem {
	var out []conflictItem
	for _, a := range plan.Actions {
		switch a.Kind {
		case "conflict_skill":
			out = append(out, conflictItem{key: a.SkillKey})
		case "conflict_mcp":
			out = append(out, conflictItem{isMcp: true, key: a.Server})
		}
	}
	return out
}

func (m *model) beginConfirm(plan *core.SyncPlan, label string) {
	n := len(plan.Actions)
	conflicts := conflictsFromPlan(plan)
	scope := plan.Scope
	m.skillDecisions = map[string]core.ConflictPolicy{}
	m.mcpDecisions = map[string]core.ConflictPolicy{}
	m.lastPlan = plan
	if len(conflicts) == 0 {
		m.status = fmt.Sprintf("%s [%s]: %d action(s). Press y to apply, n to cancel.", label, scope, n)
	} else {
		m.status = fmt.Sprintf("%s [%s]: %d action(s), %d conflict(s). Press y to resolve, n to cancel.", label, scope, n, len(conflicts))
	}
	m.pending = &pending{kind: pendSyncConfirm}
}

func (m *model) dryRunFocused() {
	scope := m.page.scope()
	var plan *core.SyncPlan
	var err error
	if m.section == sectionSkills {
		if len(m.skills()) == 0 {
			m.status = "no skill selected"
			return
		}
		plan, err = core.PlanSyncSkills(scope, m.cwd, nil, m.skills()[m.skillIdx].Key)
	} else {
		if len(m.mcps()) == 0 {
			m.status = "no mcp selected"
			return
		}
		plan, err = core.PlanSyncMcp(scope, m.cwd, nil, m.mcps()[m.mcpIdx].Key)
	}
	if err != nil {
		m.status = fmt.Sprintf("plan error: %v", err)
		return
	}
	m.beginConfirm(plan, "sync focused")
}

func (m *model) dryRunAllSkills() {
	plan, err := core.PlanSyncSkills(m.page.scope(), m.cwd, nil, "")
	if err != nil {
		m.status = fmt.Sprintf("plan error: %v", err)
		return
	}
	m.beginConfirm(plan, "sync all skills")
}

func (m *model) dryRunAllMcp() {
	plan, err := core.PlanSyncMcp(m.page.scope(), m.cwd, nil, "")
	if err != nil {
		m.status = fmt.Sprintf("plan error: %v", err)
		return
	}
	m.beginConfirm(plan, "sync all mcps")
}

func (m *model) dryRunAll() {
	scope := m.page.scope()
	sk, err1 := core.PlanSyncSkills(scope, m.cwd, nil, "")
	mc, err2 := core.PlanSyncMcp(scope, m.cwd, nil, "")
	if err1 != nil {
		m.status = fmt.Sprintf("plan error: %v", err1)
		return
	}
	if err2 != nil {
		m.status = fmt.Sprintf("plan error: %v", err2)
		return
	}
	m.beginConfirm(core.MergePlans(sk, mc), "sync all skills+mcps")
}

func (m *model) onSyncConfirmYes() {
	if m.lastPlan == nil {
		m.pending = nil
		m.status = "nothing to apply"
		return
	}
	conflicts := conflictsFromPlan(m.lastPlan)
	if len(conflicts) == 0 {
		m.applyWithDecisions()
		return
	}
	m.status = conflictPrompt(conflicts[0])
	m.pending = &pending{kind: pendResolveConflict, remaining: conflicts}
}

func conflictPrompt(it conflictItem) string {
	kind := "skill"
	if it.isMcp {
		kind = "mcp"
	}
	return fmt.Sprintf("Conflict %s '%s': [a] keep-source  [b] keep-target  [s] skip  [n] cancel", kind, it.key)
}

func (m *model) onConflictChoice(pol core.ConflictPolicy) {
	if m.pending == nil || m.pending.kind != pendResolveConflict || len(m.pending.remaining) == 0 {
		return
	}
	cur := m.pending.remaining[0]
	if cur.isMcp {
		m.mcpDecisions[cur.key] = pol
	} else {
		m.skillDecisions[cur.key] = pol
	}
	rest := m.pending.remaining[1:]
	if len(rest) == 0 {
		m.applyWithDecisions()
		return
	}
	m.status = conflictPrompt(rest[0])
	m.pending = &pending{kind: pendResolveConflict, remaining: rest}
}

func (m *model) applyWithDecisions() {
	plan := m.lastPlan
	m.lastPlan = nil
	if plan == nil {
		m.pending = nil
		m.status = "nothing to apply"
		return
	}
	scope := core.ScopeUser
	if plan.Scope == "project" {
		scope = core.ScopeProject
	}
	d := core.NewDecisions(core.ConflictSkip)
	d.Skills = m.skillDecisions
	d.Mcps = m.mcpDecisions
	log, err := core.ApplyPlan(plan, m.cwd, scope, d)
	if err != nil {
		m.status = fmt.Sprintf("apply error: %v", err)
	} else {
		m.status = fmt.Sprintf("applied %d step(s) [%s]", len(log), scope)
		m.reload()
	}
	m.pending = nil
	m.skillDecisions = map[string]core.ConflictPolicy{}
	m.mcpDecisions = map[string]core.ConflictPolicy{}
}

func (m *model) cancelPending() {
	m.pending = nil
	m.lastPlan = nil
	m.skillDecisions = map[string]core.ConflictPolicy{}
	m.mcpDecisions = map[string]core.ConflictPolicy{}
	m.status = "cancelled"
}

func (m *model) checkUpdate() {
	m.status = "checking for updates…"
	info, err := core.CheckUpdate()
	if err != nil {
		m.status = fmt.Sprintf("update check failed: %v", err)
		m.pending = nil
		return
	}
	m.updateMsg = info.Message
	if info.UpdateAvailable {
		m.status = fmt.Sprintf("%s  Press y to install, n to cancel.", info.Message)
		m.pending = &pending{kind: pendUpdateInstall}
	} else {
		m.status = info.Message
		m.pending = nil
	}
}

func (m *model) confirmUpdateInstall() {
	m.status = "installing update…"
	info, err := core.InstallUpdate(false)
	if err != nil {
		m.status = fmt.Sprintf("update install failed: %v", err)
	} else {
		m.status = info.Message
		m.updateMsg = info.Message
	}
	m.pending = nil
}

func (m *model) beginInstall() {
	if m.section == sectionSkills {
		m.pending = &pending{kind: pendInstallSkillInput}
		m.status = "Install skill: type local path or git URL, Enter to preview, Esc cancel"
	} else {
		m.pending = &pending{kind: pendInstallMcpName}
		m.status = "Add MCP: type server name, Enter next, Esc cancel"
	}
}

func (m *model) beginDelete() {
	if m.section == sectionSkills {
		if len(m.skills()) == 0 {
			m.status = "no skill selected"
			return
		}
		key := m.skills()[m.skillIdx].Key
		m.status = fmt.Sprintf("Delete skill '%s' [%s]: [y] unlink agents  [p] PURGE canonical  [n] cancel", key, m.page.scope())
		m.pending = &pending{kind: pendDeleteSkillConfirm, key: key}
	} else {
		if len(m.mcps()) == 0 {
			m.status = "no mcp selected"
			return
		}
		key := m.mcps()[m.mcpIdx].Key
		m.status = fmt.Sprintf("Remove MCP '%s' from hub+agents [%s]? y/n", key, m.page.scope())
		m.pending = &pending{kind: pendDeleteMcpConfirm, key: key}
	}
}

func (m *model) installSkillPreview(source string) {
	scope := m.page.scope()
	dir, cleanup, err := core.ResolveSkillSource(source)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		m.status = fmt.Sprintf("resolve error: %v", err)
		m.pending = &pending{kind: pendInstallSkillInput, buffer: source}
		return
	}
	plan, err := core.PlanInstallSkill(scope, m.cwd, dir, nil)
	if err != nil {
		m.status = fmt.Sprintf("plan error: %v", err)
		m.pending = &pending{kind: pendInstallSkillInput, buffer: source}
		return
	}
	key := "skill"
	for _, a := range plan.Actions {
		if a.Kind == "copy_skill" {
			key = a.SkillKey
			break
		}
	}
	m.status = fmt.Sprintf("Install '%s' → canonical + %d link(s) [%s]. y=apply n=cancel", key, len(plan.Actions)-1, scope)
	m.pending = &pending{kind: pendInstallSkillConfirm, source: source, key: key}
}

func (m *model) confirmInstallSkill(source string) {
	scope := m.page.scope()
	_, log, err := core.InstallSkill(scope, m.cwd, source, nil, false)
	if err != nil {
		m.status = fmt.Sprintf("install error: %v", err)
	} else {
		m.status = fmt.Sprintf("installed (%d steps) [%s]", len(log), scope)
		m.reload()
	}
	m.pending = nil
}

func (m *model) confirmInstallMcp(name, transport, endpoint string) {
	scope := m.page.scope()
	var norm *core.McpNormalized
	var err error
	if transport == "stdio" || transport == "local" {
		norm, err = core.McpFromCLI(transport, &endpoint, nil, boolPtr(true))
	} else {
		norm, err = core.McpFromCLI(transport, nil, &endpoint, boolPtr(true))
	}
	if err == nil {
		_, log, e2 := core.AddMcp(scope, m.cwd, name, norm, nil, false)
		if e2 != nil {
			err = e2
		} else {
			m.status = fmt.Sprintf("mcp '%s' added (%d steps) [%s]", name, len(log), scope)
			m.reload()
		}
	}
	if err != nil {
		m.status = fmt.Sprintf("mcp add error: %v", err)
	}
	m.pending = nil
}

func boolPtr(b bool) *bool { return &b }

func (m *model) confirmUnlinkSkill(key string) {
	scope := m.page.scope()
	_, log, err := core.RemoveSkill(scope, m.cwd, key, nil, false, false)
	if err != nil {
		m.status = fmt.Sprintf("unlink error: %v", err)
	} else {
		m.status = fmt.Sprintf("unlinked '%s' (%d steps) [%s]", key, len(log), scope)
		m.reload()
	}
	m.pending = nil
}

func (m *model) confirmPurgeSkill(key string) {
	scope := m.page.scope()
	_, log, err := core.RemoveSkill(scope, m.cwd, key, nil, true, false)
	if err != nil {
		m.status = fmt.Sprintf("purge error: %v", err)
	} else {
		m.status = fmt.Sprintf("PURGED '%s' (%d steps) [%s]", key, len(log), scope)
		m.reload()
	}
	m.pending = nil
}

func (m *model) confirmRemoveMcp(key string) {
	scope := m.page.scope()
	_, log, err := core.RemoveMcp(scope, m.cwd, key, nil, false)
	if err != nil {
		m.status = fmt.Sprintf("mcp remove error: %v", err)
	} else {
		m.status = fmt.Sprintf("removed mcp '%s' (%d steps) [%s]", key, len(log), scope)
		m.reload()
	}
	m.pending = nil
}

// ---------- bubbletea ----------

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.computeLayout()
		return m, nil
	case tea.KeyMsg:
		if m.handleKey(msg) {
			return m, tea.Quit
		}
		return m, nil
	case tea.MouseMsg:
		m.handleMouse(msg)
		return m, nil
	}
	return m, nil
}

func (m *model) computeLayout() {
	h := m.height
	w := m.width
	if h < 5 || w < 10 {
		return
	}
	m.layout.header = rect{0, 0, w, 1}
	m.layout.userTab = rect{7, 0, 6, 1}
	m.layout.projectTab = rect{14, 0, 9, 1}
	body := rect{0, 1, w, h - 4}
	leftW := body.w * 45 / 100
	skillH := body.h * 55 / 100
	m.layout.skills = rect{body.x, body.y, leftW, skillH}
	m.layout.mcps = rect{body.x, body.y + skillH, leftW, body.h - skillH}
	m.layout.detail = rect{body.x + leftW, body.y, body.w - leftW, body.h}
	sv, mv := m.layout.skills.h-2, m.layout.mcps.h-2
	if sv != m.skillViewport || mv != m.mcpViewport {
		m.skillViewport, m.mcpViewport = sv, mv
		m.syncScroll()
	}
}
