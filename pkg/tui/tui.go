package tui

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Colors
var (
	colorPrimary     = lipgloss.Color("#7C3AED")
	colorSecondary   = lipgloss.Color("#06B6D4")
	colorSuccess     = lipgloss.Color("#10B981")
	colorWarning     = lipgloss.Color("#F59E0B")
	colorError       = lipgloss.Color("#EF4444")
	colorDim         = lipgloss.Color("#6B7280")
	colorUserBg      = lipgloss.Color("#1F2937")
	colorAssistantBg = lipgloss.Color("#111827")
	colorThinking    = lipgloss.Color("#9CA3AF")
)

// Styles
var (
	styleApp    = lipgloss.NewStyle()
	styleHeader = lipgloss.NewStyle().
			Background(colorPrimary).
			Foreground(lipgloss.Color("#FFFFFF")).
			Bold(true).
			Padding(0, 2)
	styleFooter = lipgloss.NewStyle().
			Background(lipgloss.Color("#0F172A")).
			Foreground(lipgloss.Color("#CBD5E1")).
			Padding(0, 1)
	stylePrompt = lipgloss.NewStyle().
			Foreground(colorSecondary).
			Bold(true)
	styleUser = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E2E8F0")).
			Background(colorUserBg).
			Padding(0, 1).
			Margin(0, 0, 1, 0)
	styleAssistant = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E2E8F0")).
			Background(colorAssistantBg).
			Padding(0, 1).
			Margin(0, 0, 1, 0)
	styleThinking = lipgloss.NewStyle().
			Foreground(colorThinking).
			Italic(true).
			Padding(0, 1).
			Margin(0, 0, 1, 0)
	styleTool = lipgloss.NewStyle().
			Foreground(colorSecondary).
			Padding(0, 1).
			Margin(0, 0, 1, 0)
	styleError = lipgloss.NewStyle().
			Foreground(colorError).
			Bold(true)
	styleStats = lipgloss.NewStyle().
			Foreground(colorDim).
			Italic(true)
	styleKey = lipgloss.NewStyle().
			Foreground(colorSecondary).
			Bold(true)
	styleKeyLabel = lipgloss.NewStyle().
			Foreground(colorDim)
)

// Message types for the TUI
type tuiMessage struct {
	kind string // "user", "assistant", "thinking", "tool", "error", "stats", "system"
	data string
}

// busyMsg updates the busy indicator from the agent goroutine.
type busyMsg struct {
	busy  bool
	label string
}

// statsMsg updates the session stats footer from the agent goroutine.
type statsMsg struct {
	turns       int
	promptTok   int
	genTok      int
	totalMillis int64
}

// sessionStats tracks cumulative usage
type sessionStats struct {
	turns       int
	promptTok   int
	genTok      int
	totalMillis int64
}

// picker is a small interactive selection menu rendered above the input.
type picker struct {
	title    string
	options  []string
	selected int
	onSelect func(int)
}

// textPrompt asks the user for a single line of text (API key, URL, ...).
type textPrompt struct {
	title    string
	hint     string
	masked   bool
	onSubmit func(string)
}

// Model is the bubbletea model for the gokodek TUI.
type Model struct {
	viewport     viewport.Model
	width        int
	height       int
	messages     []tuiMessage
	busy         bool
	busyLabel    string
	input        string
	cursorIdx    int
	history      []string
	stats        sessionStats
	modelName    string
	workspace    string
	mode         string
	recentModels []string
	onSubmit     func(string)
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
	spinnerIdx   int
	lastTick     time.Time
	ch           chan tea.Msg
	onCommand    func(string) string
	lastCommand  string
	picker       *picker
	textPrompt   *textPrompt
	escCount     int
	onCancel     func()
	follow       bool // auto-scroll to bottom on new content until the user scrolls up
}

// NewModel creates a new TUI model.
func NewModel(modelName, workspace string, onSubmit func(string)) *Model {
	ctx, cancel := context.WithCancel(context.Background())
	return &Model{
		viewport:  viewport.New(0, 0),
		modelName: modelName,
		workspace: workspace,
		onSubmit:  onSubmit,
		ctx:       ctx,
		cancel:    cancel,
		ch:        make(chan tea.Msg, 64),
		follow:    true,
		mode:      "build",
	}
}

// SetChannel attaches the program channel so goroutine updates reach the loop.
func (m *Model) SetChannel(ch chan tea.Msg) {
	m.ch = ch
}

func (m *Model) SetModelName(name string) { m.modelName = name }
func (m *Model) Mode() string             { return m.mode }

func (m *Model) SetMode(mode string) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "plan" {
		mode = "build"
	}
	m.mode = mode
}
func (m *Model) SetRecentModels(names []string) { m.recentModels = append([]string(nil), names...) }

// SetOnCommand registers a callback that returns help text for a command as the
// user types it. Returning "" means the current input is not a complete command.
func (m *Model) SetOnCommand(fn func(string) string) {
	m.onCommand = fn
}

// ShowPicker opens an interactive selection menu. onSelect receives the chosen
// option index. It is replaced by any new call; closing happens on Enter/Esc.
func (m *Model) IsPickerOpen() bool { return m.picker != nil }

func (m *Model) ShowPicker(title string, options []string, onSelect func(int)) {
	m.picker = &picker{title: title, options: options, onSelect: onSelect}
	m.lastCommand = ""
}

// SetOnCancel registers a callback invoked when the user presses Esc three
// times while the agent is busy. Use it to cancel the running execution.
func (m *Model) SetOnCancel(fn func()) {
	m.onCancel = fn
}

// AskText opens a single-line text input (API keys, URLs, ...).
// masked hides the typed value with bullets.
func (m *Model) AskText(title, hint string, masked bool, onSubmit func(string)) {
	m.textPrompt = &textPrompt{title: title, hint: hint, masked: masked, onSubmit: onSubmit}
	m.input = ""
}

// renderTextPrompt draws the prompt banner above the input.
func (m *Model) renderTextPrompt() string {
	if m.textPrompt == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(styleHeader.Render(" "+m.textPrompt.title+" ") + "\n")
	display := m.input
	if m.textPrompt.masked && display != "" {
		display = strings.Repeat("•", len(display))
	}
	sb.WriteString(styleStats.Render(" "+m.textPrompt.hint+" (Enter confirmar · Esc cancelar)") + "\n")
	sb.WriteString(styleUser.Render(" "+display+"█") + "\n")
	return sb.String()
}

// maybeShowCommandHelp displays command help live as the user types a command.
// The help is shown once per distinct command text to avoid spam.
func (m *Model) maybeShowCommandHelp() {
	if m.onCommand == nil {
		return
	}
	text := m.onCommand(m.input)
	if text == "" || text == m.lastCommand {
		return
	}
	m.lastCommand = text
	m.addMessage(tuiMessage{kind: "system", data: text})
}

// pollChannel returns a command that forwards channel messages into the loop.
func (m *Model) pollChannel() tea.Cmd {
	return func() tea.Msg {
		return <-m.ch
	}
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(tickSpinner(), m.pollChannel())
}

func tickSpinner() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

type spinnerTickMsg struct{}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if msg.Width > 35 {
			m.viewport.Width = msg.Width - 32
		} else {
			m.viewport.Width = msg.Width
		}
		if msg.Height > 8 {
			m.viewport.Height = msg.Height - 6 // reserve footer + input
		} else {
			m.viewport.Height = 1
		}
		m.viewport.SetContent(m.renderMessages())
		if m.follow {
			m.viewport.GotoBottom()
		}
		return m, nil

	case tea.KeyMsg:
		// When a text prompt is open, keys edit the input and Enter submits it.
		if m.textPrompt != nil {
			switch msg.String() {
			case "enter", "\r", "\n":
				value := m.input
				onSubmit := m.textPrompt.onSubmit
				m.textPrompt = nil
				m.input = ""
				if onSubmit != nil {
					onSubmit(strings.TrimSpace(value))
				}
				return m, nil
			case "esc", "ctrl+c":
				m.textPrompt = nil
				m.input = ""
				return m, nil
			case "backspace":
				if len(m.input) > 0 {
					m.input = m.input[:len(m.input)-1]
				}
				return m, nil
			default:
				if len(msg.String()) == 1 && msg.String()[0] >= 32 {
					m.input += msg.String()
				}
				return m, nil
			}
		}
		// When a picker is open, arrow keys and Enter drive the menu instead of
		// the text input.
		if m.picker != nil {
			switch msg.String() {
			case "up", "k":
				if m.picker.selected > 0 {
					m.picker.selected--
				}
				return m, nil
			case "down", "j":
				if m.picker.selected < len(m.picker.options)-1 {
					m.picker.selected++
				}
				return m, nil
			case "enter", "\r", "\n", " ":
				onSelect := m.picker.onSelect
				index := m.picker.selected
				m.picker = nil
				m.input = ""
				if onSelect != nil {
					onSelect(index)
				}
				return m, nil
			case "esc", "ctrl+c":
				m.picker = nil
				m.input = ""
				return m, nil
			}
			return m, nil
		}
		switch msg.String() {
		case "ctrl+c":
			m.cancel()
			return m, tea.Quit
		case "left":
			if m.cursorIdx > 0 {
				m.cursorIdx--
			}
			return m, nil
		case "right":
			if m.cursorIdx < len(m.input) {
				m.cursorIdx++
			}
			return m, nil
		case "home", "ctrl+a":
			m.cursorIdx = 0
			return m, nil
		case "end", "ctrl+e":
			m.cursorIdx = len(m.input)
			return m, nil
		case "f2":
			if len(m.recentModels) > 0 {
				// Rotate to next recent model in list
				nextIdx := 0
				for i, name := range m.recentModels {
					if strings.EqualFold(name, m.modelName) || strings.EqualFold(name, strings.TrimPrefix(m.modelName, "ollama/")) {
						nextIdx = (i + 1) % len(m.recentModels)
						break
					}
				}
				chosen := m.recentModels[nextIdx]
				m.modelName = chosen
				m.addMessage(tuiMessage{kind: "system", data: "F2: Modelo cambiado a " + chosen})
			}
			return m, nil
		case "tab":
			if m.mode == "plan" {
				m.mode = "build"
			} else {
				m.mode = "plan"
			}
			m.addMessage(tuiMessage{kind: "system", data: "Modo activo: " + m.mode})
			return m, nil
		case "pgup":
			m.viewport.PageUp()
			m.follow = m.viewport.AtBottom()
			return m, nil
		case "pgdown":
			m.viewport.PageDown()
			m.follow = m.viewport.AtBottom()
			return m, nil
		case "ctrl+up":
			m.viewport.LineUp(3)
			m.follow = m.viewport.AtBottom()
			return m, nil
		case "ctrl+down":
			m.viewport.LineDown(3)
			m.follow = m.viewport.AtBottom()
			return m, nil
		case "esc":
			// Triple Esc cancels the running execution.
			if m.busy {
				m.escCount++
				if m.escCount >= 3 {
					m.escCount = 0
					m.busy = false
					m.busyLabel = ""
					if m.onCancel != nil {
						m.onCancel()
					}
				}
				return m, nil
			}
			return m, nil
		case "enter", "\r", "\n":
			if m.busy {
				return m, nil
			}
			prompt := strings.TrimSpace(m.input)
			m.input = "" // always clear the input box after sending
			m.cursorIdx = 0
			m.lastCommand = ""
			m.escCount = 0
			if prompt == "" {
				return m, nil
			}
			m.history = append(m.history, prompt)
			m.addMessage(tuiMessage{kind: "user", data: prompt})
			m.follow = true // resume auto-scroll when the user sends a message
			m.busy = true
			m.busyLabel = "Pensando..."
			m.onSubmit(prompt)
			return m, nil
		case "backspace":
			if m.cursorIdx > 0 && len(m.input) > 0 {
				m.input = m.input[:m.cursorIdx-1] + m.input[m.cursorIdx:]
				m.cursorIdx--
			}
			return m, nil
		case "delete":
			if m.cursorIdx < len(m.input) {
				m.input = m.input[:m.cursorIdx] + m.input[m.cursorIdx+1:]
			}
			return m, nil
		case "up":
			if len(m.history) > 0 {
				m.input = m.history[len(m.history)-1]
				m.cursorIdx = len(m.input)
			}
			return m, nil
		case "ctrl+l":
			m.input = ""
			m.cursorIdx = 0
			m.lastCommand = ""
			return m, nil
		default:
			// Handle printable characters
			if len(msg.String()) == 1 && msg.String()[0] >= 32 {
				ch := msg.String()
				m.input = m.input[:m.cursorIdx] + ch + m.input[m.cursorIdx:]
				m.cursorIdx++
			}
			m.maybeShowCommandHelp()
			return m, nil
		}

	case spinnerTickMsg:
		if m.busy {
			m.spinnerIdx = (m.spinnerIdx + 1) % len(spinnerFrames)
		}
		return m, tickSpinner()

	case tuiMessage:
		m.addMessage(msg)
		return m, nil

	case busyMsg:
		m.busy = msg.busy
		m.busyLabel = msg.label
		// Re-arm the channel poll so the next busy/stats message is received.
		return m, m.pollChannel()

	case statsMsg:
		m.stats.turns = msg.turns
		m.stats.promptTok = msg.promptTok
		m.stats.genTok = msg.genTok
		m.stats.totalMillis = msg.totalMillis
		return m, m.pollChannel()
	}
	return m, nil
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// addMessage appends a message. Consecutive streaming chunks of the same kind
// (assistant content or thinking) merge into the last message so the user sees
// the text flow in real time instead of one block per chunk.
func (m *Model) addMessage(msg tuiMessage) {
	m.mu.Lock()
	if msg.kind == "thinking" || msg.kind == "assistant" {
		if n := len(m.messages); n > 0 && m.messages[n-1].kind == msg.kind {
			m.messages[n-1].data += msg.data
			m.mu.Unlock()
			m.refreshViewport()
			return
		}
	}
	m.messages = append(m.messages, msg)
	m.mu.Unlock()
	m.refreshViewport()
}

func (m *Model) refreshViewport() {
	if m.width == 0 {
		return // window not sized yet; content will be set on first resize
	}
	m.mu.Lock()
	follow := m.follow
	m.mu.Unlock()
	m.viewport.SetContent(m.renderMessages())
	if follow {
		m.viewport.GotoBottom()
	}
	// When the user has scrolled away from the bottom the viewport keeps its
	// offset so they can read and copy earlier output in peace.
}

// PushMessage is called from the agent loop (in the main goroutine) to display
// assistant output, thinking, tool progress, errors and stats.
func (m *Model) PushMessage(kind, data string) {
	m.addMessage(tuiMessage{kind: kind, data: data})
}

// SetBusy toggles the busy indicator (called from the agent goroutine).
func (m *Model) SetBusy(busy bool, label string) {
	m.busy = busy
	m.busyLabel = label
}

// sendBusy forwards a busy update through the model's channel. It is a no-op
// when no program channel is attached.
func (m *Model) sendBusy(busy bool, label string) {
	if m.ch != nil {
		m.ch <- busyMsg{busy: busy, label: label}
	}
}

// sendStats forwards a stats update through the model's channel.
func (m *Model) sendStats(turns int, promptTok, genTok int, totalMillis int64) {
	if m.ch != nil {
		m.ch <- statsMsg{turns: turns, promptTok: promptTok, genTok: genTok, totalMillis: totalMillis}
	}
}

// renderMessages builds the full conversation content for the viewport.
func (m *Model) renderMessages() string {
	var sb strings.Builder
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, msg := range m.messages {
		switch msg.kind {
		case "user":
			sb.WriteString(styleUser.Render("❯ "+msg.data) + "\n")
		case "assistant":
			sb.WriteString(styleAssistant.Render(renderMarkdownLite(msg.data)) + "\n")
		case "thinking":
			sb.WriteString(styleThinking.Render("💭 "+msg.data) + "\n")
		case "tool":
			sb.WriteString(styleTool.Render("🔧 "+msg.data) + "\n")
		case "error":
			sb.WriteString(styleError.Render("✗ "+msg.data) + "\n")
		case "stats":
			sb.WriteString(styleStats.Render(msg.data) + "\n")
		case "system":
			sb.WriteString(styleStats.Render(msg.data) + "\n")
		}
	}
	return sb.String()
}

// View implements tea.Model.
func (m *Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	// Header
	header := styleHeader.Render(fmt.Sprintf(" gokodek  •  modelo: %s  •  %s  •  modo: %s ", m.modelName, m.workspace, m.mode))

	// Busy indicator with triple-Esc cancel hint
	var busyStr string
	if m.busy {
		busyStr = fmt.Sprintf(" %s %s ", spinnerFrames[m.spinnerIdx], m.busyLabel)
		if m.escCount > 0 {
			busyStr += fmt.Sprintf("  %sEsc×%d%s para cancelar", styleKey.Render(""), 3-m.escCount, "")
		}
	}

	// Footer with options
	options := fmt.Sprintf(
		"%s  %s  %s  %s  %s  %s  %s  %s",
		styleKey.Render("Enter")+styleKeyLabel.Render(" Send "),
		styleKey.Render("↑")+styleKeyLabel.Render(" History "),
		styleKey.Render("PgUp/PgDn")+styleKeyLabel.Render(" Scroll "),
		styleKey.Render("/help")+styleKeyLabel.Render(" Help "),
		styleKey.Render("/modelo")+styleKeyLabel.Render(" Model "),
		styleKey.Render("/new")+styleKeyLabel.Render(" Nuevo "),
		styleKey.Render("/config")+styleKeyLabel.Render(" Config "),
		styleKey.Render("/talk")+styleKeyLabel.Render(" Debate "),
	)
	footer := styleFooter.Render(options + busyStr)

	// Picker menu (shown above the input while open)
	var pickerStr string
	if m.picker != nil {
		pickerStr = m.renderPicker()
	}
	// Text prompt banner (API key / URL entry)
	var promptStr string
	if m.textPrompt != nil {
		promptStr = m.renderTextPrompt()
	}

	// Input box with positional cursor rendering
	cursorPos := m.cursorIdx
	if cursorPos < 0 {
		cursorPos = 0
	}
	if cursorPos > len(m.input) {
		cursorPos = len(m.input)
	}

	left := m.input[:cursorPos]
	right := ""
	cursorChar := "█"
	if cursorPos < len(m.input) {
		cursorChar = string(m.input[cursorPos])
		right = m.input[cursorPos+1:]
	}
	styledCursor := lipgloss.NewStyle().Reverse(true).Render(cursorChar)
	inputText := left + styledCursor + right

	inputBox := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E2E8F0")).
		Background(colorUserBg).
		Padding(0, 1).
		Width(m.width - 4).
		Render("❯ " + inputText)

	// Stats line
	statsStr := styleStats.Render(fmt.Sprintf(
		"turns: %d  •  prompt: %d tok  •  generated: %d tok  •  total: %s",
		m.stats.turns, m.stats.promptTok, m.stats.genTok, formatDuration(m.stats.totalMillis),
	))
	if !m.viewport.AtBottom() {
		statsStr += "  " + styleKey.Render(fmt.Sprintf("↑ scroll %d%% — PgUp/PgDn mueve, End vuelve al final", int(m.viewport.ScrollPercent()*100)))
	}

	// Sidebar lateral de métricas de tokens
	sidebarStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(colorDim).
		Padding(0, 1).
		Width(30)

	totalTok := m.stats.promptTok + m.stats.genTok
	sidebarContent := fmt.Sprintf(
		"%s\n%s\n\n%s\nPrompt: %d\nGen: %d\nTotal: %d\n\n%s\nModelo: %s\nModo: %s\nF2: Rotar modelos",
		styleHeader.Render(" METRICAS "),
		styleStats.Render("Tokens Conversación"),
		styleKey.Render("✦ RESUMEN TOKENS"),
		m.stats.promptTok,
		m.stats.genTok,
		totalTok,
		styleKey.Render("✦ PROYECTO"),
		m.modelName,
		m.mode,
	)
	sidebar := sidebarStyle.Render(sidebarContent)

	mainView := lipgloss.JoinHorizontal(lipgloss.Top,
		m.viewport.View(),
		sidebar,
	)

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		mainView,
		promptStr,
		pickerStr,
		inputBox,
		statsStr,
		footer,
	)
}

// renderPicker draws the interactive selection menu.
func (m *Model) renderPicker() string {
	if m.picker == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(styleHeader.Render(" "+m.picker.title+" ") + "\n")
	for i, option := range m.picker.options {
		marker := "  "
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("#E2E8F0"))
		if i == m.picker.selected {
			marker = "❯ "
			style = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FFFFFF")).
				Bold(true).
				Background(colorPrimary)
		}
		sb.WriteString(style.Render(marker+option) + "\n")
	}
	sb.WriteString(styleStats.Render("↑/↓ navegar · Enter seleccionar · Esc cancelar") + "\n")
	return sb.String()
}

var markdownBold = regexp.MustCompile(`\*\*(.+?)\*\*`)
var markdownCode = regexp.MustCompile("`([^`]+)`")

func renderMarkdownLite(value string) string {
	value = markdownBold.ReplaceAllString(value, "$1")
	value = markdownCode.ReplaceAllString(value, "$1")
	value = strings.ReplaceAll(value, "|||", "")
	return value
}

func formatDuration(millis int64) string {
	if millis < 1000 {
		return fmt.Sprintf("%dms", millis)
	}
	return fmt.Sprintf("%.1fs", float64(millis)/1000)
}

// UpdateStats updates the session statistics display.
func (m *Model) UpdateStats(turns int, promptTok, genTok int, totalMillis int64) {
	m.mu.Lock()
	m.stats.turns = turns
	m.stats.promptTok = promptTok
	m.stats.genTok = genTok
	m.stats.totalMillis = totalMillis
	m.mu.Unlock()
}

// Run starts the TUI. onSubmit receives each submitted prompt.
func Run(modelName, workspace string, onSubmit func(string)) error {
	m := NewModel(modelName, workspace, onSubmit)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
