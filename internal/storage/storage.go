// Package storage persists jobs, the library and settings in SQLite.
package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"ytgrabber/internal/model"
)

type DB struct {
	conn *sql.DB
}

// MigrationEnv carries what migrations need to know about the outside
// world, e.g. where old releases stored files referenced by bare names.
type MigrationEnv struct {
	LegacyDownloadsDir string
}

// migrations are applied in order; PRAGMA user_version records how many
// have run. Never edit a released migration — append a new one.
var migrations = []func(tx *sql.Tx, env MigrationEnv) error{
	migrateInitialSchema,
	migrateAbsolutePaths,
}

// Open opens the SQLite file with a single connection (so writes
// from download goroutines serialise instead of failing with SQLITE_BUSY),
// WAL journaling and a busy timeout, then brings the schema up to date.
func Open(path string, env MigrationEnv) (*DB, error) {
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	conn.SetMaxOpenConns(1)

	if err := migrate(conn, env); err != nil {
		conn.Close()
		return nil, err
	}

	return &DB{conn: conn}, nil
}

func migrate(conn *sql.DB, env MigrationEnv) error {
	var version int
	if err := conn.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > len(migrations) {
		return fmt.Errorf("база данных создана более новой версией приложения (схема v%d, поддерживается до v%d)", version, len(migrations))
	}

	for i := version; i < len(migrations); i++ {
		tx, err := conn.Begin()
		if err != nil {
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if err := migrations[i](tx, env); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
	}
	return nil
}

// migrateInitialSchema is the schema the first release created without
// versioning. IF NOT EXISTS lets it run as a no-op on those databases.
func migrateInitialSchema(tx *sql.Tx, _ MigrationEnv) error {
	_, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS jobs (
	id TEXT PRIMARY KEY,
	url TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	quality TEXT NOT NULL DEFAULT '',
	file_path TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS videos (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL,
	filename TEXT NOT NULL,
	duration_seconds REAL NOT NULL DEFAULT 0,
	thumbnail_path TEXT,
	downloaded_at TEXT NOT NULL
);`)
	return err
}

// migrateAbsolutePaths switches the library from bare filenames (relative
// to one fixed downloads folder) to absolute paths, so the downloads folder
// can be changed without breaking existing entries. It also adds the media
// kind (instead of guessing from the extension), the source URL, job error
// messages and a key/value settings table. Duplicate rows pointing at the
// same file — possible before, when equal titles overwrote each other —
// collapse into the newest one.
func migrateAbsolutePaths(tx *sql.Tx, env MigrationEnv) error {
	if _, err := tx.Exec(`
ALTER TABLE jobs ADD COLUMN error TEXT NOT NULL DEFAULT '';

CREATE TABLE settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE videos_v2 (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL,
	source_url TEXT NOT NULL DEFAULT '',
	media_kind TEXT NOT NULL,
	file_path TEXT NOT NULL UNIQUE,
	thumbnail_path TEXT NOT NULL DEFAULT '',
	duration_seconds REAL NOT NULL DEFAULT 0,
	downloaded_at TEXT NOT NULL
);`); err != nil {
		return err
	}

	type legacyVideo struct {
		id, title, filename, downloadedAt string
		thumbnail                         sql.NullString
		duration                          float64
	}

	rows, err := tx.Query(`SELECT id, title, filename, duration_seconds, thumbnail_path, downloaded_at FROM videos ORDER BY downloaded_at DESC`)
	if err != nil {
		return err
	}
	var legacy []legacyVideo
	for rows.Next() {
		var v legacyVideo
		if err := rows.Scan(&v.id, &v.title, &v.filename, &v.duration, &v.thumbnail, &v.downloadedAt); err != nil {
			rows.Close()
			return err
		}
		legacy = append(legacy, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, v := range legacy {
		kind := model.MediaVideo
		if strings.EqualFold(filepath.Ext(v.filename), ".mp3") {
			kind = model.MediaAudio
		}
		thumbnail := ""
		if v.thumbnail.String != "" {
			thumbnail = filepath.Join(env.LegacyDownloadsDir, v.thumbnail.String)
		}
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO videos_v2 (id, title, media_kind, file_path, thumbnail_path, duration_seconds, downloaded_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			v.id, v.title, kind, filepath.Join(env.LegacyDownloadsDir, v.filename), thumbnail, v.duration, v.downloadedAt,
		); err != nil {
			return err
		}
	}

	_, err = tx.Exec(`
DROP TABLE videos;
ALTER TABLE videos_v2 RENAME TO videos;`)
	return err
}

func (d *DB) Close() error {
	return d.conn.Close()
}

// SaveJob upserts a job snapshot. created_at is only set on first insert —
// it is deliberately left out of the ON CONFLICT SET clause so it survives
// subsequent status updates.
func (d *DB) SaveJob(job *model.DownloadJob) error {
	createdAt := job.CreatedAt
	if createdAt == "" {
		createdAt = time.Now().UTC().Format(time.RFC3339Nano)
	}

	_, err := d.conn.Exec(
		`INSERT INTO jobs (id, url, title, status, quality, file_path, error, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
			url = excluded.url,
			title = excluded.title,
			status = excluded.status,
			quality = excluded.quality,
			file_path = excluded.file_path,
			error = excluded.error`,
		job.ID, job.URL, job.Title, job.Status, job.Quality, job.FilePath, job.Error, createdAt,
	)
	return err
}

func (d *DB) DeleteJob(id string) error {
	_, err := d.conn.Exec(`DELETE FROM jobs WHERE id = ?`, id)
	return err
}

// MarkInterruptedJobs turns jobs that were still active when the app last
// exited (or crashed) into retryable errors — nothing is running them
// anymore.
func (d *DB) MarkInterruptedJobs(message string) error {
	_, err := d.conn.Exec(
		`UPDATE jobs SET status = ?, error = ? WHERE status IN (?, ?, ?)`,
		model.StatusError, message, model.StatusPending, model.StatusDownloading, model.StatusProcessing,
	)
	return err
}

func (d *DB) LoadJobs() ([]*model.DownloadJob, error) {
	rows, err := d.conn.Query(`SELECT id, url, title, status, quality, file_path, error, created_at FROM jobs ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobs := make([]*model.DownloadJob, 0)
	for rows.Next() {
		job := &model.DownloadJob{Tracks: []model.DownloadTrack{}}
		if err := rows.Scan(&job.ID, &job.URL, &job.Title, &job.Status, &job.Quality, &job.FilePath, &job.Error, &job.CreatedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// SaveVideo inserts a library entry. Downloading the same file again (same
// video and format) refreshes the existing entry instead of duplicating it.
func (d *DB) SaveVideo(video *model.VideoRecord) error {
	_, err := d.conn.Exec(
		`INSERT INTO videos (id, title, source_url, media_kind, file_path, thumbnail_path, duration_seconds, downloaded_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(file_path) DO UPDATE SET
			title = excluded.title,
			source_url = excluded.source_url,
			thumbnail_path = excluded.thumbnail_path,
			duration_seconds = excluded.duration_seconds,
			downloaded_at = excluded.downloaded_at`,
		video.ID, video.Title, video.SourceURL, video.MediaKind, video.FilePath, video.ThumbnailPath, video.DurationSeconds, video.DownloadedAt,
	)
	return err
}

const videoColumns = `id, title, source_url, media_kind, file_path, thumbnail_path, duration_seconds, downloaded_at`

func scanVideo(scanner interface{ Scan(...any) error }) (*model.VideoRecord, error) {
	video := &model.VideoRecord{}
	err := scanner.Scan(&video.ID, &video.Title, &video.SourceURL, &video.MediaKind, &video.FilePath, &video.ThumbnailPath, &video.DurationSeconds, &video.DownloadedAt)
	return video, err
}

func (d *DB) LoadVideos() ([]*model.VideoRecord, error) {
	rows, err := d.conn.Query(`SELECT ` + videoColumns + ` FROM videos ORDER BY downloaded_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	videos := make([]*model.VideoRecord, 0)
	for rows.Next() {
		video, err := scanVideo(rows)
		if err != nil {
			return nil, err
		}
		videos = append(videos, video)
	}
	return videos, rows.Err()
}

var ErrVideoNotFound = errors.New("запись не найдена в библиотеке")

func (d *DB) GetVideo(id string) (*model.VideoRecord, error) {
	video, err := scanVideo(d.conn.QueryRow(`SELECT `+videoColumns+` FROM videos WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrVideoNotFound
	}
	return video, err
}

func (d *DB) DeleteVideo(id string) error {
	_, err := d.conn.Exec(`DELETE FROM videos WHERE id = ?`, id)
	return err
}

// IsFileReferenced reports whether any library entry uses path as its media
// file or thumbnail — the audio and video downloads of one source share a
// thumbnail, so deleting one must not remove the other's cover.
func (d *DB) IsFileReferenced(path string) (bool, error) {
	var count int
	err := d.conn.QueryRow(`SELECT COUNT(*) FROM videos WHERE file_path = ? OR thumbnail_path = ?`, path, path).Scan(&count)
	return count > 0, err
}

func (d *DB) LoadSettings() (map[string]string, error) {
	rows, err := d.conn.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		values[key] = value
	}
	return values, rows.Err()
}

func (d *DB) SetSetting(key string, value string) error {
	if value == "" {
		_, err := d.conn.Exec(`DELETE FROM settings WHERE key = ?`, key)
		return err
	}
	_, err := d.conn.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return err
}
