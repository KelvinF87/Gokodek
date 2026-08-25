package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const tasksRel = ".gokodek/tasks.json"

// Task is a single checklist item in the project plan.
type Task struct {
	ID          int       `json:"id"`
	Description string    `json:"description"`
	Done        bool      `json:"done"`
	File        string    `json:"file,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// TaskStore persists the task checklist for a project.
type TaskStore struct {
	Path  string `json:"-"`
	Tasks []Task `json:"tasks"`
}

func LoadTasks(workspace string) (*TaskStore, error) {
	store := &TaskStore{Path: filepath.Join(workspace, tasksRel)}
	data, err := os.ReadFile(store.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, store); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *TaskStore) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.Path, data, 0600)
}

func (s *TaskStore) Add(description, file string) Task {
	nextID := 1
	for _, task := range s.Tasks {
		if task.ID >= nextID {
			nextID = task.ID + 1
		}
	}
	task := Task{ID: nextID, Description: description, File: file, CreatedAt: time.Now()}
	s.Tasks = append(s.Tasks, task)
	return task
}

func (s *TaskStore) SetDone(id int, done bool) (Task, bool) {
	for i := range s.Tasks {
		if s.Tasks[i].ID == id {
			s.Tasks[i].Done = done
			return s.Tasks[i], true
		}
	}
	return Task{}, false
}

func (s *TaskStore) Progress() (int, int) {
	done := 0
	for _, task := range s.Tasks {
		if task.Done {
			done++
		}
	}
	return done, len(s.Tasks)
}

func (s *TaskStore) Render() string {
	var sb strings.Builder
	done, total := s.Progress()
	sb.WriteString(fmt.Sprintf("Progreso del plan: %d/%d tareas completadas\n", done, total))
	if total > 0 {
		bar := progressBar(done, total, 20)
		sb.WriteString("  " + bar + "\n")
	}
	for _, task := range s.Tasks {
		mark := "[ ]"
		if task.Done {
			mark = "[x]"
		}
		line := fmt.Sprintf("  %s #%d %s", mark, task.ID, task.Description)
		if task.File != "" {
			line += fmt.Sprintf("  (%s)", task.File)
		}
		sb.WriteString(line + "\n")
	}
	if total == 0 {
		sb.WriteString("  (sin tareas; añade con add)\n")
	}
	return sb.String()
}

func progressBar(done, total, width int) string {
	if total <= 0 {
		return ""
	}
	filled := int(float64(done) / float64(total) * float64(width))
	if done > 0 && filled == 0 {
		filled = 1
	}
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}

// TasksTool lets the agent organize work into a checklist and mark progress.
type TasksTool struct {
	workspace string
}

func NewTasksTool(workspace string) *TasksTool {
	return &TasksTool{workspace: workspace}
}

func (t *TasksTool) Name() string { return "tasks" }
func (t *TasksTool) Description() string {
	return "Manage the project task checklist: add tasks, mark done, and show progress. Use it to plan and track the project until completion."
}
func (t *TasksTool) Parameters() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"action":      map[string]interface{}{"type": "string", "description": "add, done, pending, list, clear, or plan"},
		"description": map[string]interface{}{"type": "string", "description": "Task description (for action=add or plan lines)"},
		"file":        map[string]interface{}{"type": "string", "description": "Optional file the task relates to"},
		"id":          map[string]interface{}{"type": "integer", "description": "Task id (for action=done or pending)"},
	})
}

func (t *TasksTool) Execute(argsJSON string) (string, error) {
	var args struct {
		Action      string `json:"action"`
		Description string `json:"description"`
		File        string `json:"file"`
		ID          int    `json:"id"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("tasks arguments: %w", err)
	}
	store, err := LoadTasks(t.workspace)
	if err != nil {
		return "", err
	}
	action := strings.ToLower(strings.TrimSpace(args.Action))
	if action == "" {
		action = "list"
	}
	switch action {
	case "add":
		if strings.TrimSpace(args.Description) == "" {
			return "", fmt.Errorf("tasks add requires a description")
		}
		task := store.Add(args.Description, args.File)
		if err := store.Save(); err != nil {
			return "", err
		}
		return fmt.Sprintf("Tarea #%d añadida: %s\n%s", task.ID, task.Description, store.Render()), nil
	case "done":
		if args.ID <= 0 {
			return "", fmt.Errorf("tasks done requires an id")
		}
		task, ok := store.SetDone(args.ID, true)
		if !ok {
			return "", fmt.Errorf("tarea #%d no encontrada", args.ID)
		}
		if err := store.Save(); err != nil {
			return "", err
		}
		return fmt.Sprintf("Tarea #%d completada ✓: %s\n%s", task.ID, task.Description, store.Render()), nil
	case "pending":
		if args.ID <= 0 {
			return "", fmt.Errorf("tasks pending requires an id")
		}
		task, ok := store.SetDone(args.ID, false)
		if !ok {
			return "", fmt.Errorf("tarea #%d no encontrada", args.ID)
		}
		if err := store.Save(); err != nil {
			return "", err
		}
		return fmt.Sprintf("Tarea #%d marcada como pendiente: %s\n%s", task.ID, task.Description, store.Render()), nil
	case "plan":
		// Split a plan text into individual tasks (one per line or per dash).
		added := 0
		for _, line := range strings.Split(args.Description, "\n") {
			line = strings.TrimSpace(line)
			line = strings.TrimPrefix(line, "-")
			line = strings.TrimPrefix(line, "*")
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			store.Add(line, args.File)
			added++
		}
		if added == 0 {
			return "", fmt.Errorf("tasks plan requires a description with tasks")
		}
		if err := store.Save(); err != nil {
			return "", err
		}
		return fmt.Sprintf("Plan creado: %d tareas añadidas\n%s", added, store.Render()), nil
	case "list", "show", "status":
		return store.Render(), nil
	case "clear":
		store.Tasks = nil
		if err := store.Save(); err != nil {
			return "", err
		}
		return "Lista de tareas vaciada.", nil
	default:
		return "", fmt.Errorf("unknown tasks action %q; use add, done, pending, list, plan, or clear", action)
	}
}
