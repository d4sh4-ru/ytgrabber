package main

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type sqliteDB struct {
	conn *sql.DB
}

const schema = `
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
);
`

func openDatabase(path string) (*sqliteDB, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if _, err := conn.Exec(schema); err != nil {
		conn.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	return &sqliteDB{conn: conn}, nil
}

func (d *sqliteDB) Close() error {
	return d.conn.Close()
}

// saveJob upserts a job snapshot. created_at is only set on first insert —
// it is deliberately left out of the ON CONFLICT SET clause so it survives
// subsequent status updates.
func (d *sqliteDB) saveJob(job *DownloadJob) error {
	_, err := d.conn.Exec(
		`INSERT INTO jobs (id, url, title, status, quality, file_path, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
			url = excluded.url,
			title = excluded.title,
			status = excluded.status,
			quality = excluded.quality,
			file_path = excluded.file_path`,
		job.ID, job.URL, job.Title, job.Status, job.Quality, job.FilePath,
		time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

func (d *sqliteDB) deleteJob(id string) error {
	_, err := d.conn.Exec(`DELETE FROM jobs WHERE id = ?`, id)
	return err
}

func (d *sqliteDB) loadJobs() ([]*DownloadJob, error) {
	rows, err := d.conn.Query(`SELECT id, url, title, status, quality, file_path FROM jobs ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobs := make([]*DownloadJob, 0)
	for rows.Next() {
		job := &DownloadJob{Tracks: []DownloadTrack{}}
		if err := rows.Scan(&job.ID, &job.URL, &job.Title, &job.Status, &job.Quality, &job.FilePath); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (d *sqliteDB) saveVideo(video *VideoRecord) error {
	var thumbnailPath sql.NullString
	if video.ThumbnailPath != "" {
		thumbnailPath = sql.NullString{String: video.ThumbnailPath, Valid: true}
	}

	_, err := d.conn.Exec(
		`INSERT INTO videos (id, title, filename, duration_seconds, thumbnail_path, downloaded_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		video.ID, video.Title, video.Filename, video.DurationSeconds, thumbnailPath, video.DownloadedAt,
	)
	return err
}

func (d *sqliteDB) loadVideos() ([]*VideoRecord, error) {
	rows, err := d.conn.Query(`SELECT id, title, filename, duration_seconds, thumbnail_path, downloaded_at FROM videos ORDER BY downloaded_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	videos := make([]*VideoRecord, 0)
	for rows.Next() {
		video := &VideoRecord{}
		var thumbnailPath sql.NullString
		if err := rows.Scan(&video.ID, &video.Title, &video.Filename, &video.DurationSeconds, &thumbnailPath, &video.DownloadedAt); err != nil {
			return nil, err
		}
		video.ThumbnailPath = thumbnailPath.String
		videos = append(videos, video)
	}
	return videos, rows.Err()
}
