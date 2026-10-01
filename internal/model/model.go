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
	CreatedAt string          `json:"createdAt"`
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
