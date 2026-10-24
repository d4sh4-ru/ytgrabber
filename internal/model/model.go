// Package model holds the data types shared between storage, the download
// engine and the frontend (Wails generates TypeScript classes from them).
package model

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
	ID        string          `json:"id"`
	URL       string          `json:"url"`
	Title     string          `json:"title"`
	Quality   string          `json:"quality"`
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
	ID     string        `json:"id"`
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
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	SourceURL       string    `json:"sourceUrl"`
	MediaKind       MediaKind `json:"mediaKind"`
	FilePath        string    `json:"filePath"`
	ThumbnailPath   string    `json:"-"`
	DurationSeconds float64   `json:"durationSeconds"`
	DownloadedAt    string    `json:"downloadedAt"`
	MediaURL        string    `json:"mediaUrl"`
	ThumbnailURL    string    `json:"thumbnailUrl"`
	FileMissing     bool      `json:"fileMissing"`
}
