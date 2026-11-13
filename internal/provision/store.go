package provision

import (
	"context"
	"database/sql"

	"pier/internal/platform/sqlite"
)

type store struct{ db *sql.DB }

const credentialColumns = `id, provider, name, key_id, created_at`

func scanCredential(row interface{ Scan(...any) error }) (Credential, error) {
	var credential Credential
	var created string
	err := row.Scan(&credential.ID, &credential.Provider, &credential.Name, &credential.KeyID, &created)
	credential.CreatedAt = sqlite.ParseTime(created)
	return credential, err
}

func (s store) insertCredential(ctx context.Context, credential Credential, sealedToken string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO cloud_credentials (id, provider, name, token, key_id, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		credential.ID, credential.Provider, credential.Name, sealedToken, credential.KeyID, sqlite.FormatTime(credential.CreatedAt))
	return err
}

func (s store) credentials(ctx context.Context) ([]Credential, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+credentialColumns+` FROM cloud_credentials ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Credential{}
	for rows.Next() {
		credential, err := scanCredential(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, credential)
	}
	return list, rows.Err()
}

func (s store) credential(ctx context.Context, id string) (Credential, string, error) {
	var sealed string
	row := s.db.QueryRowContext(ctx, `SELECT `+credentialColumns+`, token FROM cloud_credentials WHERE id = ?`, id)
	var credential Credential
	var created string
	err := row.Scan(&credential.ID, &credential.Provider, &credential.Name, &credential.KeyID, &created, &sealed)
	credential.CreatedAt = sqlite.ParseTime(created)
	return credential, sealed, sqlite.NotFoundIfNoRows(err)
}

func (s store) setCredentialKey(ctx context.Context, id, keyID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE cloud_credentials SET key_id = ? WHERE id = ?`, keyID, id)
	return err
}

func (s store) deleteCredential(ctx context.Context, id string) error {
	return sqlite.RequireAffected(s.db.ExecContext(ctx, `DELETE FROM cloud_credentials WHERE id = ?`, id))
}

func (s store) countMachines(ctx context.Context, credentialID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cloud_machines WHERE credential_id = ?`, credentialID).Scan(&count)
	return count, err
}

const machineColumns = `m.id, m.credential_id, c.provider, m.name, m.region, m.size, m.image, m.install_docker,
	m.external_id, m.ipv4, COALESCE(m.server_id, ''), m.status, m.error, m.created_at`

func scanMachine(row interface{ Scan(...any) error }) (Machine, error) {
	var machine Machine
	var created string
	err := row.Scan(&machine.ID, &machine.CredentialID, &machine.Provider, &machine.Name, &machine.Region, &machine.Size,
		&machine.Image, &machine.InstallDocker, &machine.ExternalID, &machine.IPv4, &machine.ServerID, &machine.Status,
		&machine.Error, &created)
	machine.CreatedAt = sqlite.ParseTime(created)
	return machine, err
}

func (s store) insertMachine(ctx context.Context, machine Machine) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO cloud_machines (id, credential_id, name, region, size, image, install_docker, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, machine.ID, machine.CredentialID, machine.Name, machine.Region, machine.Size,
		machine.Image, machine.InstallDocker, machine.Status, sqlite.FormatTime(machine.CreatedAt))
	return err
}

func (s store) machines(ctx context.Context) ([]Machine, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+machineColumns+` FROM cloud_machines m
		JOIN cloud_credentials c ON c.id = m.credential_id ORDER BY m.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Machine{}
	for rows.Next() {
		machine, err := scanMachine(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, machine)
	}
	return list, rows.Err()
}

func (s store) machine(ctx context.Context, id string) (Machine, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+machineColumns+` FROM cloud_machines m
		JOIN cloud_credentials c ON c.id = m.credential_id WHERE m.id = ?`, id)
	machine, err := scanMachine(row)
	return machine, sqlite.NotFoundIfNoRows(err)
}

func (s store) machineNameTaken(ctx context.Context, name string) (bool, error) {
	var taken bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM cloud_machines WHERE name = ?)`, name).Scan(&taken)
	return taken, err
}

// progress records how far provisioning got.
type progress struct {
	status, externalID, ipv4, serverID, failure string
}

func (s store) setProgress(ctx context.Context, id string, p progress) error {
	_, err := s.db.ExecContext(ctx, `UPDATE cloud_machines SET status = ?,
		external_id = CASE WHEN ? = '' THEN external_id ELSE ? END,
		ipv4 = CASE WHEN ? = '' THEN ipv4 ELSE ? END,
		server_id = CASE WHEN ? = '' THEN server_id ELSE ? END,
		error = ? WHERE id = ?`,
		p.status, p.externalID, p.externalID, p.ipv4, p.ipv4, p.serverID, p.serverID, p.failure, id)
	return err
}

func (s store) deleteMachine(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM cloud_machines WHERE id = ?`, id)
	return err
}

// interrupt marks machines whose provisioning a restart cut short.
func (s store) interrupt(ctx context.Context, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE cloud_machines SET status = ?, error = ? WHERE status IN (?, ?, ?)`,
		StatusFailed, message, StatusCreating, StatusWaitingSSH, StatusInstalling)
	return err
}
