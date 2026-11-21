package storage

import (
	"database/sql"
	"errors"
	"time"

	"ytgrabber/internal/model"
)

// migrateShares adds links to single videos (model.Share).
func migrateShares(tx *sql.Tx, _ MigrationEnv) error {
	_, err := tx.Exec(`
CREATE TABLE shares (
	id TEXT PRIMARY KEY,
	token_hash TEXT NOT NULL UNIQUE,
	user_id TEXT NOT NULL,
	video_id TEXT NOT NULL,
	code_hash TEXT NOT NULL DEFAULT '',
	allow_download INTEGER NOT NULL DEFAULT 0,
	views INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	expires_at INTEGER NOT NULL
);
CREATE INDEX shares_by_video ON shares(video_id);
CREATE INDEX shares_by_user ON shares(user_id);`)
	return err
}

// migrateShareTokens keeps a link's token and code, so sharing a video
// again shows the same link. Links made before can't be shown again:
// only the token's hash was kept.
func migrateShareTokens(tx *sql.Tx, _ MigrationEnv) error {
	_, err := tx.Exec(`
ALTER TABLE shares ADD COLUMN token TEXT NOT NULL DEFAULT '';
ALTER TABLE shares ADD COLUMN code TEXT NOT NULL DEFAULT '';`)
	return err
}

var ErrShareNotFound = errors.New("ссылка не найдена или истекла")

const shareColumns = `s.id, s.user_id, s.video_id, v.title, s.token, s.code, s.code_hash, s.allow_download, s.views, s.created_at, s.expires_at`

func scanShare(scanner interface{ Scan(...any) error }) (*model.Share, error) {
	share := &model.Share{}
	var expires int64
	err := scanner.Scan(&share.ID, &share.UserID, &share.VideoID, &share.Title, &share.Token, &share.Code, &share.CodeHash,
		&share.AllowDownload, &share.Views, &share.CreatedAt, &expires)
	share.HasCode = share.CodeHash != ""
	share.ExpiresAt = time.Unix(expires, 0).UTC().Format(time.RFC3339)
	return share, err
}

// CreateShare saves a link known by the token whose hash is tokenHash.
func (d *DB) CreateShare(share *model.Share, tokenHash string, expiresAt time.Time) error {
	if share.UserID == "" {
		return errNoOwner
	}
	_, err := d.conn.Exec(
		`INSERT INTO shares (id, token_hash, token, code, user_id, video_id, code_hash, allow_download, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		share.ID, tokenHash, share.Token, share.Code, share.UserID, share.VideoID, share.CodeHash, share.AllowDownload, share.CreatedAt, expiresAt.Unix())
	return err
}

// ShareByToken finds the link whose token hashes to tokenHash, unless it
// has expired or its video is gone.
func (d *DB) ShareByToken(tokenHash string, now time.Time) (*model.Share, error) {
	share, err := scanShare(d.conn.QueryRow(
		`SELECT `+shareColumns+` FROM shares s JOIN videos v ON v.id = s.video_id AND v.user_id = s.user_id
		 WHERE s.token_hash = ? AND s.expires_at > ?`, tokenHash, now.Unix()))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrShareNotFound
	}
	return share, err
}

// ShareForVideo is the link that works for user's video id, the newest
// if there are several (made before a video had at most one).
func (d *DB) ShareForVideo(user string, videoID string, now time.Time) (*model.Share, error) {
	share, err := scanShare(d.conn.QueryRow(
		`SELECT `+shareColumns+` FROM shares s JOIN videos v ON v.id = s.video_id AND v.user_id = s.user_id
		 WHERE s.user_id = ? AND s.video_id = ? AND s.expires_at > ? ORDER BY s.created_at DESC LIMIT 1`,
		user, videoID, now.Unix()))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrShareNotFound
	}
	return share, err
}

// ListShares returns user's links that still work, newest first; expired
// ones are cleared on the way.
func (d *DB) ListShares(user string, now time.Time) ([]*model.Share, error) {
	if _, err := d.conn.Exec(`DELETE FROM shares WHERE expires_at <= ?`, now.Unix()); err != nil {
		return nil, err
	}
	rows, err := d.conn.Query(
		`SELECT `+shareColumns+` FROM shares s JOIN videos v ON v.id = s.video_id AND v.user_id = s.user_id
		 WHERE s.user_id = ? ORDER BY s.created_at DESC`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	shares := make([]*model.Share, 0)
	for rows.Next() {
		share, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		shares = append(shares, share)
	}
	return shares, rows.Err()
}

// DeleteShare revokes user's link id; another user's is ErrShareNotFound.
func (d *DB) DeleteShare(user string, id string) error {
	result, err := d.conn.Exec(`DELETE FROM shares WHERE id = ? AND user_id = ?`, id, user)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrShareNotFound
	}
	return nil
}

// CountShareView records that the link's page was opened.
func (d *DB) CountShareView(id string) error {
	_, err := d.conn.Exec(`UPDATE shares SET views = views + 1 WHERE id = ?`, id)
	return err
}
