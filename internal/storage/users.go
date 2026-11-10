package storage

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"ytgrabber/internal/model"
)

// migrateUserDevices adds the server's users, their devices and the
// one-time codes (invites, device links) that create them. Tokens and
// codes are stored only as hashes.
func migrateUserDevices(tx *sql.Tx, _ MigrationEnv) error {
	_, err := tx.Exec(`
CREATE TABLE users (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	-- name_key is the lower-cased name: SQLite's NOCASE folds only ASCII.
	name_key TEXT NOT NULL UNIQUE,
	role TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE TABLE devices (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	name TEXT NOT NULL,
	token_hash TEXT NOT NULL UNIQUE,
	created_at TEXT NOT NULL,
	last_seen_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX devices_by_user ON devices(user_id);
CREATE TABLE codes (
	hash TEXT PRIMARY KEY,
	kind TEXT NOT NULL,
	user_id TEXT NOT NULL DEFAULT '',
	role TEXT NOT NULL DEFAULT '',
	created_by TEXT NOT NULL,
	expires_at INTEGER NOT NULL
);`)
	return err
}

var (
	ErrUserNotFound   = errors.New("пользователь не найден")
	ErrNameTaken      = errors.New("это имя уже занято")
	ErrDeviceNotFound = errors.New("устройство не найдено")
	ErrCodeNotFound   = errors.New("код не подходит или истёк")
)

const userColumns = `id, name, role, created_at`

func scanUser(scanner interface{ Scan(...any) error }) (*model.User, error) {
	user := &model.User{}
	err := scanner.Scan(&user.ID, &user.Name, &user.Role, &user.CreatedAt)
	return user, err
}

func (d *DB) CountUsers() (int, error) {
	var n int
	err := d.conn.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// CreateUser adds user; a name already taken (in any case) is ErrNameTaken.
func (d *DB) CreateUser(user *model.User) error {
	_, err := d.conn.Exec(`INSERT INTO users (`+userColumns+`, name_key) VALUES (?, ?, ?, ?, ?)`,
		user.ID, user.Name, user.Role, user.CreatedAt, nameKey(user.Name))
	if err != nil && isUniqueViolation(err) {
		return ErrNameTaken
	}
	return err
}

// CreateFirstUser adds user only while there are no users at all, so two
// setups racing each other can't both create an admin.
func (d *DB) CreateFirstUser(user *model.User) (bool, error) {
	result, err := d.conn.Exec(
		`INSERT INTO users (`+userColumns+`, name_key) SELECT ?, ?, ?, ?, ? WHERE NOT EXISTS (SELECT 1 FROM users)`,
		user.ID, user.Name, user.Role, user.CreatedAt, nameKey(user.Name))
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (d *DB) GetUser(id string) (*model.User, error) {
	user, err := scanUser(d.conn.QueryRow(`SELECT `+userColumns+` FROM users WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	return user, err
}

func (d *DB) ListUsers() ([]*model.User, error) {
	rows, err := d.conn.Query(`SELECT ` + userColumns + ` FROM users ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]*model.User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

// DeleteUser removes the user with their devices and the codes that would
// sign in as them. Their library, queue and accounts are the core's to
// delete.
func (d *DB) DeleteUser(id string) error {
	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrUserNotFound
	}
	if _, err := tx.Exec(`DELETE FROM devices WHERE user_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM codes WHERE user_id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

const deviceColumns = `id, user_id, name, created_at, last_seen_at`

func scanDevice(scanner interface{ Scan(...any) error }) (*model.Device, error) {
	device := &model.Device{}
	err := scanner.Scan(&device.ID, &device.UserID, &device.Name, &device.CreatedAt, &device.LastSeenAt)
	return device, err
}

// CreateDevice adds a device known by the token whose hash is tokenHash.
func (d *DB) CreateDevice(device *model.Device, tokenHash string) error {
	if device.UserID == "" {
		return errNoOwner
	}
	_, err := d.conn.Exec(`INSERT INTO devices (`+deviceColumns+`, token_hash) VALUES (?, ?, ?, ?, ?, ?)`,
		device.ID, device.UserID, device.Name, device.CreatedAt, device.LastSeenAt, tokenHash)
	return err
}

// DeviceByToken finds the device whose token hashes to tokenHash.
func (d *DB) DeviceByToken(tokenHash string) (*model.Device, error) {
	device, err := scanDevice(d.conn.QueryRow(`SELECT `+deviceColumns+` FROM devices WHERE token_hash = ?`, tokenHash))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDeviceNotFound
	}
	return device, err
}

func (d *DB) ListDevices(user string) ([]*model.Device, error) {
	rows, err := d.conn.Query(`SELECT `+deviceColumns+` FROM devices WHERE user_id = ? ORDER BY created_at ASC`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	devices := make([]*model.Device, 0)
	for rows.Next() {
		device, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	return devices, rows.Err()
}

// DeleteDevice revokes user's device id; another user's is
// ErrDeviceNotFound.
func (d *DB) DeleteDevice(user string, id string) error {
	result, err := d.conn.Exec(`DELETE FROM devices WHERE id = ? AND user_id = ?`, id, user)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrDeviceNotFound
	}
	return nil
}

func (d *DB) TouchDevice(id string, at string) error {
	_, err := d.conn.Exec(`UPDATE devices SET last_seen_at = ? WHERE id = ?`, at, id)
	return err
}

func (d *DB) SaveCode(code *model.Code) error {
	_, err := d.conn.Exec(`INSERT INTO codes (hash, kind, user_id, role, created_by, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		code.Hash, code.Kind, code.UserID, code.Role, code.CreatedBy, code.ExpiresAt.Unix())
	return err
}

// PeekCode returns the code that hashes to hash if it hasn't expired,
// leaving it usable.
func (d *DB) PeekCode(hash string, now time.Time) (*model.Code, error) {
	return scanCode(d.conn.QueryRow(`SELECT hash, kind, user_id, role, created_by, expires_at FROM codes WHERE hash = ? AND expires_at > ?`, hash, now.Unix()))
}

// TakeCode is PeekCode that also uses the code up: of two requests with
// the same code only one gets it. Expired codes are cleared on the way.
func (d *DB) TakeCode(hash string, now time.Time) (*model.Code, error) {
	if _, err := d.conn.Exec(`DELETE FROM codes WHERE expires_at <= ?`, now.Unix()); err != nil {
		return nil, err
	}
	return scanCode(d.conn.QueryRow(`DELETE FROM codes WHERE hash = ? RETURNING hash, kind, user_id, role, created_by, expires_at`, hash))
}

func scanCode(row *sql.Row) (*model.Code, error) {
	code := &model.Code{}
	var expires int64
	err := row.Scan(&code.Hash, &code.Kind, &code.UserID, &code.Role, &code.CreatedBy, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCodeNotFound
	}
	code.ExpiresAt = time.Unix(expires, 0)
	return code, err
}

// nameKey is what makes two names the same: "Анна" and "анна" are.
func nameKey(name string) string {
	return strings.ToLower(name)
}

func isUniqueViolation(err error) bool {
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
