package tui

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

// Run launches the interactive Bubble Tea TUI (port of synca-tui/run.rs).
func Run(cwd string) error {
	if r, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = r
	}
	p := tea.NewProgram(newModel(cwd), tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}
