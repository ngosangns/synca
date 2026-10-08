package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ngosangns/synca/go/internal/core"
)

// handleKey — port of run.rs::handle_key. Returns true to quit.
func (m *model) handleKey(msg tea.KeyMsg) bool {
	if m.pending != nil {
		m.handlePendingKey(msg)
		return false
	}

	switch msg.String() {
	case "q", "esc", "ctrl+c":
		return true
	case "tab":
		m.page = m.page.toggle()
		m.onContextChange()
		m.status = fmt.Sprintf("page: %s", m.page.label())
	case "[":
		m.section = sectionSkills
		m.onContextChange()
		m.status = "section: " + m.section.label()
	case "]":
		m.section = sectionMcps
		m.onContextChange()
		m.status = "section: " + m.section.label()
	case " ":
		m.section = m.section.toggle()
		m.onContextChange()
		m.status = "section: " + m.section.label()
	case "pgdown":
		m.pageSel(true)
	case "pgup":
		m.pageSel(false)
	case "j", "down":
		m.moveSel(1)
	case "k", "up":
		m.moveSel(-1)
	case "s":
		m.dryRunFocused()
	case "S":
		m.dryRunAllSkills()
	case "M":
		m.dryRunAllMcp()
	case "A":
		m.dryRunAll()
	case "i":
		m.beginInstall()
	case "d":
		m.beginDelete()
	case "u":
		m.checkUpdate()
	case "r":
		m.reload()
		m.status = "reloaded"
	case "?", "h":
		m.help = !m.help
	}
	return false
}

func (m *model) handlePendingKey(msg tea.KeyMsg) {
	p := m.pending
	key := msg.String()

	// Text-input pendings share the edit loop.
	switch p.kind {
	case pendInstallSkillInput, pendInstallMcpName, pendInstallMcpTransport, pendInstallMcpEndpoint:
		m.handleInputKey(msg)
		return
	}

	switch p.kind {
	case pendSyncConfirm:
		switch key {
		case "y", "Y":
			m.onSyncConfirmYes()
		case "n", "N", "esc":
			m.cancelPending()
		default:
			m.status = "confirm sync: press y to continue, n to cancel"
		}
	case pendResolveConflict:
		switch key {
		case "a", "A":
			m.onConflictChoice(core.ConflictKeepSource)
		case "b", "B":
			m.onConflictChoice(core.ConflictKeepTarget)
		case "s", "S":
			m.onConflictChoice(core.ConflictSkip)
		case "n", "N", "esc":
			m.cancelPending()
		default:
			m.status = "conflict: [a] keep-source  [b] keep-target  [s] skip  [n] cancel"
		}
	case pendUpdateInstall:
		switch key {
		case "y", "Y":
			m.confirmUpdateInstall()
		case "n", "N", "esc":
			m.cancelPending()
		default:
			m.status = "install update? y / n"
		}
	case pendInstallSkillConfirm:
		switch key {
		case "y", "Y":
			m.confirmInstallSkill(p.source)
		case "n", "N", "esc":
			m.cancelPending()
		default:
			m.status = "install skill? y / n"
		}
	case pendInstallMcpConfirm:
		switch key {
		case "y", "Y":
			m.confirmInstallMcp(p.name, p.transport, p.endpoint)
		case "n", "N", "esc":
			m.cancelPending()
		default:
			m.status = "add mcp? y / n"
		}
	case pendDeleteSkillConfirm:
		switch key {
		case "y", "Y":
			m.confirmUnlinkSkill(p.key)
		case "p", "P":
			m.status = fmt.Sprintf("PURGE '%s' deletes canonical + all links. Type y again to confirm, n cancel", p.key)
			m.pending = &pending{kind: pendDeleteSkillPurgeConfirm, key: p.key}
		case "n", "N", "esc":
			m.cancelPending()
		default:
			m.status = "delete skill: [y] unlink  [p] purge  [n] cancel"
		}
	case pendDeleteSkillPurgeConfirm:
		switch key {
		case "y", "Y":
			m.confirmPurgeSkill(p.key)
		case "n", "N", "esc":
			m.cancelPending()
		default:
			m.status = "confirm PURGE? y / n"
		}
	case pendDeleteMcpConfirm:
		switch key {
		case "y", "Y":
			m.confirmRemoveMcp(p.key)
		case "n", "N", "esc":
			m.cancelPending()
		default:
			m.status = "remove mcp? y / n"
		}
	}
}

// handleInputKey mirrors the char-by-char buffer editing in run.rs.
func (m *model) handleInputKey(msg tea.KeyMsg) {
	p := m.pending
	buf := p.buffer
	set := func(b string) { m.pending.buffer = b }

	switch msg.Type {
	case tea.KeyEsc:
		m.cancelPending()
		return
	case tea.KeyBackspace, tea.KeyDelete:
		if len(buf) > 0 {
			r := []rune(buf)
			buf = string(r[:len(r)-1])
		}
		m.setInputStatus(buf)
		set(buf)
		return
	case tea.KeyEnter:
		m.onInputEnter()
		return
	}

	if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
		buf += string(msg.Runes)
		if msg.Type == tea.KeySpace {
			buf += " "
		}
		m.setInputStatus(buf)
		set(buf)
	}
}

func (m *model) setInputStatus(buf string) {
	p := m.pending
	switch p.kind {
	case pendInstallSkillInput:
		m.status = fmt.Sprintf("Install skill path/URL: %s_", buf)
	case pendInstallMcpName:
		m.status = fmt.Sprintf("MCP name: %s_", buf)
	case pendInstallMcpTransport:
		m.status = fmt.Sprintf("MCP '%s' transport: %s_", p.name, buf)
	case pendInstallMcpEndpoint:
		m.status = fmt.Sprintf("MCP '%s' endpoint: %s_", p.name, buf)
	}
}

func (m *model) onInputEnter() {
	p := m.pending
	buf := strings.TrimSpace(p.buffer)
	switch p.kind {
	case pendInstallSkillInput:
		if buf == "" {
			m.status = "enter a path or git URL"
			return
		}
		m.installSkillPreview(buf)
	case pendInstallMcpName:
		if buf == "" {
			m.status = "enter MCP name"
			return
		}
		m.pending = &pending{kind: pendInstallMcpTransport, name: buf, buffer: "stdio"}
		m.status = fmt.Sprintf("MCP '%s' transport [stdio/sse/http] (default stdio): stdio_", buf)
	case pendInstallMcpTransport:
		transport := buf
		if transport == "" {
			transport = "stdio"
		} else {
			transport = strings.ToLower(transport)
		}
		hint := "url"
		if transport == "stdio" || transport == "local" {
			hint = "command"
		}
		m.pending = &pending{kind: pendInstallMcpEndpoint, name: p.name, transport: transport}
		m.status = fmt.Sprintf("MCP '%s' (%s) %s: _", p.name, transport, hint)
	case pendInstallMcpEndpoint:
		if buf == "" {
			m.status = "endpoint required"
			return
		}
		m.status = fmt.Sprintf("Add MCP '%s' (%s: %s) [%s]? y/n", p.name, p.transport, buf, m.page.scope())
		m.pending = &pending{kind: pendInstallMcpConfirm, name: p.name, transport: p.transport, endpoint: buf}
	}
}

// handleMouse — port of run.rs::handle_mouse.
func (m *model) handleMouse(msg tea.MouseMsg) {
	if m.pending != nil {
		return
	}
	col, row := msg.X, msg.Y
	l := m.layout

	if m.help {
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			m.help = false
		}
		return
	}

	switch {
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
		switch {
		case l.userTab.contains(col, row):
			if m.page != pageUser {
				m.page = pageUser
				m.onContextChange()
				m.status = "page: User"
			}
		case l.projectTab.contains(col, row):
			if m.page != pageProject {
				m.page = pageProject
				m.onContextChange()
				m.status = "page: Project"
			}
		case l.skills.contains(col, row):
			m.section = sectionSkills
			if idx, ok := listRowAt(l.skills, row, m.skillScroll, len(m.skills())); ok {
				m.selectSkill(idx)
				m.status = "skill: " + m.skills()[idx].DisplayName
			} else {
				m.onContextChange()
				m.status = "section: Skills"
			}
		case l.mcps.contains(col, row):
			m.section = sectionMcps
			if idx, ok := listRowAt(l.mcps, row, m.mcpScroll, len(m.mcps())); ok {
				m.selectMcp(idx)
				m.status = "mcp: " + m.mcps()[idx].Key
			} else {
				m.onContextChange()
				m.status = "section: MCPs"
			}
		}
	case msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown:
		down := msg.Button == tea.MouseButtonWheelDown
		switch {
		case l.detail.contains(col, row):
			if down {
				m.detailScroll += 3
			} else {
				m.detailScroll = max(0, m.detailScroll-3)
			}
		case l.skills.contains(col, row):
			if m.section != sectionSkills {
				m.section = sectionSkills
				m.onContextChange()
			}
			if down {
				m.moveSel(1)
			} else {
				m.moveSel(-1)
			}
		case l.mcps.contains(col, row):
			if m.section != sectionMcps {
				m.section = sectionMcps
				m.onContextChange()
			}
			if down {
				m.moveSel(1)
			} else {
				m.moveSel(-1)
			}
		}
	}
}

// listRowAt maps a mouse row inside a bordered list pane to a list index.
func listRowAt(r rect, mouseRow, offset, length int) (int, bool) {
	if length == 0 || r.h < 3 {
		return 0, false
	}
	innerTop := r.y + 1
	innerH := r.h - 2
	if mouseRow < innerTop || mouseRow >= innerTop+innerH {
		return 0, false
	}
	idx := offset + (mouseRow - innerTop)
	return idx, idx < length
}
