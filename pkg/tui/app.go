package tui

import (
	"io"
	"regexp"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07]*(\x07|\x1b\\)|\x1b[()][0-9A-Za-z]|\r`)

// stripANSI removes terminal escape sequences from text.
func stripANSI(text string) string {
	return ansiPattern.ReplaceAllString(text, "")
}

// bridgeWriter implements io.Writer and forwards each chunk to the TUI program
// as a message of the given kind. It is safe for concurrent use from the agent
// goroutine while the TUI program is running.
type bridgeWriter struct {
	p    *tea.Program
	kind string
	mu   sync.Mutex
}

func (w *bridgeWriter) Write(p []byte) (int, error) {
	text := stripANSI(string(p))
	if text != "" {
		w.mu.Lock()
		w.p.Send(tuiMessage{kind: w.kind, data: text})
		w.mu.Unlock()
	}
	return len(p), nil
}

// App wires an agent's output writers to a running TUI program.
type App struct {
	program        *tea.Program
	model          *Model
	ProgressWriter io.Writer // tool progress lines
	StreamWriter   io.Writer // assistant content stream
	ThinkingWriter io.Writer // model thinking stream
	SystemWriter   io.Writer // system/debate messages
	StatsWriter    io.Writer // stats lines
	onSubmit       func(string)
	setModel       func(string)
}

// NewApp creates the TUI app. The program is not started until Start is called.
func NewApp(modelName, workspace string) *App {
	m := NewModel(modelName, workspace, nil)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())

	a := &App{
		program:        p,
		model:          m,
		ProgressWriter: &bridgeWriter{p: p, kind: "tool"},
		StreamWriter:   &bridgeWriter{p: p, kind: "assistant"},
		ThinkingWriter: &bridgeWriter{p: p, kind: "thinking"},
		SystemWriter:   &bridgeWriter{p: p, kind: "system"},
		StatsWriter:    &bridgeWriter{p: p, kind: "stats"},
	}
	m.onSubmit = func(prompt string) {
		if a.onSubmit != nil {
			a.onSubmit(prompt)
		}
	}
	return a
}

// SetOnSubmit registers the handler invoked for each submitted prompt.
func (a *App) SetOnSubmit(handler func(string)) { a.onSubmit = handler }

func (a *App) SetModelName(name string)       { a.model.SetModelName(name) }
func (a *App) SetMode(mode string)            { a.model.SetMode(mode) }
func (a *App) CurrentMode() string            { return a.model.Mode() }
func (a *App) SetRecentModels(names []string) { a.model.SetRecentModels(names) }

// SetOnCommand registers a live help callback for commands as they are typed.
func (a *App) SetOnCommand(fn func(string) string) {
	a.model.SetOnCommand(fn)
}

// ShowPicker opens an interactive selection menu.
func (a *App) ShowPicker(title string, options []string, onSelect func(int)) {
	a.model.ShowPicker(title, options, onSelect)
}
func (a *App) IsPickerOpen() bool { return a.model.IsPickerOpen() }

// AskText opens a single-line text input for API keys, URLs, etc.
func (a *App) AskText(title, hint string, masked bool, onSubmit func(string)) {
	a.model.AskText(title, hint, masked, onSubmit)
}

// SetOnCancel registers the callback fired when the user presses Esc three
// times during execution.
func (a *App) SetOnCancel(fn func()) {
	a.model.SetOnCancel(fn)
}

// AddSystemMessage appends a system message directly to the model before or
// during program execution. Safe to call before Start since it does not rely
// on the tea event loop.
func (a *App) AddSystemMessage(text string) {
	a.model.addMessage(tuiMessage{kind: "system", data: text})
}

// Start runs the TUI program, blocking until the user exits.
func (a *App) Start() error {
	_, err := a.program.Run()
	return err
}

// UpdateStats refreshes the session statistics footer. Safe from any goroutine.
func (a *App) UpdateStats(turns int, promptTok, genTok int, totalMillis int64) {
	a.model.sendStats(turns, promptTok, genTok, totalMillis)
}

// SetBusy toggles the busy indicator in the footer. Safe from any goroutine.
func (a *App) SetBusy(busy bool, label string) {
	a.model.sendBusy(busy, label)
}

// Quit stops the TUI program.
func (a *App) Quit() {
	a.program.Send(tea.Quit())
}
