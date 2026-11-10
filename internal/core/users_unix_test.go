//go:build !windows

package core

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"ytgrabber/internal/model"
)

// userEvents records who each event went to: PublishTo per user,
// Publish to everyone.
type userEvents struct {
	mu        sync.Mutex
	perUser   map[UserID][]string
	broadcast []string
}

func (e *userEvents) Publish(name string, _ ...interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.broadcast = append(e.broadcast, name)
}

func (e *userEvents) PublishTo(user UserID, name string, _ ...interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.perUser[user] = append(e.perUser[user], name)
}

func (e *userEvents) of(user UserID) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.perUser[user])
}

const (
	alice UserID = "alice"
	bob   UserID = "bob"
)

// TestUsersSeeOnlyTheirLibrary: alice's download lands in her folder and
// her library; bob doesn't see it, isn't told about it and can't touch it.
func TestUsersSeeOnlyTheirLibrary(t *testing.T) {
	gh := newFakeGitHub(t, "2026.01.01", "")
	app, _ := newAppWithManagedYtDlp(t, fakeBuild("2026.01.01", "Alice clip", "", ""), gh)
	events := &userEvents{perUser: map[UserID][]string{}}
	app.events = events

	if _, err := app.DownloadVideo(alice, "https://example.com/watch?v=a1", "best", false); err != nil {
		t.Fatal(err)
	}
	var video *model.VideoRecord
	waitFor(t, "alice's download", func() bool {
		videos, _ := app.GetLibrary(alice)
		if len(videos) == 1 {
			video = videos[0]
		}
		return video != nil
	})

	aliceDir := app.GetSettings(alice).DownloadsDir
	if aliceDir != filepath.Join(app.settings.DownloadsDir, "alice") || !strings.HasPrefix(video.FilePath, aliceDir+string(filepath.Separator)) {
		t.Errorf("alice's file %s, her folder %s", video.FilePath, aliceDir)
	}
	if dir := app.GetSettings(LocalUser).DownloadsDir; dir != app.settings.DownloadsDir {
		t.Errorf("the local user's folder moved: %s", dir)
	}

	if videos, _ := app.GetLibrary(bob); len(videos) != 0 {
		t.Errorf("bob sees %d videos", len(videos))
	}
	if jobs, _ := app.GetJobs(bob); len(jobs) != 0 {
		t.Errorf("bob sees %d jobs", len(jobs))
	}
	if got := events.of(alice); !slices.Contains(got, "download-status") || !slices.Contains(got, "library-changed") {
		t.Errorf("alice's events: %v", got)
	}
	if got := events.of(bob); len(got) != 0 {
		t.Errorf("bob got alice's events: %v", got)
	}
	if slices.Contains(events.broadcast, "download-status") || slices.Contains(events.broadcast, "library-changed") {
		t.Errorf("alice's events went to everyone: %v", events.broadcast)
	}

	if err := app.DeleteVideo(bob, video.ID, true); !IsNotFound(err) {
		t.Errorf("bob deleting alice's video: %v", err)
	}
	if _, err := os.Stat(video.FilePath); err != nil {
		t.Errorf("alice's file is gone: %v", err)
	}
	if _, err := app.SavePlaybackPosition(bob, video.ID, 30); !IsNotFound(err) {
		t.Errorf("bob saving a position on alice's video: %v", err)
	}
	if err := app.RevealVideo(bob, video.ID); !IsNotFound(err) {
		t.Errorf("bob revealing alice's video: %v", err)
	}
	if videos, _ := app.GetLibrary(alice); len(videos) != 1 || videos[0].PositionSeconds != 0 {
		t.Errorf("alice's library after bob: %+v", videos)
	}

	for user, want := range map[UserID]int{alice: http.StatusOK, bob: http.StatusNotFound} {
		handler := MediaHandler(app, func(*http.Request) UserID { return user })
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, video.MediaURL, nil))
		if rec.Code != want {
			t.Errorf("%s fetching alice's file: %d, want %d", user, rec.Code, want)
		}
	}
}

// TestUsersHaveSeparateAccounts: the same site connected by two users is
// two accounts; neither can see, check or remove the other's.
func TestUsersHaveSeparateAccounts(t *testing.T) {
	app, _ := newAppWithCookieTools(t)
	events := &userEvents{perUser: map[UserID][]string{}}
	app.events = events
	file := filepath.Join(t.TempDir(), "cookies.txt")
	os.WriteFile(file, []byte("# Netscape HTTP Cookie File\n"+
		".example.com\tTRUE\t/\tTRUE\t2000000000\tsession\ts1\n"), 0o644)

	aliceAccount, err := app.importCookiesFromPath(alice, "example.com", file)
	if err != nil {
		t.Fatal(err)
	}
	if list, _ := app.GetAccounts(bob); len(list) != 0 {
		t.Errorf("bob sees %d accounts", len(list))
	}
	if got := events.of(bob); len(got) != 0 {
		t.Errorf("bob got alice's events: %v", got)
	}
	if _, err := app.CheckAccount(bob, aliceAccount.ID); !IsNotFound(err) {
		t.Errorf("bob checking alice's account: %v", err)
	}
	if err := app.RemoveAccount(bob, aliceAccount.ID); !IsNotFound(err) {
		t.Errorf("bob removing alice's account: %v", err)
	}

	bobAccount, err := app.importCookiesFromPath(bob, "example.com", file)
	if err != nil {
		t.Fatal(err)
	}
	if bobAccount.ID == aliceAccount.ID {
		t.Fatal("bob's import replaced alice's account")
	}
	if list, _ := app.GetAccounts(alice); len(list) != 1 || list[0].ID != aliceAccount.ID {
		t.Errorf("alice's accounts: %+v", list)
	}
	if names := snapshotNames(t, app, aliceAccount); !slices.Equal(names, []string{"session"}) {
		t.Errorf("alice's cookies: %v", names)
	}
	// A snapshot bound to alice doesn't open as bob's.
	if _, err := app.accounts.Load(aliceAccount.ID, string(bob), "example.com"); err == nil {
		t.Error("alice's cookies decrypted as bob's")
	}
	if session := app.startAccountSession(bob, "example.com", "run"); session == nil || session.account.ID != bobAccount.ID {
		t.Errorf("bob's download would use %+v", session)
	} else {
		app.finishAccountSession(session, false, false)
	}
}

// TestUsersCantTouchOthersJobs: bob can neither retry nor remove a job of
// alice's, and doesn't see it.
func TestUsersCantTouchOthersJobs(t *testing.T) {
	app, _ := newTestApp(t)
	j := &job{DownloadJob: model.DownloadJob{UserID: string(alice), ID: "j", URL: "https://x", Status: model.StatusError, Tracks: []model.DownloadTrack{}}}
	app.jobs[j.ID] = j
	if err := app.db.SaveJob(&j.DownloadJob); err != nil {
		t.Fatal(err)
	}

	if err := app.RetryJob(bob, "j", false); !IsNotFound(err) {
		t.Errorf("bob retrying alice's job: %v", err)
	}
	if err := app.RemoveJob(bob, "j"); err != nil {
		t.Errorf("bob removing alice's job: %v", err)
	}
	if jobs, _ := app.GetJobs(alice); len(jobs) != 1 || jobs[0].Status != model.StatusError {
		t.Errorf("alice's jobs after bob: %+v", jobs)
	}
	if err := app.RemoveJob(alice, "j"); err != nil {
		t.Fatal(err)
	}
	if jobs, _ := app.GetJobs(alice); len(jobs) != 0 {
		t.Errorf("alice's job not removed: %+v", jobs)
	}
}

func TestUserDirName(t *testing.T) {
	for user, want := range map[UserID]string{
		"alice":    "alice",
		"a-b_9":    "a-b_9",
		"..":       "user-",
		"../x":     "user-",
		"a/b":      "user-",
		"":         "user-",
		"Анна":     "user-",
		"with dot": "user-",
	} {
		got := userDirName(user)
		if got != want && !(want == "user-" && strings.HasPrefix(got, want) && len(got) == len(want)+16) {
			t.Errorf("userDirName(%q) = %q", user, got)
		}
	}
}
