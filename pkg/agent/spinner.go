package agent

import (
	"fmt"
	"io"
	"sync"
	"time"
)

type Spinner struct {
	writer io.Writer
	label  string
	stop   chan struct{}
	done   chan struct{}
	once   sync.Once
}

func NewSpinner(writer io.Writer, label string) *Spinner {
	return &Spinner{writer: writer, label: label, stop: make(chan struct{}), done: make(chan struct{})}
}

func (s *Spinner) Start() {
	if s == nil || s.writer == nil {
		return
	}
	go func() {
		defer close(s.done)
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		index := 0
		for {
			select {
			case <-ticker.C:
				fmt.Fprintf(s.writer, "\r\033[K%s %s", frames[index%len(frames)], s.label)
				index++
			case <-s.stop:
				return
			}
		}
	}()
}

func (s *Spinner) Stop() {
	if s == nil || s.writer == nil {
		return
	}
	s.once.Do(func() {
		close(s.stop)
		<-s.done
		fmt.Fprint(s.writer, "\r\033[K")
	})
}
