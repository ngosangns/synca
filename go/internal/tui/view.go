package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"
	"github.com/ngosangns/synca/go/internal/core"
)

// ---------- styles (ratatui color equivalents) ----------

var (
	stYellow   = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	stGray     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	stDarkGray = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	stCyan     = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	stCyanB    = stCyan
	stMagenta  = lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true)
	stRed      = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	stGreen    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	stGreenB   = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	stWhite    = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	stBold     = lipgloss.NewStyle().Bold(true)
	stAdd      = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	stDel      = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	stMeta     = stDarkGray
	stTabOn    = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("6")).Bold(true)
	stTabOff   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	stSel      = lipgloss.NewStyle().Background(lipgloss.Color("4")).Foreground(lipgloss.Color("15")).Bold(true)
	stBorderOn = lipgloss.Color("6")
	stBorderOf = lipgloss.Color("8")
	stChip     = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("8"))
	stPending  = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("3"))
	stStatusP  = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("3"))
	stStatus   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

func (m model) View() string {
	if m.width == 0 {
		return "loading…"
	}
	header := m.renderHeader()
	body := m.renderBody()
	footer := m.renderFooter()
	screen := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)

	lines := strings.Split(screen, "\n")
	if m.pending != nil && m.pending.kind == pendResolveConflict {
		m.overlay(&lines, m.conflictOverlay())
	}
	if m.help {
		m.overlay(&lines, helpOverlay())
	}
	return strings.Join(lines, "\n")
}

// overlay draws a centered box over screen lines.
func (m model) overlay(lines *[]string, content []string) {
	if len(*lines) == 0 {
		return
	}
	w := m.width * 2 / 3
	if w < 40 {
		w = 40
	}
	if w > 90 {
		w = 90
	}
	h := len(content) + 2
	if h > m.height-2 {
		h = m.height - 2
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("3")).
		Background(lipgloss.Color("0")).
		Width(w - 2).
		Render(strings.Join(content, "\n"))
	bl := strings.Split(box, "\n")
	y := (m.height - len(bl)) / 2
	x := (m.width - w) / 2
	for i, bline := range bl {
		row := y + i
		if row < 0 || row >= len(*lines) {
			continue
		}
		existing := stripAnsi((*lines)[row])
		left := clipRunes(existing, x)
		(*lines)[row] = left + bline
	}
}

func stripAnsi(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if r == 'm' || r == 'K' || r == 'H' || r == 'J' {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s + strings.Repeat(" ", n-len(r))
	}
	return string(r[:n])
}

// ---------- header/footer ----------

func (m model) renderHeader() string {
	userSt, projSt := stTabOff, stTabOff
	if m.page == pageUser {
		userSt = stTabOn
	} else {
		projSt = stTabOn
	}
	root := "(not a git repo)"
	if m.projectInv.ProjectRoot != nil {
		root = *m.projectInv.ProjectRoot
	}
	line := stYellow.Render(" synca ") +
		userSt.Render(" User ") + " " +
		projSt.Render(" Project ") +
		fmt.Sprintf("  ·  cwd project: %s", root)
	return clipRunes(stripAnsiPad(line, m.width), m.width)
}

// stripAnsiPad pads a styled line to width w (keeping ANSI codes).
func stripAnsiPad(s string, w int) string {
	vis := lipgloss.Width(s)
	if vis < w {
		return s + strings.Repeat(" ", w-vis)
	}
	return s
}

func chip(label string) string  { return stChip.Render(label) }
func pchip(label string) string { return stPending.Render(label) }

func (m model) navHotkeys() string {
	return chip(" Tab ") + "pages " +
		chip(" [/]/Space ") + "sections " +
		chip(" j/k ") + "nav " +
		chip(" PgUp/Dn ") + "page " +
		chip(" click/wheel ") + "mouse " +
		chip(" u ") + "update " +
		chip(" r ") + "reload " +
		chip(" ? ") + "help " +
		chip(" q ") + "quit"
}

func (m model) syncHotkeys() string {
	s := chip(" s ") + "focused " +
		chip(" S ") + "skills-all " +
		chip(" M ") + "mcp-all " +
		chip(" A ") + "all " +
		chip(" i ") + "install " +
		chip(" d ") + "delete " +
		"(this page) "
	if m.pending == nil {
		return s
	}
	s += " · "
	switch m.pending.kind {
	case pendSyncConfirm, pendUpdateInstall, pendInstallSkillConfirm, pendInstallMcpConfirm,
		pendDeleteMcpConfirm, pendDeleteSkillPurgeConfirm:
		s += pchip(" y ") + "confirm " + pchip(" n ") + "cancel"
	case pendDeleteSkillConfirm:
		s += pchip(" y ") + "unlink " + pchip(" p ") + "purge " + pchip(" n ") + "cancel"
	case pendResolveConflict:
		s += pchip(" a ") + "keep-src " + pchip(" b ") + "keep-tgt " + pchip(" s ") + "skip"
	case pendInstallSkillInput, pendInstallMcpName, pendInstallMcpTransport, pendInstallMcpEndpoint:
		s += pchip(" Enter ") + "next " + pchip(" Esc ") + "cancel"
	}
	return s
}

func (m model) renderFooter() string {
	status := m.status
	if status == "" {
		status = " "
	}
	var statusLine string
	if m.pending != nil {
		statusLine = stStatusP.Render(clipRunes(status, m.width))
	} else {
		statusLine = stStatus.Render(clipRunes(status, m.width))
	}
	return statusLine + "\n" + clipRunes(stripAnsiPad(m.navHotkeys(), m.width), m.width) + "\n" + stripAnsiPad(m.syncHotkeys(), m.width)
}

// ---------- body ----------

func (m model) renderBody() string {
	skills := m.renderList(m.layout.skills, len(m.skills()), m.skillIdx, m.skillScroll, m.section == sectionSkills, true)
	mcps := m.renderList(m.layout.mcps, len(m.mcps()), m.mcpIdx, m.mcpScroll, m.section == sectionMcps, false)
	left := lipgloss.JoinVertical(lipgloss.Left, skills, mcps)
	detail := m.renderDetail()
	return lipgloss.JoinHorizontal(lipgloss.Top, left, detail)
}

// bordered draws a box with a title spliced into the top border.
// Content lines are already ANSI-styled; visible width is truncated/padded to w-2.
func bordered(title string, rows []string, w, h int, color lipgloss.Color) string {
	if w < 4 || h < 2 {
		return ""
	}
	inner := w - 2
	bc := lipgloss.NewStyle().Foreground(color)
	// top border with title
	t := []rune(title)
	top := []rune("┌" + strings.Repeat("─", inner) + "┐")
	for i := 0; i < len(t) && i+1 < len(top)-1; i++ {
		top[i+1] = t[i]
	}
	var out []string
	out = append(out, bc.Render(string(top)))
	for i := 0; i < h-2; i++ {
		row := ""
		if i < len(rows) {
			row = rows[i]
		}
		out = append(out, bc.Render("│")+truncateVisible(row, inner)+bc.Render("│"))
	}
	out = append(out, bc.Render("└"+strings.Repeat("─", inner)+"┘"))
	return strings.Join(out, "\n")
}

// truncateVisible clamps a styled string to `w` visible cells (ANSI-aware).
func truncateVisible(s string, w int) string {
	if lipgloss.Width(s) > w {
		s = truncate.StringWithTail(s, uint(w), "")
	}
	return padVisible(s, w)
}

// renderList renders a bordered list pane; isSkills picks the row renderer.
func (m model) renderList(r rect, length, idx, scroll int, focused bool, isSkills bool) string {
	if r.h < 3 || r.w < 4 {
		return strings.Repeat("\n", max(0, r.h-1))
	}
	viewport := max(r.h-2, 1)
	end := min(scroll+viewport, length)
	inner := r.w - 2
	var rows []string
	for i := scroll; i < end; i++ {
		var line string
		if isSkills {
			line = m.skillRow(m.skills()[i], inner)
		} else {
			line = m.mcpRow(m.mcps()[i], inner)
		}
		if focused && i == idx {
			line = stSel.Render(padVisible("›"+truncateVisible(line, inner-1), inner))
		} else {
			line = padVisible(line, inner)
		}
		rows = append(rows, line)
	}
	pos := "0/0"
	if length > 0 {
		pos = fmt.Sprintf("%d/%d", idx+1, length)
	}
	above, below := " ", " "
	if scroll > 0 {
		above = "↑"
	}
	if end < length {
		below = "↓"
	}
	label := "Skills"
	if !isSkills {
		label = "MCPs"
	}
	title := fmt.Sprintf(" %s · %s %s%s ", label, pos, above, below)
	if focused {
		title = fmt.Sprintf(" %s * · %s %s%s ", label, pos, above, below)
	}
	borderColor := stBorderOf
	if focused {
		borderColor = stBorderOn
	}
	return bordered(title, rows, r.w, r.h, borderColor)
}

func padVisible(s string, w int) string {
	vis := lipgloss.Width(s)
	if vis < w {
		return s + strings.Repeat(" ", w-vis)
	}
	return s
}

func (m model) skillRow(s core.SkillEntry, w int) string {
	var agents []string
	for _, p := range s.Presence {
		agents = append(agents, p.Agent)
	}
	mark := " "
	if s.Mismatch {
		mark = stRed.Render("!")
	}
	line := mark + " " + stCyan.Render(s.DisplayName) +
		stDarkGray.Render(fmt.Sprintf("  (%d) [%s]", len(s.Presence), strings.Join(agents, ",")))
	return line
}

func (m model) mcpRow(e core.McpEntry, w int) string {
	var agents []string
	for _, p := range e.Presence {
		agents = append(agents, p.Agent)
	}
	mark := " "
	if e.Mismatch {
		mark = stRed.Render("!")
	}
	return mark + " " + stMagenta.Render(e.Key) +
		stDarkGray.Render(fmt.Sprintf("  (%d) [%s]", len(e.Presence), strings.Join(agents, ",")))
}

// ---------- detail ----------

func (m model) renderDetail() string {
	r := m.layout.detail
	if r.h < 3 || r.w < 4 {
		return ""
	}
	lines := m.detailLines()
	viewport := r.h - 2
	off := min(m.detailScroll, max(0, len(lines)-viewport))
	vis := lines[off:min(len(lines), off+viewport)]
	inner := r.w - 2
	var rows []string
	for _, ln := range vis {
		rows = append(rows, truncateVisible(ln, inner))
	}
	return bordered(" Detail ", rows, r.w, r.h, stBorderOf)
}

func (m model) detailLines() []string {
	var lines []string
	if m.section == sectionSkills {
		if len(m.skills()) > 0 && m.skillIdx < len(m.skills()) {
			s := m.skills()[m.skillIdx]
			lines = append(lines, stYellow.Render("Skill: ")+stCyan.Render(s.DisplayName))
			lines = append(lines, stBold.Render("Description:"))
			if s.Description != nil && strings.TrimSpace(*s.Description) != "" {
				lines = append(lines, stWhite.Render(strings.TrimSpace(*s.Description)))
			} else {
				lines = append(lines, stDarkGray.Render("(no description in SKILL.md frontmatter)"))
			}
			lines = append(lines, "key: "+s.Key)
			mism := stGreen.Render("no")
			if s.Mismatch {
				mism = stRed.Render("YES")
			}
			lines = append(lines, "mismatch: "+mism)
			lines = append(lines, skillMismatchLines(s)...)
			lines = append(lines, "")
			lines = append(lines, stBold.Render("Agents:"))
			for _, p := range s.Presence {
				link := ""
				if p.IsSymlink && p.SymlinkTarget != nil {
					link = " → " + *p.SymlinkTarget
				}
				lines = append(lines, "  · "+stCyan.Render(p.Agent)+"  "+p.Path+link)
				lines = append(lines, "      hash: "+shortHash(p.ContentHash))
			}
		} else {
			lines = append(lines, "No skills in this scope.")
			if m.page == pageProject && !m.projectOK {
				lines = append(lines, "Current directory is not inside a git repository.")
			}
		}
	} else {
		if len(m.mcps()) > 0 && m.mcpIdx < len(m.mcps()) {
			e := m.mcps()[m.mcpIdx]
			lines = append(lines, stYellow.Render("MCP: ")+stMagenta.Render(e.Key))
			if p := preferredMcpPresence(e.Presence); p != nil && p.Normalized != nil {
				n := p.Normalized
				lines = append(lines, stBold.Render("Summary:"))
				lines = append(lines, "  transport: "+n.Transport)
				if n.Command != nil {
					parts := append(append([]string{}, n.Command...), n.Args...)
					lines = append(lines, "  command: "+strings.Join(parts, " "))
				}
				if n.URL != nil {
					lines = append(lines, "  url: "+*n.URL)
				}
				if n.Enabled != nil {
					lines = append(lines, fmt.Sprintf("  enabled: %t", *n.Enabled))
				}
			}
			mism := stGreen.Render("no")
			if e.Mismatch {
				mism = stRed.Render("YES")
			}
			lines = append(lines, "mismatch: "+mism)
			lines = append(lines, mcpMismatchLines(e)...)
			lines = append(lines, "")
			lines = append(lines, stBold.Render("Agents:"))
			for _, p := range e.Presence {
				lines = append(lines, "  · "+stMagenta.Render(p.Agent)+"  "+p.Path)
				if p.Normalized != nil {
					lines = append(lines, fmt.Sprintf("      transport=%s  fp=%s", p.Normalized.Transport, shortHash(p.Fingerprint)))
					if p.Normalized.Command != nil {
						lines = append(lines, "      command: "+strings.Join(p.Normalized.Command, " "))
					}
					if p.Normalized.URL != nil {
						lines = append(lines, "      url: "+*p.Normalized.URL)
					}
					if len(p.Normalized.EnvKeys) > 0 {
						lines = append(lines, "      env keys: "+strings.Join(p.Normalized.EnvKeys, ", "))
					}
				}
			}
		} else {
			lines = append(lines, "No MCP servers in this scope.")
		}
	}

	if m.lastPlan != nil {
		lines = append(lines, "")
		lines = append(lines, stGreenB.Render(fmt.Sprintf("Plan (%d actions):", len(m.lastPlan.Actions))))
		n := min(12, len(m.lastPlan.Actions))
		for i := 0; i < n; i++ {
			b, _ := json.Marshal(m.lastPlan.Actions[i])
			s := string(b)
			if len(s) > 90 {
				s = s[:90] + "…"
			}
			lines = append(lines, fmt.Sprintf("  %d. %s", i+1, s))
		}
		if len(m.lastPlan.Actions) > 12 {
			lines = append(lines, fmt.Sprintf("  … +%d more", len(m.lastPlan.Actions)-12))
		}
	}
	return lines
}

// ---------- diffview port ----------

func shortHash(h string) string {
	r := []rune(h)
	if len(r) > 12 {
		return string(r[:12])
	}
	return h
}

func preferredMcpPresence(p []core.McpPresence) *core.McpPresence {
	for i := range p {
		if p[i].Agent == "agents" {
			return &p[i]
		}
	}
	if len(p) > 0 {
		return &p[0]
	}
	return nil
}

func pickPresenceT(p []core.SkillPresence) *core.SkillPresence {
	for i := range p {
		if p[i].Agent == "agents" {
			return &p[i]
		}
	}
	if len(p) > 0 {
		return &p[0]
	}
	return nil
}

func skillTarget(e *core.SkillEntry) *core.SkillPresence {
	src := pickPresenceT(e.Presence)
	if src == nil {
		return nil
	}
	for i := range e.Presence {
		if e.Presence[i].ContentHash != src.ContentHash {
			return &e.Presence[i]
		}
	}
	for i := range e.Presence {
		if e.Presence[i].Agent != "agents" {
			return &e.Presence[i]
		}
	}
	return nil
}

func mcpTarget(e *core.McpEntry) *core.McpPresence {
	src := pickPresenceM(e.Presence)
	if src == nil {
		return nil
	}
	for i := range e.Presence {
		if e.Presence[i].Fingerprint != src.Fingerprint {
			return &e.Presence[i]
		}
	}
	for i := range e.Presence {
		if e.Presence[i].Agent != "agents" {
			return &e.Presence[i]
		}
	}
	return nil
}

func pickPresenceM(p []core.McpPresence) *core.McpPresence { return preferredMcpPresence(p) }

var (
	stTitle = stRed
	stLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
	stName  = stCyan
)

func skillMismatchLines(e core.SkillEntry) []string {
	if !e.Mismatch {
		return nil
	}
	var lines []string
	lines = append(lines, "")
	lines = append(lines, stTitle.Render("⚠ CONTENT MISMATCH — choose keep-source (a) / keep-target (b) when syncing"))

	byHash := map[string][]*core.SkillPresence{}
	var hashOrder []string
	for i := range e.Presence {
		p := &e.Presence[i]
		if _, ok := byHash[p.ContentHash]; !ok {
			hashOrder = append(hashOrder, p.ContentHash)
		}
		byHash[p.ContentHash] = append(byHash[p.ContentHash], p)
	}
	sort.Strings(hashOrder)

	src := pickPresenceT(e.Presence)
	tgt := skillTarget(&e)
	if src != nil {
		lines = append(lines, stLabel.Render("SOURCE (a / keep-source)")+"  "+stName.Render(src.Agent)+stMeta.Render("  hash="+shortHash(src.ContentHash)))
		lines = append(lines, "  path: "+stWhite.Render(src.Path))
		if src.IsSymlink && src.SymlinkTarget != nil {
			lines = append(lines, "  link→ "+stMeta.Render(*src.SymlinkTarget))
		}
	}
	if tgt != nil {
		lines = append(lines, stLabel.Render("TARGET (b / keep-target)")+"  "+stName.Render(tgt.Agent)+stMeta.Render("  hash="+shortHash(tgt.ContentHash)))
		lines = append(lines, "  path: "+stWhite.Render(tgt.Path))
		if tgt.IsSymlink && tgt.SymlinkTarget != nil {
			lines = append(lines, "  link→ "+stMeta.Render(*tgt.SymlinkTarget))
		}
	}

	lines = append(lines, stBold.Render("Variants by hash:"))
	for _, hash := range hashOrder {
		group := byHash[hash]
		var agents []string
		for _, p := range group {
			agents = append(agents, p.Agent)
		}
		mark := ""
		if src != nil && src.ContentHash == hash {
			mark = " [source]"
		} else if tgt != nil && tgt.ContentHash == hash {
			mark = " [target]"
		}
		lines = append(lines, stMagenta.Render("  · "+shortHash(hash))+stMeta.Render(mark)+"  agents=["+strings.Join(agents, ", ")+"]")
		for i, p := range group {
			if i >= 4 {
				break
			}
			lines = append(lines, "      "+stName.Render(p.Agent)+"  "+stWhite.Render(p.Path))
		}
		if len(group) > 4 {
			lines = append(lines, fmt.Sprintf("      … +%d more", len(group)-4))
		}
	}

	if src != nil && tgt != nil {
		lines = append(lines, skillMdDiffLines(src.Path, tgt.Path)...)
	}
	return lines
}

func resolveSkillMd(dir string) string {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		real = dir
	}
	md := filepath.Join(real, "SKILL.md")
	if st, err := os.Stat(md); err == nil && !st.IsDir() {
		return md
	}
	return ""
}

func skillMdDiffLines(srcDir, tgtDir string) []string {
	srcMd, tgtMd := resolveSkillMd(srcDir), resolveSkillMd(tgtDir)
	if srcMd == "" || tgtMd == "" {
		return []string{stMeta.Render("(SKILL.md missing on one side — cannot show text diff)")}
	}
	sa, _ := os.ReadFile(srcMd)
	tb, _ := os.ReadFile(tgtMd)
	if string(sa) == string(tb) {
		return []string{stMeta.Render("SKILL.md text identical (hash differs from other files in tree)")}
	}
	lines := []string{"", stBold.Render("SKILL.md unified diff (source − / target +):")}
	lines = append(lines, stDel.Render("--- "+srcMd), stAdd.Render("+++ "+tgtMd))
	lines = append(lines, unifiedDiffHunks(string(sa), string(tb), 40)...)
	return lines
}

func mcpMismatchLines(e core.McpEntry) []string {
	if !e.Mismatch {
		return nil
	}
	var lines []string
	lines = append(lines, "")
	lines = append(lines, stTitle.Render("⚠ CONFIG MISMATCH — choose keep-source (a) / keep-target (b) when syncing"))

	byFp := map[string][]*core.McpPresence{}
	var fpOrder []string
	for i := range e.Presence {
		p := &e.Presence[i]
		if _, ok := byFp[p.Fingerprint]; !ok {
			fpOrder = append(fpOrder, p.Fingerprint)
		}
		byFp[p.Fingerprint] = append(byFp[p.Fingerprint], p)
	}
	sort.Strings(fpOrder)

	src := preferredMcpPresence(e.Presence)
	tgt := mcpTarget(&e)
	if src != nil {
		lines = append(lines, stLabel.Render("SOURCE (a / keep-source)")+"  "+stName.Render(src.Agent)+stMeta.Render("  fp="+shortHash(src.Fingerprint)))
		lines = append(lines, "  path: "+stWhite.Render(src.Path))
		lines = append(lines, mcpNormSummary(src.Normalized, "  ")...)
	}
	if tgt != nil {
		lines = append(lines, stLabel.Render("TARGET (b / keep-target)")+"  "+stName.Render(tgt.Agent)+stMeta.Render("  fp="+shortHash(tgt.Fingerprint)))
		lines = append(lines, "  path: "+stWhite.Render(tgt.Path))
		lines = append(lines, mcpNormSummary(tgt.Normalized, "  ")...)
	}

	lines = append(lines, stBold.Render("Variants by fingerprint:"))
	for _, fp := range fpOrder {
		group := byFp[fp]
		var agents []string
		for _, p := range group {
			agents = append(agents, p.Agent)
		}
		mark := ""
		if src != nil && src.Fingerprint == fp {
			mark = " [source]"
		} else if tgt != nil && tgt.Fingerprint == fp {
			mark = " [target]"
		}
		lines = append(lines, stMagenta.Render("  · "+shortHash(fp))+stMeta.Render(mark)+"  agents=["+strings.Join(agents, ", ")+"]")
		for i, p := range group {
			if i >= 4 {
				break
			}
			lines = append(lines, "      "+stName.Render(p.Agent)+"  "+stWhite.Render(p.Path))
		}
	}
	if src != nil && tgt != nil {
		lines = append(lines, mcpFieldDiffLines(src.Normalized, tgt.Normalized)...)
	}
	return lines
}

func mcpNormSummary(n *core.McpNormalized, indent string) []string {
	if n == nil {
		return nil
	}
	en := "-"
	if n.Enabled != nil {
		en = fmt.Sprintf("%t", *n.Enabled)
	}
	lines := []string{fmt.Sprintf("%stransport=%s  enabled=%s", indent, n.Transport, en)}
	if n.Command != nil {
		parts := append(append([]string{}, n.Command...), n.Args...)
		lines = append(lines, fmt.Sprintf("%scommand: %s", indent, strings.Join(parts, " ")))
	}
	if n.URL != nil {
		lines = append(lines, fmt.Sprintf("%surl: %s", indent, *n.URL))
	}
	if len(n.EnvKeys) > 0 {
		lines = append(lines, fmt.Sprintf("%senv keys: %s", indent, strings.Join(n.EnvKeys, ", ")))
	}
	return lines
}

func flattenCmd(n *core.McpNormalized) *string {
	if n == nil || n.Command == nil {
		return nil
	}
	parts := append(append([]string{}, n.Command...), n.Args...)
	s := strings.Join(parts, " ")
	return &s
}

func fieldChangeLines(field string, src, tgt *string) []string {
	s, t := "(none)", "(none)"
	if src != nil {
		s = *src
	}
	if tgt != nil {
		t = *tgt
	}
	if s == t {
		return []string{stMeta.Render(fmt.Sprintf("  %s: same", field)) + fmt.Sprintf(" (%s)", s)}
	}
	return []string{
		stBold.Render(fmt.Sprintf("  %s:", field)),
		stDel.Render(fmt.Sprintf("    − %s", s)),
		stAdd.Render(fmt.Sprintf("    + %s", t)),
	}
}

func mcpFieldDiffLines(src, tgt *core.McpNormalized) []string {
	lines := []string{"", stBold.Render("Field diff (source → target):")}
	var sT, tT *string
	if src != nil {
		sT = &src.Transport
	}
	if tgt != nil {
		tT = &tgt.Transport
	}
	lines = append(lines, fieldChangeLines("transport", sT, tT)...)
	lines = append(lines, fieldChangeLines("command", flattenCmd(src), flattenCmd(tgt))...)
	var sU, tU *string
	if src != nil {
		sU = src.URL
	}
	if tgt != nil {
		tU = tgt.URL
	}
	lines = append(lines, fieldChangeLines("url", sU, tU)...)
	var sE, tE *string
	if src != nil && src.Enabled != nil {
		v := fmt.Sprintf("%t", *src.Enabled)
		sE = &v
	}
	if tgt != nil && tgt.Enabled != nil {
		v := fmt.Sprintf("%t", *tgt.Enabled)
		tE = &v
	}
	lines = append(lines, fieldChangeLines("enabled", sE, tE)...)
	var sK, tK *string
	if src != nil && len(src.EnvKeys) > 0 {
		v := strings.Join(src.EnvKeys, ", ")
		sK = &v
	}
	if tgt != nil && len(tgt.EnvKeys) > 0 {
		v := strings.Join(tgt.EnvKeys, ", ")
		tK = &v
	}
	lines = append(lines, fieldChangeLines("env_keys", sK, tK)...)
	return lines
}

// unifiedDiffHunks — same algorithm as diffview.rs.
func unifiedDiffHunks(a, b string, maxLines int) []string {
	aLines := strings.Split(strings.TrimRight(a, "\n"), "\n")
	bLines := strings.Split(strings.TrimRight(b, "\n"), "\n")
	var out []string
	i, j := 0, 0
	for (i < len(aLines) || j < len(bLines)) && len(out) < maxLines {
		if i < len(aLines) && j < len(bLines) && aLines[i] == bLines[j] {
			run := 0
			for i+run < len(aLines) && j+run < len(bLines) && aLines[i+run] == bLines[j+run] {
				run++
			}
			if run > 0 && len(out) == 0 {
				// leading equals — skip
			} else if run > 3 {
				out = append(out, stMeta.Render(fmt.Sprintf("  … %d identical lines …", run)))
			} else {
				for k := 0; k < run && len(out) < maxLines; k++ {
					out = append(out, stMeta.Render("  "+aLines[i+k]))
				}
			}
			i += run
			j += run
			continue
		}
		if i < len(aLines) && (j >= len(bLines) || !containsIn(bLines[j:min(len(bLines), j+8)], aLines[i])) {
			out = append(out, stDel.Render("− "+aLines[i]))
			i++
			continue
		}
		if j < len(bLines) && (i >= len(aLines) || !containsIn(aLines[i:min(len(aLines), i+8)], bLines[j])) {
			out = append(out, stAdd.Render("+ "+bLines[j]))
			j++
			continue
		}
		if i < len(aLines) {
			out = append(out, stDel.Render("− "+aLines[i]))
			i++
		}
		if j < len(bLines) && len(out) < maxLines {
			out = append(out, stAdd.Render("+ "+bLines[j]))
			j++
		}
	}
	if i < len(aLines) || j < len(bLines) {
		out = append(out, stMeta.Render(fmt.Sprintf("  … truncated (%d src / %d tgt lines left)", len(aLines)-i, len(bLines)-j)))
	}
	return out
}

func containsIn(lines []string, s string) bool {
	for _, l := range lines {
		if l == s {
			return true
		}
	}
	return false
}

// ---------- overlays ----------

func (m model) conflictOverlay() []string {
	if m.pending == nil || len(m.pending.remaining) == 0 {
		return nil
	}
	it := m.pending.remaining[0]
	kind := "skill"
	if it.isMcp {
		kind = "mcp"
	}
	var lines []string
	lines = append(lines, stPending.Render(fmt.Sprintf(" Conflict · %s ", kind)))
	lines = append(lines, "Name: "+stCyan.Render(it.key))
	lines = append(lines, stYellow.Render("[a] keep-source   [b] keep-target   [s] skip   [n] cancel"))
	lines = append(lines, "")

	if !it.isMcp {
		var e *core.SkillEntry
		for i := range m.skills() {
			if m.skills()[i].Key == it.key {
				e = &m.skills()[i]
				break
			}
		}
		if e == nil {
			for i := range m.userInv.Skills {
				if m.userInv.Skills[i].Key == it.key {
					e = &m.userInv.Skills[i]
					break
				}
			}
		}
		if e == nil {
			for i := range m.projectInv.Skills {
				if m.projectInv.Skills[i].Key == it.key {
					e = &m.projectInv.Skills[i]
					break
				}
			}
		}
		if e != nil {
			lines = append(lines, stYellow.Render("Skill: ")+stCyan.Render(e.DisplayName))
			lines = append(lines, skillMismatchLines(*e)...)
		} else {
			lines = append(lines, fmt.Sprintf("(skill '%s' not in current inventory)", it.key))
		}
	} else {
		var e *core.McpEntry
		for i := range m.mcps() {
			if m.mcps()[i].Key == it.key {
				e = &m.mcps()[i]
				break
			}
		}
		if e == nil {
			for i := range m.userInv.Mcps {
				if m.userInv.Mcps[i].Key == it.key {
					e = &m.userInv.Mcps[i]
					break
				}
			}
		}
		if e == nil {
			for i := range m.projectInv.Mcps {
				if m.projectInv.Mcps[i].Key == it.key {
					e = &m.projectInv.Mcps[i]
					break
				}
			}
		}
		if e != nil {
			lines = append(lines, stYellow.Render("MCP: ")+stMagenta.Render(e.Key))
			lines = append(lines, mcpMismatchLines(*e)...)
		} else {
			lines = append(lines, fmt.Sprintf("(mcp '%s' not in current inventory)", it.key))
		}
	}

	left := len(m.pending.remaining) - 1
	if left > 0 {
		lines = append(lines, "")
		lines = append(lines, stDarkGray.Render(fmt.Sprintf("… %d more conflict(s) after this", left)))
	}
	return lines
}

func helpOverlay() []string {
	return []string{
		"Keys / Mouse",
		"  Tab / click tabs   User ↔ Project",
		"  [ / ] / click list Skills / MCPs section",
		"  j / k / click row  move (stops at ends)",
		"  wheel on list      move (stops at ends)",
		"  PgUp/PgDn          page list / scroll detail",
		"  s                  sync focused item (current page) → y/n",
		"  S                  sync ALL skills (current page) → y/n",
		"  M                  sync ALL MCPs (current page) → y/n",
		"  A                  sync ALL skills + MCPs (current page) → y/n",
		"  i                  install skill (path/git) or add MCP",
		"  d                  delete: skill unlink (y) / purge (p×2); MCP remove",
		"  u                  check/install update from GitHub",
		"  r                  reload inventory",
		"  ?                  toggle help",
		"  q                  quit",
		"",
		"Conflicts: a=keep-source b=keep-target s=skip",
		"Install/delete are scope-locked to the current User|Project page.",
	}
}
