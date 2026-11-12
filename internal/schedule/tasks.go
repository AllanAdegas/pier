package schedule

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"pier/internal/application"
	"pier/internal/docker"
	"pier/internal/notify"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/platform/idgen"
	"pier/internal/platform/sqlite"
	"pier/internal/server"
)

const (
	taskTimeout        = time.Hour
	maxTaskOutputBytes = 64 * 1024
	keptExecutions     = 30
)

// Task is a command run periodically inside the container of an application.
type Task struct {
	ID            string    `json:"id"`
	ApplicationID string    `json:"application_id"`
	Name          string    `json:"name"`
	Command       string    `json:"command"`
	Cron          string    `json:"cron"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
}

// TaskInput carries the editable fields of a task.
type TaskInput struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Cron    string `json:"cron"`
	Enabled bool   `json:"enabled"`
}

// TaskExecution is one run of a task.
type TaskExecution struct {
	ID         string     `json:"id"`
	TaskID     string     `json:"task_id"`
	Status     string     `json:"status"`
	Output     string     `json:"output"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

// Applications resolves the application a task belongs to.
type Applications interface {
	Get(ctx context.Context, id string) (application.Application, error)
	OutputSecrets(ctx context.Context, id string) ([]string, error)
}

// Connector reaches the server an application runs on.
type Connector interface {
	Executor(ctx context.Context, serverID string) (executor.Executor, server.Server, error)
}

// TaskService manages scheduled tasks and executes them.
type TaskService struct {
	db        *sql.DB
	apps      Applications
	connector Connector
	events    notify.Publisher
	onChange  func()
	mu        sync.Mutex
	running   map[string]bool
}

// NewTaskService creates the service.
func NewTaskService(db *sql.DB, apps Applications, connector Connector, events notify.Publisher) *TaskService {
	return &TaskService{db: db, apps: apps, connector: connector, events: events, onChange: func() {}, running: make(map[string]bool)}
}

// OnChange registers the callback fired when the set of tasks changes.
func (s *TaskService) OnChange(callback func()) { s.onChange = callback }

// Jobs is the scheduler Source for enabled tasks.
func (s *TaskService) Jobs(ctx context.Context) ([]Job, error) {
	tasks, err := s.list(ctx, `WHERE enabled = 1`)
	if err != nil {
		return nil, err
	}
	jobs := make([]Job, len(tasks))
	for i, task := range tasks {
		jobs[i] = Job{Name: "task:" + task.ID, Cron: task.Cron, Run: func() { _, _ = s.Run(context.Background(), task.ID) }}
	}
	return jobs, nil
}

// List returns the tasks of an application.
func (s *TaskService) List(ctx context.Context, applicationID string) ([]Task, error) {
	return s.list(ctx, `WHERE application_id = ?`, applicationID)
}

// Create registers a task.
func (s *TaskService) Create(ctx context.Context, applicationID string, input TaskInput) (Task, error) {
	if _, err := s.apps.Get(ctx, applicationID); err != nil {
		return Task{}, err
	}
	task := Task{ID: idgen.New(), ApplicationID: applicationID, CreatedAt: time.Now()}
	if err := applyTaskInput(&task, input); err != nil {
		return Task{}, err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO scheduled_tasks
		(id, application_id, name, command, cron, enabled, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.ApplicationID, task.Name, task.Command, task.Cron, task.Enabled, sqlite.FormatTime(task.CreatedAt))
	if err != nil {
		return Task{}, err
	}
	s.onChange()
	return task, nil
}

// Update changes a task.
func (s *TaskService) Update(ctx context.Context, id string, input TaskInput) (Task, error) {
	task, err := s.get(ctx, id)
	if err != nil {
		return Task{}, err
	}
	if err := applyTaskInput(&task, input); err != nil {
		return Task{}, err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE scheduled_tasks SET name = ?, command = ?, cron = ?, enabled = ? WHERE id = ?`,
		task.Name, task.Command, task.Cron, task.Enabled, id)
	if err != nil {
		return Task{}, err
	}
	s.onChange()
	return task, nil
}

// Delete removes a task and its history.
func (s *TaskService) Delete(ctx context.Context, id string) error {
	err := sqlite.RequireAffected(s.db.ExecContext(ctx, `DELETE FROM scheduled_tasks WHERE id = ?`, id))
	if errors.Is(err, sqlite.ErrNotFound) {
		return apperr.NotFound("Tarefa agendada")
	}
	s.onChange()
	return err
}

// Executions returns the latest runs of a task.
func (s *TaskService) Executions(ctx context.Context, taskID string, raw ...bool) ([]TaskExecution, error) {
	allowRaw := len(raw) > 0 && raw[0]
	var redactor application.OutputRedactor
	if !allowRaw {
		task, err := s.get(ctx, taskID)
		if err != nil {
			return nil, err
		}
		values, err := s.apps.OutputSecrets(ctx, task.ApplicationID)
		if err != nil {
			return nil, err
		}
		redactor.Redact(values...)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, task_id, status, output, output_masked, started_at, finished_at
		FROM task_executions WHERE task_id = ? ORDER BY started_at DESC LIMIT ?`, taskID, keptExecutions)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	executions := []TaskExecution{}
	for rows.Next() {
		var execution TaskExecution
		var startedAt string
		var outputMasked bool
		var finishedAt sql.NullString
		if err := rows.Scan(&execution.ID, &execution.TaskID, &execution.Status, &execution.Output, &outputMasked,
			&startedAt, &finishedAt); err != nil {
			return nil, err
		}
		if !allowRaw && !outputMasked && execution.Output != "" {
			execution.Output = "••••••"
		}
		execution.Output = redactor.Mask(execution.Output)
		execution.StartedAt = sqlite.ParseTime(startedAt)
		execution.FinishedAt = sqlite.ParseOptionalTime(finishedAt)
		executions = append(executions, execution)
	}
	return executions, rows.Err()
}

// Run executes a task now and records the outcome.
func (s *TaskService) Run(ctx context.Context, taskID string) (TaskExecution, error) {
	s.mu.Lock()
	if s.running[taskID] {
		s.mu.Unlock()
		return TaskExecution{}, apperr.Conflict("Esta tarefa já está em execução. Aguarde a conclusão.")
	}
	s.running[taskID] = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.running, taskID); s.mu.Unlock() }()
	task, err := s.get(ctx, taskID)
	if err != nil {
		return TaskExecution{}, err
	}
	values, err := s.apps.OutputSecrets(ctx, task.ApplicationID)
	if err != nil {
		return TaskExecution{}, err
	}
	var redactor application.OutputRedactor
	redactor.Redact(values...)
	execution := TaskExecution{ID: idgen.New(), TaskID: taskID, Status: "running", StartedAt: time.Now()}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO task_executions (id, task_id, status, started_at, output_masked) VALUES (?, ?, ?, ?, 1)`,
		execution.ID, taskID, execution.Status, sqlite.FormatTime(execution.StartedAt)); err != nil {
		return TaskExecution{}, err
	}
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), taskTimeout)
	defer cancel()
	output, runErr := s.execute(runCtx, task, &redactor)
	finishedAt := time.Now()
	execution.Status, execution.Output, execution.FinishedAt = "succeeded", output, &finishedAt
	if runErr != nil {
		execution.Status = "failed"
		execution.Output = redactor.Mask(strings.TrimSpace(output + "\n" + i18n.Localize(runErr, i18n.Instance())))
		s.events.Publish(notify.Event{
			Type: notify.TaskFailed, Resource: task.Name,
			Title:   i18n.Instance().Sprintf("Tarefa agendada falhou: %s", task.Name),
			Message: tailBytes(execution.Output, 1500),
		})
	}
	persistCtx, stopPersist := context.WithTimeout(context.WithoutCancel(runCtx), 5*time.Second)
	defer stopPersist()
	_, err = s.db.ExecContext(persistCtx, `UPDATE task_executions SET status = ?, output = ?, finished_at = ? WHERE id = ?`,
		execution.Status, execution.Output, sqlite.FormatTime(finishedAt), execution.ID)
	s.trimHistory(persistCtx, taskID)
	return execution, err
}

func (s *TaskService) execute(ctx context.Context, task Task, redactor *application.OutputRedactor) (string, error) {
	app, err := s.apps.Get(ctx, task.ApplicationID)
	if err != nil {
		return "", err
	}
	ex, _, err := s.connector.Executor(ctx, app.ServerID)
	if err != nil {
		return "", err
	}
	containers, err := docker.ListByResource(ctx, ex, app.ID)
	if err != nil {
		return "", err
	}
	for _, container := range containers {
		if container.State != "running" {
			continue
		}
		values, err := docker.Inspect(ctx, ex, container.ID, "{{json .Config.Env}}")
		if err != nil {
			return "", err
		}
		var environment []string
		if err := json.Unmarshal([]byte(values), &environment); err != nil {
			return "", i18n.Errorf("não foi possível ler o ambiente do container para mascarar a saída da tarefa")
		}
		for _, entry := range environment {
			if _, value, ok := strings.Cut(entry, "="); ok {
				redactor.Redact(value)
			}
		}
		var output strings.Builder
		err = ex.Stream(ctx, docker.Exec(container.ID, task.Command), func(line string) {
			line = redactor.Mask(line) + "\n"
			remaining := maxTaskOutputBytes - output.Len()
			if remaining > 0 {
				if len(line) > remaining {
					line = line[:remaining]
				}
				output.WriteString(line)
			}
		})
		return strings.TrimSpace(output.String()), err
	}
	return "", i18n.Errorf("a aplicação não tem container em execução")
}

func (s *TaskService) trimHistory(ctx context.Context, taskID string) {
	_, _ = s.db.ExecContext(ctx, `DELETE FROM task_executions WHERE task_id = ? AND id NOT IN (
		SELECT id FROM task_executions WHERE task_id = ? ORDER BY started_at DESC LIMIT ?)`,
		taskID, taskID, keptExecutions)
}

func (s *TaskService) get(ctx context.Context, id string) (Task, error) {
	tasks, err := s.list(ctx, `WHERE id = ?`, id)
	if err != nil {
		return Task{}, err
	}
	if len(tasks) == 0 {
		return Task{}, apperr.NotFound("Tarefa agendada")
	}
	return tasks[0], nil
}

func (s *TaskService) list(ctx context.Context, where string, args ...any) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, application_id, name, command, cron, enabled, created_at
		FROM scheduled_tasks `+where+` ORDER BY created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := []Task{}
	for rows.Next() {
		var task Task
		var createdAt string
		if err := rows.Scan(&task.ID, &task.ApplicationID, &task.Name, &task.Command, &task.Cron,
			&task.Enabled, &createdAt); err != nil {
			return nil, err
		}
		task.CreatedAt = sqlite.ParseTime(createdAt)
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func applyTaskInput(task *Task, input TaskInput) error {
	fields := apperr.Fields{}
	task.Name = strings.TrimSpace(input.Name)
	task.Command = strings.TrimSpace(input.Command)
	task.Cron = strings.TrimSpace(input.Cron)
	task.Enabled = input.Enabled
	if task.Name == "" {
		fields.Add("name", "Informe o nome da tarefa.")
	}
	if task.Command == "" {
		fields.Add("command", "Informe o comando a executar.")
	}
	if _, err := cron.ParseStandard(task.Cron); err != nil {
		fields.Add("cron", "Expressão cron inválida (use 5 campos, ex.: */15 * * * *).")
	}
	return fields.Err()
}

func tailBytes(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return "…" + text[len(text)-limit:]
}
