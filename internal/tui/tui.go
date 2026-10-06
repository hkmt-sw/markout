// Package tui implements the interactive terminal UI for markout.
//
// The UI is a single full-screen panel inspired by Midnight Commander: a file
// list on top, a docked "Convert" box below it, and a function-key bar at the
// bottom. Highlighting a Markdown file in the list and pressing Enter converts
// it; the conversion itself is delegated to internal/convert. F2 opens the
// settings dialog, where the Markdown flavor is chosen.
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/hkmt-sw/markout/internal/convert"
	"github.com/hkmt-sw/markout/internal/flavor"
	"github.com/hkmt-sw/markout/internal/settings"
)

// format mirrors the two output formats the engine supports.
type format int

const (
	formatDOCX format = iota
	formatPDF
)

func (f format) String() string {
	if f == formatPDF {
		return "PDF"
	}
	return "DOCX"
}

func (f format) ext() string {
	if f == formatPDF {
		return ".pdf"
	}
	return ".docx"
}

func (f format) convertFormat() convert.OutputFormat {
	if f == formatPDF {
		return convert.FormatPDF
	}
	return convert.FormatDOCX
}

// statusKind controls how the status line is colored.
type statusKind int

const (
	statusInfo statusKind = iota
	statusSuccess
	statusError
)

// entry is one row in the file list.
type entry struct {
	name       string
	isDir      bool
	isUp       bool // the ".." entry
	size       int64
	mod        time.Time
	selectable bool // a .md/.markdown file that can be converted
}

// ============================================================================
// Styles
//
// Every color is an AdaptiveColor with a Light and a Dark variant. lipgloss
// detects the terminal background and picks the readable one, so the UI looks
// right on both white and black terminals. Light variants are darker (legible
// on white); Dark variants are lighter (legible on black).
// ============================================================================

var (
	colAccent  = lipgloss.AdaptiveColor{Light: "25", Dark: "39"}   // blue
	colMuted   = lipgloss.AdaptiveColor{Light: "239", Dark: "246"} // borders, labels
	colDim     = lipgloss.AdaptiveColor{Light: "245", Dark: "240"} // sizes, dates
	colText    = lipgloss.AdaptiveColor{Light: "234", Dark: "253"} // primary text
	colDir     = lipgloss.AdaptiveColor{Light: "25", Dark: "75"}   // directories
	colSuccess = lipgloss.AdaptiveColor{Light: "28", Dark: "42"}   // green
	colError   = lipgloss.AdaptiveColor{Light: "124", Dark: "203"} // red
	colSelFg   = lipgloss.AdaptiveColor{Light: "231", Dark: "231"} // text on accent bar

	borderStyle = lipgloss.NewStyle().Foreground(colMuted)
	titleStyle  = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	labelStyle  = lipgloss.NewStyle().Foreground(colMuted)
	pathStyle   = lipgloss.NewStyle().Foreground(colText)
	dimStyle    = lipgloss.NewStyle().Foreground(colDim)

	dirStyle  = lipgloss.NewStyle().Foreground(colDir).Bold(true)
	fileStyle = lipgloss.NewStyle().Foreground(colText)
	skipStyle = lipgloss.NewStyle().Foreground(colDim) // non-markdown files

	cursorStyle = lipgloss.NewStyle().Foreground(colSelFg).Background(colAccent).Bold(true)

	formatActive   = lipgloss.NewStyle().Foreground(colSelFg).Background(colAccent).Bold(true).Padding(0, 1)
	formatInactive = lipgloss.NewStyle().Foreground(colMuted).Padding(0, 1)

	keyStyle     = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	keyDescStyle = lipgloss.NewStyle().Foreground(colMuted)
)

// ============================================================================
// Messages
// ============================================================================

type convertDoneMsg struct {
	output string
	err    error
}

// ============================================================================
// Model
// ============================================================================

// Model is the root bubbletea model for the markout TUI.
type Model struct {
	width  int
	height int

	cwd     string
	entries []entry
	cursor  int
	offset  int // index of the first visible row (scrolling)
	loadErr error

	format         format
	outputOverride string // set when the user edits the path by hand; cleared on cursor move

	editingOutput bool
	outputInput   textinput.Model

	searching   bool   // incremental search mode (Ctrl+S)
	searchQuery string // current search text
	searchMiss  bool   // true when the query matches nothing

	flavor       flavor.Flavor                 // Markdown dialect used for conversions
	saveSettings func(settings.Settings) error // persists the settings; nil in tests
	pickFlavor   bool                          // the settings dialog is open
	flavorCursor int                           // highlighted row in the settings dialog

	confirmOverwrite bool   // waiting for confirmation before clobbering a file
	pendingOut       string // output path awaiting overwrite confirmation
	confirmChoice    int    // focused button: 0 = Overwrite, 1 = Cancel

	converting bool
	spinner    spinner.Model

	status     string
	statusKind statusKind
	lastOutput string // path of the most recent successful conversion

	quit bool
}

// New builds the model rooted at the current working directory. fl is the
// Markdown flavor to start with; changes made in the settings dialog are saved.
func New(fl flavor.Flavor) Model {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	m := newAt(cwd)
	m.flavor = fl
	m.saveSettings = settings.Save
	return m
}

// newAt builds a model rooted at dir (used by tests).
func newAt(dir string) Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 1024

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colAccent)

	m := Model{
		cwd:         dir,
		format:      formatPDF,
		flavor:      flavor.Default(),
		outputInput: ti,
		spinner:     sp,
		status:      "select a Markdown file and press Enter",
		statusKind:  statusInfo,
		width:       80,
		height:      24,
	}
	m.loadEntries()
	return m
}

func (m Model) Init() tea.Cmd { return nil }

// loadEntries reads m.cwd into m.entries, directories first then markdown files.
func (m *Model) loadEntries() {
	m.cursor = 0
	m.offset = 0
	m.entries = nil
	m.loadErr = nil
	m.searching = false
	m.searchQuery = ""
	m.searchMiss = false

	items, err := os.ReadDir(m.cwd)
	if err != nil {
		m.loadErr = err
		return
	}

	var dirs, files []entry
	for _, it := range items {
		name := it.Name()
		if strings.HasPrefix(name, ".") {
			continue // hide dotfiles, mc-style toggle could come later
		}
		info, ierr := it.Info()
		if it.IsDir() {
			d := entry{name: name, isDir: true}
			if ierr == nil {
				d.mod = info.ModTime()
			}
			dirs = append(dirs, d)
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		md := ext == ".md" || ext == ".markdown"
		e := entry{name: name, selectable: md}
		if ierr == nil {
			e.size = info.Size()
			e.mod = info.ModTime()
		}
		files = append(files, e)
	}

	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].name) < strings.ToLower(dirs[j].name) })
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].name) < strings.ToLower(files[j].name) })

	// ".." first unless we are at the filesystem root.
	if parent := filepath.Dir(m.cwd); parent != m.cwd {
		m.entries = append(m.entries, entry{name: "..", isDir: true, isUp: true})
	}
	m.entries = append(m.entries, dirs...)
	m.entries = append(m.entries, files...)
}

// current returns the highlighted entry, or zero value if the list is empty.
func (m Model) current() (entry, bool) {
	if m.cursor < 0 || m.cursor >= len(m.entries) {
		return entry{}, false
	}
	return m.entries[m.cursor], true
}

// inputPath is the absolute path of the highlighted selectable file, or "".
func (m Model) inputPath() string {
	e, ok := m.current()
	if !ok || !e.selectable {
		return ""
	}
	return filepath.Join(m.cwd, e.name)
}

// outputPath is the manual override if set, else derived from the highlight.
func (m Model) outputPath() string {
	if m.outputOverride != "" {
		return m.outputOverride
	}
	in := m.inputPath()
	if in == "" {
		return ""
	}
	return strings.TrimSuffix(in, filepath.Ext(in)) + m.format.ext()
}

// ============================================================================
// Update
// ============================================================================

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case convertDoneMsg:
		m.converting = false
		if msg.err != nil {
			m.status = msg.err.Error()
			m.statusKind = statusError
		} else {
			m.status = "saved → " + msg.output
			m.statusKind = statusSuccess
			m.lastOutput = msg.output
		}
		return m, nil

	case spinner.TickMsg:
		if m.converting {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.quit = true
			return m, tea.Quit
		}
		if m.editingOutput {
			return m.updateEditing(msg)
		}
		if m.confirmOverwrite {
			return m.updateConfirm(msg)
		}
		if m.pickFlavor {
			return m.updateFlavorPicker(msg)
		}
		if m.searching {
			return m.updateSearch(msg)
		}
		return m.updateNav(msg)
	}
	return m, nil
}

func (m Model) updateEditing(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.outputOverride = strings.TrimSpace(m.outputInput.Value())
		m.editingOutput = false
		m.outputInput.Blur()
		return m, nil
	case "esc":
		m.editingOutput = false
		m.outputInput.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.outputInput, cmd = m.outputInput.Update(msg)
	return m, cmd
}

func (m Model) updateNav(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "f10":
		m.quit = true
		return m, tea.Quit

	case "ctrl+s", "/":
		m.searching = true
		m.searchQuery = ""
		m.searchMiss = false
		return m, nil

	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "pgup":
		m.moveCursor(-m.listHeight())
	case "pgdown":
		m.moveCursor(m.listHeight())
	case "home", "g":
		m.cursor = 0
		m.clampScroll()
	case "end", "G":
		m.cursor = len(m.entries) - 1
		m.clampScroll()

	case "left", "h", "backspace":
		return m.goUp()

	case "f2", "s":
		m.pickFlavor = true
		m.flavorCursor = 0
		for i, f := range flavor.All() {
			if f.ID == m.flavor.ID {
				m.flavorCursor = i
			}
		}

	case "f3", "f":
		if m.format == formatDOCX {
			m.format = formatPDF
		} else {
			m.format = formatDOCX
		}

	case "f4", "e":
		if m.inputPath() != "" {
			m.editingOutput = true
			m.outputInput.SetValue(m.outputPath())
			m.outputInput.CursorEnd()
			return m, m.outputInput.Focus()
		}

	case "o":
		if m.lastOutput != "" {
			_ = openFile(m.lastOutput)
		}

	case "enter", "right", "l":
		e, ok := m.current()
		if !ok {
			return m, nil
		}
		if e.isUp {
			return m.goUp()
		}
		if e.isDir {
			m.cwd = filepath.Join(m.cwd, e.name)
			m.outputOverride = ""
			m.loadEntries()
			return m, nil
		}
		// A file: convert it if it is Markdown.
		if msg.String() != "enter" {
			return m, nil // right/l only navigates dirs
		}
		return m.startConversion()
	}
	return m, nil
}

func (m *Model) moveCursor(delta int) {
	if len(m.entries) == 0 {
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.entries) {
		m.cursor = len(m.entries) - 1
	}
	m.outputOverride = "" // derived output follows the highlight
	m.clampScroll()
}

func (m *Model) clampScroll() {
	h := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m Model) goUp() (tea.Model, tea.Cmd) {
	parent := filepath.Dir(m.cwd)
	if parent == m.cwd {
		return m, nil
	}
	m.cwd = parent
	m.outputOverride = ""
	m.loadEntries()
	return m, nil
}

func (m Model) startConversion() (tea.Model, tea.Cmd) {
	in := m.inputPath()
	if in == "" {
		m.status = "highlight a .md file to convert"
		m.statusKind = statusError
		return m, nil
	}
	out := m.outputPath()
	// Guard against silently clobbering an existing file.
	if _, err := os.Stat(out); err == nil {
		m.confirmOverwrite = true
		m.pendingOut = out
		m.confirmChoice = 1 // default focus on Cancel (safe)
		return m, nil
	}
	return m.runConvert(in, out)
}

// runConvert performs the conversion in the background.
func (m Model) runConvert(in, out string) (tea.Model, tea.Cmd) {
	format := m.format
	opts := convert.Options{Flavor: m.flavor}
	m.confirmOverwrite = false
	m.pendingOut = ""
	m.converting = true
	m.status = "converting…"
	m.statusKind = statusInfo
	return m, tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			err := convert.ConvertFileWith(in, out, format.convertFormat(), opts)
			return convertDoneMsg{output: out, err: err}
		},
	)
}

// updateConfirm handles the overwrite popup: arrow/Tab move between the
// buttons, Enter activates the focused one, and y/n are direct shortcuts.
func (m Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "left", "right", "tab", "shift+tab", "h", "l":
		m.confirmChoice = 1 - m.confirmChoice
		return m, nil
	case "enter":
		if m.confirmChoice == 0 {
			return m.runConvert(m.inputPath(), m.pendingOut)
		}
		return m.cancelOverwrite(), nil
	case "y", "Y":
		return m.runConvert(m.inputPath(), m.pendingOut)
	case "n", "N", "esc", "ctrl+g":
		return m.cancelOverwrite(), nil
	}
	return m, nil
}

func (m Model) cancelOverwrite() Model {
	m.confirmOverwrite = false
	m.pendingOut = ""
	m.status = "cancelled — file not overwritten"
	m.statusKind = statusInfo
	return m
}

// updateFlavorPicker handles the settings dialog: arrows move through the
// flavors, Enter selects and saves the highlighted one, Esc leaves it as is.
func (m Model) updateFlavorPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	flavors := flavor.All()
	switch msg.String() {
	case "up", "k":
		if m.flavorCursor > 0 {
			m.flavorCursor--
		}
	case "down", "j":
		if m.flavorCursor < len(flavors)-1 {
			m.flavorCursor++
		}
	case "home", "g":
		m.flavorCursor = 0
	case "end", "G":
		m.flavorCursor = len(flavors) - 1
	case "esc", "f2", "q", "ctrl+g":
		m.pickFlavor = false
	case "enter":
		m.pickFlavor = false
		m.flavor = flavors[m.flavorCursor]
		m.status = "flavor: " + m.flavor.Name
		m.statusKind = statusInfo
		if m.saveSettings != nil {
			if err := m.saveSettings(settings.Settings{Flavor: m.flavor.ID}); err != nil {
				m.status = "flavor set, but not saved: " + err.Error()
				m.statusKind = statusError
			}
		}
	}
	return m, nil
}

// updateSearch handles keys while incremental search (Ctrl+S) is active.
func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+s", "down":
		m.jumpNext()
		return m, nil
	case "up":
		m.jumpPrev()
		return m, nil
	case "esc", "ctrl+g":
		m.endSearch()
		return m, nil
	case "enter":
		// Confirm the search and act on the highlighted entry.
		m.endSearch()
		return m.updateNav(tea.KeyMsg{Type: tea.KeyEnter})
	case "backspace":
		if r := []rune(m.searchQuery); len(r) > 0 {
			m.searchQuery = string(r[:len(r)-1])
		}
		m.searchFrom(m.cursor)
		return m, nil
	}
	if msg.Type == tea.KeyRunes {
		m.searchQuery += string(msg.Runes)
		m.searchFrom(m.cursor)
	}
	return m, nil
}

func (m *Model) endSearch() {
	m.searching = false
	m.searchQuery = ""
	m.searchMiss = false
}

// searchFrom jumps to the first entry matching the query, starting at index
// start (inclusive) and wrapping around.
func (m *Model) searchFrom(start int) {
	if m.searchQuery == "" {
		m.searchMiss = false
		return
	}
	if idx, ok := m.matchFrom(m.searchQuery, start, +1); ok {
		m.cursor = idx
		m.searchMiss = false
		m.clampScroll()
	} else {
		m.searchMiss = true
	}
}

func (m *Model) jumpNext() {
	if m.searchQuery == "" || len(m.entries) == 0 {
		return
	}
	if idx, ok := m.matchFrom(m.searchQuery, (m.cursor+1)%len(m.entries), +1); ok {
		m.cursor = idx
		m.searchMiss = false
		m.clampScroll()
	} else {
		m.searchMiss = true
	}
}

func (m *Model) jumpPrev() {
	if m.searchQuery == "" || len(m.entries) == 0 {
		return
	}
	start := (m.cursor - 1 + len(m.entries)) % len(m.entries)
	if idx, ok := m.matchFrom(m.searchQuery, start, -1); ok {
		m.cursor = idx
		m.searchMiss = false
		m.clampScroll()
	} else {
		m.searchMiss = true
	}
}

// matchFrom scans entries from start in the given direction (+1/-1), wrapping,
// for the first whose name contains q (case-insensitive).
func (m Model) matchFrom(q string, start, dir int) (int, bool) {
	n := len(m.entries)
	if n == 0 {
		return 0, false
	}
	ql := strings.ToLower(q)
	for i := 0; i < n; i++ {
		idx := ((start+dir*i)%n + n) % n
		if strings.Contains(strings.ToLower(m.entries[idx].name), ql) {
			return idx, true
		}
	}
	return 0, false
}

// ============================================================================
// View
// ============================================================================

// listHeight is the number of file rows that fit on screen.
func (m Model) listHeight() int {
	// Chrome: top border(1) + cwd(1) + convert box [sep+flavor+format+output+
	// status](5) + fkey sep(1) + fkey(1) + bottom border(1) = 10 lines around
	// the list.
	h := m.height - 10
	if h < 1 {
		h = 1
	}
	return h
}

func (m Model) innerWidth() int {
	w := m.width - 2
	if w < 10 {
		w = 10
	}
	return w
}

func (m Model) View() string {
	if m.quit {
		return ""
	}
	inner := m.innerWidth()
	var lines []string

	lines = append(lines, m.borderTop("markout"))

	// Current directory line.
	cwd := m.cwd
	if h, err := os.UserHomeDir(); err == nil && strings.HasPrefix(cwd, h) {
		cwd = "~" + strings.TrimPrefix(cwd, h)
	}
	lines = append(lines, m.row(pathStyle.Render(truncLeft(printable(cwd), inner))))

	// File list.
	lines = append(lines, m.fileRows()...)

	// Convert box.
	lines = append(lines, m.borderSep("Convert"))
	lines = append(lines, m.row(m.flavorLine()))
	lines = append(lines, m.row(m.formatLine()))
	lines = append(lines, m.row(m.outputLine()))
	lines = append(lines, m.row(m.statusLine()))

	// Function-key bar.
	lines = append(lines, m.borderSep(""))
	lines = append(lines, m.row(m.keyBar()))

	lines = append(lines, m.borderBottom())
	base := strings.Join(lines, "\n")

	if m.confirmOverwrite {
		base = m.overlayCentered(base, m.overwriteDialog())
	}
	if m.pickFlavor {
		base = m.overlayCentered(base, m.flavorDialog())
	}
	return base
}

// flavorDialog renders the settings dialog: the list of Markdown flavors with
// the description of the highlighted one underneath.
func (m Model) flavorDialog() string {
	flavors := flavor.All()

	w := 62
	if w > m.width-4 {
		w = m.width - 4
	}
	inner := w - 6 // border (2) + horizontal padding (4)
	if inner < 10 {
		inner = 10
	}

	// Show as many flavors as fit, scrolled to keep the cursor in view.
	visible := m.height - 10
	if visible > len(flavors) {
		visible = len(flavors)
	}
	if visible < 1 {
		visible = 1
	}
	first := 0
	if m.flavorCursor >= visible {
		first = m.flavorCursor - visible + 1
	}

	rows := []string{titleStyle.Render("Settings · Markdown flavor"), ""}
	for i := first; i < first+visible; i++ {
		f := flavors[i]
		mark := "  "
		if f.ID == m.flavor.ID {
			mark = " ✓"
		}
		name := truncRight(f.Name, inner-4)
		line := "  " + name + strings.Repeat(" ", inner-4-lipgloss.Width(name)) + mark
		switch {
		case i == m.flavorCursor:
			line = cursorStyle.Render("▶" + line[1:])
		case f.ID == m.flavor.ID:
			line = lipgloss.NewStyle().Foreground(colSuccess).Render(line)
		default:
			line = fileStyle.Render(line)
		}
		rows = append(rows, line)
	}
	rows = append(rows,
		"",
		lipgloss.NewStyle().Foreground(colMuted).Width(inner).Height(2).Render(flavors[m.flavorCursor].Description),
		"",
		keyStyle.Render("↑↓")+" "+keyDescStyle.Render("Select")+"   "+
			keyStyle.Render("↵")+" "+keyDescStyle.Render("Save")+"   "+
			keyStyle.Render("Esc")+" "+keyDescStyle.Render("Cancel"),
	)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Padding(0, 2).
		Width(w - 2).
		Render(strings.Join(rows, "\n"))
}

// overwriteDialog renders the modal shown before clobbering an existing file.
func (m Model) overwriteDialog() string {
	warn := lipgloss.NewStyle().Foreground(colError).Bold(true)
	name := pathStyle.Render(printable(filepath.Base(m.pendingOut)))

	body := lipgloss.JoinVertical(lipgloss.Center,
		warn.Render("⚠  File already exists"),
		"",
		name,
		"",
		keyDescStyle.Render("Overwrite it?"),
		"",
		m.button("Overwrite", 0)+"   "+m.button("Cancel", 1),
	)

	w := 40
	if w > m.width-4 {
		w = m.width - 4
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Padding(1, 2).
		Width(w).
		Align(lipgloss.Center).
		Render(body)
}

// button renders a dialog button, highlighted when it is the focused choice.
func (m Model) button(label string, idx int) string {
	if m.confirmChoice == idx {
		return lipgloss.NewStyle().
			Foreground(colSelFg).Background(colAccent).Bold(true).
			Padding(0, 2).Render(label)
	}
	return lipgloss.NewStyle().
		Foreground(colMuted).
		Padding(0, 2).Render(label)
}

// overlayCentered composites fg on top of bg, centered, preserving bg around it.
func (m Model) overlayCentered(bg, fg string) string {
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")
	fgW := lipgloss.Width(fg)

	x := (m.width - fgW) / 2
	if x < 0 {
		x = 0
	}
	y := (len(bgLines) - len(fgLines)) / 2
	if y < 0 {
		y = 0
	}

	for i, fl := range fgLines {
		row := y + i
		if row < 0 || row >= len(bgLines) {
			continue
		}
		b := bgLines[row]
		left := ansi.Truncate(b, x, "")
		if lw := ansi.StringWidth(left); lw < x {
			left += strings.Repeat(" ", x-lw)
		}
		right := ansi.TruncateLeft(b, x+ansi.StringWidth(fl), "")
		bgLines[row] = left + fl + right
	}
	return strings.Join(bgLines, "\n")
}

func (m Model) fileRows() []string {
	h := m.listHeight()
	inner := m.innerWidth()
	rows := make([]string, 0, h)

	if m.loadErr != nil {
		rows = append(rows, m.row(skipStyle.Render("cannot read directory: "+m.loadErr.Error())))
		for len(rows) < h {
			rows = append(rows, m.row(""))
		}
		return rows
	}

	end := m.offset + h
	if end > len(m.entries) {
		end = len(m.entries)
	}
	for i := m.offset; i < end; i++ {
		rows = append(rows, m.row(m.renderEntry(i, inner)))
	}
	for len(rows) < h {
		rows = append(rows, m.row(""))
	}
	return rows
}

// renderEntry formats one file-list row to exactly innerWidth visible columns.
func (m Model) renderEntry(i, inner int) string {
	e := m.entries[i]
	cursor := i == m.cursor

	var right string
	switch {
	case e.isUp:
		right = ""
	case e.isDir:
		right = "<DIR>"
	default:
		right = fmt.Sprintf("%6s  %s", humanSize(e.size), e.mod.Format("01-02 15:04"))
	}

	marker := " "
	if cursor {
		marker = "▶"
	}
	// marker(1) + space(1) + name + space + right + trailing space(1)
	nameW := inner - 2 - lipgloss.Width(right) - 2
	if nameW < 3 {
		nameW = 3
	}
	name := printable(e.name)
	if e.isDir && !e.isUp {
		name += "/"
	}
	name = truncRight(name, nameW)
	namePadded := name + strings.Repeat(" ", nameW-lipgloss.Width(name))

	plain := fmt.Sprintf("%s %s %s ", marker, namePadded, right)
	// Ensure exact inner width.
	if w := lipgloss.Width(plain); w < inner {
		plain += strings.Repeat(" ", inner-w)
	}

	if cursor {
		return cursorStyle.Width(inner).Render(plain)
	}
	// Color by type without breaking the fixed width: style only the name span.
	var nameStyled string
	switch {
	case e.isDir:
		nameStyled = dirStyle.Render(namePadded)
	case e.selectable:
		nameStyled = fileStyle.Render(namePadded)
	default:
		nameStyled = skipStyle.Render(namePadded)
	}
	styled := fmt.Sprintf("%s %s %s ", marker, nameStyled, dimStyle.Render(right))
	if w := lipgloss.Width(styled); w < inner {
		styled += strings.Repeat(" ", inner-w)
	}
	return styled
}

func (m Model) flavorLine() string {
	return labelStyle.Render("Flavor:  ") + pathStyle.Render(m.flavor.Name)
}

func (m Model) formatLine() string {
	docx, pdf := formatInactive.Render("DOCX"), formatInactive.Render("PDF")
	if m.format == formatDOCX {
		docx = formatActive.Render("DOCX")
	} else {
		pdf = formatActive.Render("PDF")
	}
	return labelStyle.Render("Format:  ") + docx + " " + pdf
}

func (m Model) outputLine() string {
	if m.editingOutput {
		return labelStyle.Render("Output:  ") + m.outputInput.View()
	}
	out := m.outputPath()
	if out == "" {
		return labelStyle.Render("Output:  ") + dimStyle.Render("(highlight a .md file)")
	}
	return labelStyle.Render("Output:  ") + pathStyle.Render(printable(filepath.Base(out)))
}

func (m Model) statusLine() string {
	if m.searching {
		s := labelStyle.Render("Search:  ") + pathStyle.Render(m.searchQuery) +
			lipgloss.NewStyle().Foreground(colAccent).Render("▏")
		if m.searchMiss {
			s += "  " + lipgloss.NewStyle().Foreground(colError).Render("(no match)")
		}
		return s
	}
	prefix := labelStyle.Render("Status:  ")
	if m.converting {
		return prefix + m.spinner.View() + " " + m.status
	}
	var st lipgloss.Style
	switch m.statusKind {
	case statusSuccess:
		st = lipgloss.NewStyle().Foreground(colSuccess)
	case statusError:
		st = lipgloss.NewStyle().Foreground(colError)
	default:
		st = lipgloss.NewStyle().Foreground(colText)
	}
	return prefix + st.Render(printable(m.status))
}

func (m Model) keyBar() string {
	var keys [][2]string
	if m.searching {
		keys = [][2]string{
			{"^S/↓", "Next"},
			{"↑", "Prev"},
			{"↵", "Go"},
			{"Esc", "Cancel"},
		}
	} else {
		keys = [][2]string{
			{"↵", "Convert"},
			{"^S", "Find"},
			{"F2", "Flavor"},
			{"F3", "Format"},
			{"F4", "Output"},
		}
		if m.lastOutput != "" {
			keys = append(keys, [2]string{"o", "Open"})
		}
		keys = append(keys, [2]string{"F10", "Quit"})
	}

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, keyStyle.Render(k[0])+" "+keyDescStyle.Render(k[1]))
	}
	return strings.Join(parts, "   ")
}

// ---- box drawing -----------------------------------------------------------

func (m Model) row(content string) string {
	inner := m.innerWidth()
	if w := lipgloss.Width(content); w < inner {
		content += strings.Repeat(" ", inner-w)
	} else if w > inner {
		content = truncRight(content, inner)
	}
	b := borderStyle.Render("│")
	return b + content + b
}

func (m Model) borderTop(title string) string { return m.hline("┌", "┐", title) }
func (m Model) borderSep(title string) string { return m.hline("├", "┤", title) }
func (m Model) borderBottom() string          { return m.hline("└", "┘", "") }

func (m Model) hline(left, right, title string) string {
	inner := m.innerWidth()
	var label string
	if title != "" {
		label = "─ " + titleStyle.Render(title) + " "
	}
	dashes := inner - lipgloss.Width(label)
	if dashes < 0 {
		dashes = 0
	}
	return borderStyle.Render(left) + label +
		borderStyle.Render(strings.Repeat("─", dashes)) + borderStyle.Render(right)
}

// ============================================================================
// Helpers
// ============================================================================

func humanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.0f%c", float64(b)/float64(div), "KMGTPE"[exp])
}

// truncRight keeps the start of s, trimming the end to fit width w.
func truncRight(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > w {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

// truncLeft keeps the end of s (useful for long paths), trimming the start.
func truncLeft(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > w {
		r = r[1:]
	}
	return "…" + string(r)
}

// printable replaces control and other non-printing characters, so a file
// name cannot smuggle terminal escape sequences into the screen.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return '?'
	}, s)
}

func openFile(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path).Run()
	case "windows":
		// Not "cmd /c start": cmd would interpret characters such as & in the
		// file name as commands.
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", path).Run()
	default:
		return exec.Command("xdg-open", path).Run()
	}
}

// Run starts the TUI program with the given Markdown flavor selected and
// blocks until the user quits.
func Run(fl flavor.Flavor) error {
	p := tea.NewProgram(New(fl), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
