package storage

import (
	"database/sql"
	"path/filepath"
	"testing"

	"ytgrabber/internal/model"
)

func openTestDB(t *testing.T, legacyDir string) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"), MigrationEnv{LegacyDownloadsDir: legacyDir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestMigrateLegacyDatabase opens a database in the layout of the first
// release (no user_version, bare filenames) and checks it is upgraded.
func TestMigrateLegacyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacyDir := filepath.Join(t.TempDir(), "Movies", "ytgrabber")

	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	tx := mustBegin(t, conn)
	if err := migrateInitialSchema(tx, MigrationEnv{}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`INSERT INTO jobs (id, url, status, created_at) VALUES ('j1', 'https://x', 'downloading', '2025-01-01T00:00:00Z')`,
		`INSERT INTO videos VALUES ('v1', 'Song', 'Song.mp3', 10, NULL, '2025-01-01T00:00:00Z')`,
		`INSERT INTO videos VALUES ('v2', 'Clip', 'Clip.mp4', 20, 'Clip.jpg', '2025-01-02T00:00:00Z')`,
		// Same file as v2: before the migration equal titles overwrote each other.
		`INSERT INTO videos VALUES ('v3', 'Clip', 'Clip.mp4', 20, 'Clip.jpg', '2025-01-03T00:00:00Z')`,
	} {
		if _, err := conn.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	conn.Close()

	db, err := Open(path, MigrationEnv{LegacyDownloadsDir: legacyDir})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var version int
	db.conn.QueryRow(`PRAGMA user_version`).Scan(&version)
	if version != len(migrations) {
		t.Errorf("user_version = %d, want %d", version, len(migrations))
	}

	// What existed before users belongs to the desktop's local user.
	videos, err := db.LoadVideos("local")
	if err != nil {
		t.Fatal(err)
	}
	if len(videos) != 2 {
		t.Fatalf("got %d videos, want 2 (duplicates collapsed)", len(videos))
	}

	clip, song := videos[0], videos[1]
	if clip.ID != "v3" || clip.FilePath != filepath.Join(legacyDir, "Clip.mp4") || clip.MediaKind != model.MediaVideo {
		t.Errorf("clip migrated wrong: %+v", clip)
	}
	if clip.ThumbnailPath != filepath.Join(legacyDir, "Clip.jpg") {
		t.Errorf("thumbnail migrated wrong: %q", clip.ThumbnailPath)
	}
	if song.MediaKind != model.MediaAudio || song.ThumbnailPath != "" {
		t.Errorf("song migrated wrong: %+v", song)
	}

	jobs, err := db.LoadJobs()
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs = %v, %v", jobs, err)
	}

	// Re-opening an up-to-date database must be a no-op.
	db.Close()
	db, err = Open(path, MigrationEnv{LegacyDownloadsDir: legacyDir})
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
}

func mustBegin(t *testing.T, conn *sql.DB) *sql.Tx {
	t.Helper()
	tx, err := conn.Begin()
	if err != nil {
		t.Fatal(err)
	}
	return tx
}

func TestRejectNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	conn, _ := sql.Open("sqlite", path)
	conn.Exec(`PRAGMA user_version = 999`)
	conn.Close()

	if _, err := Open(path, MigrationEnv{}); err == nil {
		t.Fatal("expected an error for a database from a newer version")
	}
}

func TestMarkInterruptedJobs(t *testing.T) {
	db := openTestDB(t, "")

	for id, status := range map[string]model.DownloadStatus{
		"pending": model.StatusPending, "downloading": model.StatusDownloading,
		"processing": model.StatusProcessing, "failed": model.StatusError,
	} {
		if err := db.SaveJob(&model.DownloadJob{UserID: "local", ID: id, URL: "https://x", Status: status, Error: "old", ErrorDetails: "old details"}); err != nil {
			t.Fatal(err)
		}
	}

	if err := db.MarkInterruptedJobs("interrupted"); err != nil {
		t.Fatal(err)
	}

	jobs, _ := db.LoadJobs()
	for _, job := range jobs {
		if job.Status != model.StatusError {
			t.Errorf("%s: status %s, want error", job.ID, job.Status)
		}
		wantErr := "interrupted"
		if job.ID == "failed" {
			wantErr = "old"
		}
		if job.Error != wantErr {
			t.Errorf("%s: error %q, want %q", job.ID, job.Error, wantErr)
		}
		wantDetails := ""
		if job.ID == "failed" {
			wantDetails = "old details"
		}
		if job.ErrorDetails != wantDetails {
			t.Errorf("%s: details %q, want %q", job.ID, job.ErrorDetails, wantDetails)
		}
	}
}

func TestSaveVideoUpsertsByPath(t *testing.T) {
	db := openTestDB(t, "")

	first := &model.VideoRecord{ID: "a", UserID: "u1", Title: "Old", MediaKind: model.MediaVideo, FilePath: "/d/x.mp4", DownloadedAt: "1"}
	second := &model.VideoRecord{ID: "b", UserID: "u1", Title: "New", MediaKind: model.MediaVideo, FilePath: "/d/x.mp4", DownloadedAt: "2"}
	if err := db.SaveVideo(first); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveVideo(second); err != nil {
		t.Fatal(err)
	}

	videos, _ := db.LoadVideos("u1")
	if len(videos) != 1 || videos[0].ID != "a" || videos[0].Title != "New" {
		t.Errorf("videos = %+v", videos)
	}
}

func TestSettingsKeyValue(t *testing.T) {
	db := openTestDB(t, "")

	db.SetSetting("a", "1")
	db.SetSetting("a", "2")
	db.SetSetting("b", "x")
	db.SetSetting("b", "") // empty deletes

	values, err := db.LoadSettings()
	if err != nil || len(values) != 1 || values["a"] != "2" {
		t.Errorf("settings = %v, %v", values, err)
	}
}

func TestJobAccountFieldsAndAccounts(t *testing.T) {
	db := openTestDB(t, "")

	job := &model.DownloadJob{UserID: "local", ID: "j1", URL: "https://x.com/a", Quality: "audio", Status: model.StatusError, ErrorKind: model.ErrorLoginRequired,
		UseAccount: false, Site: "x.com", AccountUsed: true}
	if err := db.SaveJob(job); err != nil {
		t.Fatal(err)
	}
	jobs, err := db.LoadJobs()
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs = %+v, %v", jobs, err)
	}
	if got := jobs[0]; got.Quality != "audio" || got.ErrorKind != model.ErrorLoginRequired || got.UseAccount || got.Site != "x.com" || !got.AccountUsed {
		t.Errorf("job = %+v", got)
	}

	account := &model.Account{ID: "a1", UserID: "u1", Domain: "x.com", Source: model.AccountFromFile, CookieCount: 3, LoggedIn: true,
		Status: model.AccountOK, CreatedAt: "c", RefreshedAt: "r"}
	if err := db.SaveAccount(account); err != nil {
		t.Fatal(err)
	}
	account.Status = model.AccountExpired
	account.CreatedAt = "ignored on update"
	if err := db.SaveAccount(account); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetAccountByDomain("u1", "x.com")
	if err != nil || got.Status != model.AccountExpired || got.CreatedAt != "c" || !got.LoggedIn || got.CookieCount != 3 {
		t.Errorf("account = %+v, %v", got, err)
	}
	if err := db.SaveAccount(&model.Account{ID: "a2", UserID: "u1", Domain: "x.com", Source: model.AccountFromFile, Status: model.AccountOK, CreatedAt: "c", RefreshedAt: "r"}); err == nil {
		t.Error("two accounts for one domain")
	}

	if err := db.DeleteAccount("u1", "a1"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetAccount("u1", "a1"); err != ErrAccountNotFound {
		t.Errorf("deleted account error = %v", err)
	}
}

// Each user sees only their own library and accounts; another user's id
// is "not found", and two users may connect the same site.
func TestUsersAreSeparate(t *testing.T) {
	db := openTestDB(t, "")

	for _, user := range []string{"alice", "bob"} {
		video := &model.VideoRecord{ID: user + "-v", UserID: user, Title: user, MediaKind: model.MediaVideo,
			FilePath: "/media/" + user + "/x.mp4", DownloadedAt: "1"}
		if err := db.SaveVideo(video); err != nil {
			t.Fatal(err)
		}
		account := &model.Account{ID: user + "-a", UserID: user, Domain: "x.com", Source: model.AccountFromFile,
			Status: model.AccountOK, CreatedAt: "c", RefreshedAt: "r"}
		if err := db.SaveAccount(account); err != nil {
			t.Fatalf("%s: %v", user, err)
		}
	}

	videos, _ := db.LoadVideos("alice")
	if len(videos) != 1 || videos[0].ID != "alice-v" || videos[0].UserID != "alice" {
		t.Errorf("alice's library = %+v", videos)
	}
	if _, err := db.GetVideo("alice", "bob-v"); err != ErrVideoNotFound {
		t.Errorf("alice got bob's video: %v", err)
	}
	if _, err := db.GetAccount("alice", "bob-a"); err != ErrAccountNotFound {
		t.Errorf("alice got bob's account: %v", err)
	}
	if account, err := db.GetAccountByDomain("bob", "x.com"); err != nil || account.ID != "bob-a" {
		t.Errorf("bob's x.com = %+v, %v", account, err)
	}

	// Writes by id don't reach another user's rows.
	db.SavePosition("alice", "bob-v", 42)
	db.DeleteVideo("alice", "bob-v")
	db.DeleteAccount("alice", "bob-a")
	if video, err := db.GetVideo("bob", "bob-v"); err != nil || video.PositionSeconds != 0 {
		t.Errorf("bob's video = %+v, %v", video, err)
	}
	if _, err := db.GetAccount("bob", "bob-a"); err != nil {
		t.Errorf("bob's account deleted by alice: %v", err)
	}
	if all, _ := db.AllVideos(); len(all) != 2 {
		t.Errorf("all videos = %d", len(all))
	}
}
