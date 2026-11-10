// Package model holds the data types shared between storage, the download
// engine and the frontend (Wails generates TypeScript classes from them).
package model

import "time"

type DownloadStatus string

const (
	StatusPending     DownloadStatus = "pending"
	StatusDownloading DownloadStatus = "downloading"
	StatusProcessing  DownloadStatus = "processing"
	StatusDone        DownloadStatus = "done"
	StatusError       DownloadStatus = "error"
)

// ActiveStatuses are the statuses of a job that something is still working on.
var ActiveStatuses = []DownloadStatus{StatusPending, StatusDownloading, StatusProcessing}

type TrackKind string

const (
	TrackVideo TrackKind = "video"
	TrackAudio TrackKind = "audio"
)

// DownloadTrack is one stream yt-dlp downloads for a job (video and audio
// are fetched separately and merged afterwards).
type DownloadTrack struct {
	Kind     TrackKind `json:"kind"`
	Progress float64   `json:"progress"`
	Filename string    `json:"filename"`
}

// ErrorKind classifies a failed job when the UI can offer a specific fix.
type ErrorKind string

const (
	ErrorGeneric ErrorKind = ""
	// ErrorLoginRequired means the site wants a signed-in user: the UI
	// offers to connect or refresh an account for Site.
	ErrorLoginRequired ErrorKind = "login_required"
)

// DownloadJob is a download that hasn't made it into the library yet:
// queued, running or failed.
type DownloadJob struct {
	ID string `json:"id"`
	// UserID is whose job it is (core.UserID); never sent to clients.
	UserID  string `json:"-"`
	URL     string `json:"url"`
	Title   string `json:"title"`
	Quality string `json:"quality"`
	// Format is what Quality produces ("mp4", "mp3"). It is derived, not
	// stored: the core fills it in for new and reloaded jobs.
	Format    string          `json:"format"`
	FilePath  string          `json:"filePath"`
	Status    DownloadStatus  `json:"status"`
	Tracks    []DownloadTrack `json:"tracks"`
	Error     string          `json:"error,omitempty"`
	ErrorKind ErrorKind       `json:"errorKind"`
	CreatedAt string          `json:"createdAt"`
	// UseAccount lets the job use a connected account for its site; off
	// for "download without an account".
	UseAccount bool `json:"useAccount"`
	// Site is the account key of the URL (see accounts.SiteOf), empty if
	// it has none; AccountUsed is set while/after the job ran with that
	// account's cookies.
	Site        string `json:"site"`
	AccountUsed bool   `json:"accountUsed"`
	// ErrorDetails is the full tool output behind Error (e.g. yt-dlp's
	// stderr), shown on demand; empty when Error says it all.
	ErrorDetails string `json:"errorDetails,omitempty"`
	// WaitingFor lists the tools a queued job waits for while the server
	// installs them ("yt-dlp, ffmpeg"); empty otherwise. Not stored.
	WaitingFor string `json:"waitingFor,omitempty"`
}

type AccountSource string

const (
	AccountFromFile    AccountSource = "file"
	AccountFromBrowser AccountSource = "browser"
)

type AccountStatus string

const (
	AccountOK      AccountStatus = "ok"
	AccountExpired AccountStatus = "expired"
	AccountError   AccountStatus = "error"
)

// Account is a site the user connected their login for. Only metadata is
// stored here and sent to the frontend; the cookies themselves live in an
// encrypted snapshot (package accounts) and never leave the backend.
type Account struct {
	ID string `json:"id"`
	// UserID is whose account it is (core.UserID); never sent to clients.
	UserID string        `json:"-"`
	Domain string        `json:"domain"`
	Source AccountSource `json:"source"`
	// Browser is the browser id (package browsers), BrowserName its
	// display name; Profile is the profile's display name and ProfileDir
	// where its cookies are re-read from on refresh.
	Browser     string `json:"browser"`
	BrowserName string `json:"browserName"`
	Profile     string `json:"profile"`
	ProfileDir  string `json:"-"`
	CookieCount int    `json:"cookieCount"`
	// LoginKnown is set for sites whose session cookie the app knows;
	// only then LoggedIn is meaningful.
	LoginKnown  bool          `json:"loginKnown"`
	LoggedIn    bool          `json:"loggedIn"`
	Status      AccountStatus `json:"status"`
	CreatedAt   string        `json:"createdAt"`
	RefreshedAt string        `json:"refreshedAt"`
	LastUsedAt  string        `json:"lastUsedAt"`
}

type MediaKind string

const (
	MediaVideo MediaKind = "video"
	MediaAudio MediaKind = "audio"
)

// VideoRecord is one library entry. FilePath is absolute, so entries keep
// working after the downloads folder is changed. The frontend never loads
// files by path — WebViews block file:// access — but through MediaURL /
// ThumbnailURL, which the app's media handler serves by record id.
type VideoRecord struct {
	ID string `json:"id"`
	// UserID is whose library the entry is in (core.UserID); never sent
	// to clients.
	UserID          string    `json:"-"`
	Title           string    `json:"title"`
	SourceURL       string    `json:"sourceUrl"`
	MediaKind       MediaKind `json:"mediaKind"`
	FilePath        string    `json:"filePath"`
	ThumbnailPath   string    `json:"-"`
	DurationSeconds float64   `json:"durationSeconds"`
	// PositionSeconds is where playback stopped last time; 0 means from
	// the start (never played, or watched to the end).
	PositionSeconds float64 `json:"positionSeconds"`
	DownloadedAt    string  `json:"downloadedAt"`
	MediaURL        string  `json:"mediaUrl"`
	ThumbnailURL    string  `json:"thumbnailUrl"`
	FileMissing     bool    `json:"fileMissing"`
	// SizeBytes is the file's size, read along with FileMissing; 0 when
	// the file is gone.
	SizeBytes int64 `json:"sizeBytes"`
}

// Role is what a server user may do: an admin also manages users, the
// tools and the server's settings.
type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

// User is someone with their own library, queue and site accounts on a
// server. The desktop app has none: it is the single local user.
type User struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Role      Role   `json:"role" enum:"admin,user"`
	CreatedAt string `json:"createdAt"`
}

// Device is one signed-in browser, phone or API client of a user. It is
// known by a random token of its own, stored only as a hash; revoking the
// device signs it out.
type Device struct {
	ID         string `json:"id"`
	UserID     string `json:"-"`
	Name       string `json:"name"`
	CreatedAt  string `json:"createdAt"`
	LastSeenAt string `json:"lastSeenAt"`
	// Current marks the device the request came from.
	Current bool `json:"current"`
}

// CodeKind is what a one-time code does when entered on the login screen.
type CodeKind string

const (
	// CodeSetup creates the first admin; it exists until there is one.
	CodeSetup CodeKind = "setup"
	// CodeInvite creates a new user.
	CodeInvite CodeKind = "invite"
	// CodeLink signs a new device in as an existing user.
	CodeLink CodeKind = "link"
)

// Code is a stored one-time code (an invite or a device link), kept as a
// hash like device tokens.
type Code struct {
	Hash      string
	Kind      CodeKind
	UserID    string // link: who the device signs in as
	Role      Role   // invite: the new user's role
	CreatedBy string
	ExpiresAt time.Time
}
