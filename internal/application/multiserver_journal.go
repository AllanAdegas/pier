package application

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"pier/internal/platform/i18n"
	"pier/internal/platform/sqlite"
)

// Rollout journal states. preparing changes no server; releasing and
// compensating do, so a restart in them leaves the rollout interrupted.
const (
	RolloutPreparing    = "preparing"
	RolloutReleasing    = "releasing"
	RolloutCompensating = "compensating"
	RolloutInterrupted  = "interrupted"
	RolloutCompleted    = "completed"
	RolloutFailed       = "failed"
	RolloutRolledBack   = "rolled_back"
)

// States of one server inside a rollout.
const (
	StepPending       = "pending"
	StepReleasing     = "releasing"
	StepReleased      = "released"
	StepFailed        = "failed"
	StepRestored      = "restored"
	StepRestoreFailed = "restore_failed"
)

// RolloutJournal is the durable record of the last multi-server rollout of
// an application: what it releases, where, and how far it got.
type RolloutJournal struct {
	ApplicationID       string        `json:"application_id"`
	DeploymentID        string        `json:"deployment_id"`
	Image               string        `json:"image"`
	State               string        `json:"state"`
	InterruptedIn       string        `json:"interrupted_in"`
	ActionDeploymentID  string        `json:"action_deployment_id"`
	AutomaticRecoveries int           `json:"automatic_recoveries"`
	Message             string        `json:"message"`
	StartedAt           time.Time     `json:"started_at"`
	UpdatedAt           time.Time     `json:"updated_at"`
	Servers             []RolloutStep `json:"servers"`
}

// RolloutStep is the progress of the rollout on one server.
type RolloutStep struct {
	ServerID      string    `json:"server_id"`
	Position      int       `json:"position"`
	PreviousImage string    `json:"previous_image"`
	State         string    `json:"state"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// serverIDs are the servers of the rollout in release order.
func (j RolloutJournal) serverIDs() []string {
	ids := make([]string, len(j.Servers))
	for index, step := range j.Servers {
		ids[index] = step.ServerID
	}
	return ids
}

// rolloutJournal persists RolloutJournal rows. Writes that precede a change
// on a server must succeed; the rollout stops otherwise.
type rolloutJournal struct{ db *sql.DB }

// begin replaces the journal of the application with a new rollout whose
// servers are all pending.
func (s rolloutJournal) begin(ctx context.Context, appID, deploymentID string, servers []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := sqlite.FormatTime(time.Now())
	if _, err := tx.ExecContext(ctx, `DELETE FROM placement_rollout_servers WHERE application_id = ?`, appID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM placement_rollouts WHERE application_id = ?`, appID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO placement_rollouts (application_id, deployment_id, state, started_at, updated_at)
		VALUES (?, ?, ?, ?, ?)`, appID, deploymentID, RolloutPreparing, now, now); err != nil {
		return err
	}
	for position, serverID := range servers {
		if _, err := tx.ExecContext(ctx, `INSERT INTO placement_rollout_servers (application_id, server_id, position, state, updated_at)
			VALUES (?, ?, ?, ?, ?)`, appID, serverID, position, StepPending, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s rolloutJournal) setImage(ctx context.Context, appID, image string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE placement_rollouts SET image = ?, updated_at = ? WHERE application_id = ?`,
		image, sqlite.FormatTime(time.Now()), appID)
	return err
}

func (s rolloutJournal) setState(ctx context.Context, appID, state, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE placement_rollouts SET state = ?, message = ?, updated_at = ? WHERE application_id = ?`,
		state, truncateMessage(message), sqlite.FormatTime(time.Now()), appID)
	return err
}

// interrupt leaves a rollout for the operator (or the next startup) to
// resume or roll back, remembering the phase it was in.
func (s rolloutJournal) interrupt(ctx context.Context, appID, phase, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE placement_rollouts SET state = ?, interrupted_in = ?, message = ?, updated_at = ?
		WHERE application_id = ?`, RolloutInterrupted, phase, truncateMessage(message), sqlite.FormatTime(time.Now()), appID)
	return err
}

// step records the state of one server; previous is stored when not nil.
func (s rolloutJournal) step(ctx context.Context, appID, serverID, state string, previous *string) error {
	now := sqlite.FormatTime(time.Now())
	if previous != nil {
		_, err := s.db.ExecContext(ctx, `UPDATE placement_rollout_servers SET state = ?, previous_image = ?, updated_at = ?
			WHERE application_id = ? AND server_id = ?`, state, *previous, now, appID, serverID)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE placement_rollout_servers SET state = ?, updated_at = ?
		WHERE application_id = ? AND server_id = ?`, state, now, appID, serverID)
	return err
}

// claim points the interrupted rollout at the action deployment that will
// resume or roll it back; automatic claims are counted.
func (s rolloutJournal) claim(ctx context.Context, appID, deploymentID string, automatic bool) error {
	increment := 0
	if automatic {
		increment = 1
	}
	_, err := s.db.ExecContext(ctx, `UPDATE placement_rollouts SET action_deployment_id = ?,
		automatic_recoveries = automatic_recoveries + ?, updated_at = ? WHERE application_id = ?`,
		deploymentID, increment, sqlite.FormatTime(time.Now()), appID)
	return err
}

// noteAutomatic records an automatic action that was refused (a frozen
// environment, a server in maintenance): it is not tried again.
func (s rolloutJournal) noteAutomatic(ctx context.Context, appID, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE placement_rollouts SET automatic_recoveries = automatic_recoveries + 1,
		message = ?, updated_at = ? WHERE application_id = ?`, truncateMessage(message), sqlite.FormatTime(time.Now()), appID)
	return err
}

// load returns the journal of an application; found is false without one.
func (s rolloutJournal) load(ctx context.Context, appID string) (RolloutJournal, bool, error) {
	j := RolloutJournal{ApplicationID: appID, Servers: []RolloutStep{}}
	var started, updated string
	err := s.db.QueryRowContext(ctx, `SELECT deployment_id, image, state, interrupted_in, action_deployment_id,
		automatic_recoveries, message, started_at, updated_at FROM placement_rollouts WHERE application_id = ?`, appID).
		Scan(&j.DeploymentID, &j.Image, &j.State, &j.InterruptedIn, &j.ActionDeploymentID, &j.AutomaticRecoveries,
			&j.Message, &started, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return j, false, nil
	}
	if err != nil {
		return j, false, err
	}
	j.StartedAt, j.UpdatedAt = sqlite.ParseTime(started), sqlite.ParseTime(updated)
	rows, err := s.db.QueryContext(ctx, `SELECT server_id, position, previous_image, state, updated_at
		FROM placement_rollout_servers WHERE application_id = ? ORDER BY position`, appID)
	if err != nil {
		return j, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var step RolloutStep
		if err := rows.Scan(&step.ServerID, &step.Position, &step.PreviousImage, &step.State, &updated); err != nil {
			return j, false, err
		}
		step.UpdatedAt = sqlite.ParseTime(updated)
		j.Servers = append(j.Servers, step)
	}
	return j, true, rows.Err()
}

// interruptedApplications lists the applications whose rollout waits for a
// resume or a rollback.
func (s rolloutJournal) interruptedApplications(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT application_id FROM placement_rollouts WHERE state = ? ORDER BY started_at`,
		RolloutInterrupted)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// recoverAfterRestart classifies the rollouts a restart cut: one still
// preparing changed no server and simply failed; one releasing or
// compensating changed some and becomes interrupted.
func (s rolloutJournal) recoverAfterRestart(ctx context.Context) error {
	now := sqlite.FormatTime(time.Now())
	language := i18n.Instance()
	if _, err := s.db.ExecContext(ctx, `UPDATE placement_rollouts SET state = ?, message = ?, updated_at = ? WHERE state = ?`,
		RolloutFailed, language.Text("Interrompido pela reinicialização do Pier antes de mudar qualquer servidor."), now,
		RolloutPreparing); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE placement_rollouts SET interrupted_in = state, state = ?, message = ?, updated_at = ?
		WHERE state IN (?, ?)`, RolloutInterrupted, language.Text("Interrompido pela reinicialização do Pier."), now,
		RolloutReleasing, RolloutCompensating)
	return err
}
