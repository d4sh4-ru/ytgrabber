package server

import (
	"crypto/subtle"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"ytgrabber/internal/core"
	"ytgrabber/internal/model"
	"ytgrabber/internal/storage"
)

// Links to single videos: an admin makes one for a video of their own
// library, and anyone who has it watches the video on a page of its own
// (/s/<token>), without signing in and without seeing anything else of
// the server. A link lasts the term chosen for it; it may also ask for a
// six-digit code, sent apart from the link. A video has at most one link
// that works: sharing it again shows that one, as it was made.
const (
	sharePrefix = "/s/"
	shareCookie = "ytg_share"
	// A six-digit code is only safe while guessing it is slow: after
	// shareMaxFailures wrong codes within shareLockout the link takes
	// none until that window has passed.
	shareCodeDigits  = 6
	shareMaxFailures = 5
	shareLockout     = 15 * time.Minute
	maxShareForm     = 1 << 10
)

// shareTerms are the terms a link can be made for; "day" if none is given.
var shareTerms = map[string]time.Duration{
	"hour":  time.Hour,
	"day":   24 * time.Hour,
	"week":  7 * 24 * time.Hour,
	"month": 30 * 24 * time.Hour,
}

const defaultShareTerm = "day"

type shareRequest struct {
	VideoID string `json:"videoId"`
	// Term is how long the link works.
	Term string `json:"term,omitempty" enum:"hour,day,week,month"`
	// WithCode makes the link ask for a six-digit code.
	WithCode      bool `json:"withCode"`
	AllowDownload bool `json:"allowDownload"`
}

// shareGuard remembers wrong codes per link and the browsers that
// entered the right one. Both are in memory: after a restart the code is
// asked for again, which costs nothing.
type shareGuard struct {
	mu       sync.Mutex
	failures map[string][]time.Time // share id → wrong attempts
	grants   map[string]shareGrant  // cookie value → what it opens
}

type shareGrant struct {
	shareID string
	expires time.Time
}

func newShareGuard() *shareGuard {
	return &shareGuard{failures: make(map[string][]time.Time), grants: make(map[string]shareGrant)}
}

// lockedFor is how long the link still refuses codes; 0 if it takes them.
func (g *shareGuard) lockedFor(shareID string, now time.Time) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	attempts := g.recent(shareID, now)
	if len(attempts) < shareMaxFailures {
		return 0
	}
	return attempts[0].Add(shareLockout).Sub(now)
}

func (g *shareGuard) recordFailure(shareID string, now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.failures[shareID] = append(g.recent(shareID, now), now)
}

// recent drops the attempts older than the lockout window; g.mu is held.
func (g *shareGuard) recent(shareID string, now time.Time) []time.Time {
	cutoff := now.Add(-shareLockout)
	kept := g.failures[shareID][:0]
	for _, at := range g.failures[shareID] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	if len(kept) == 0 {
		delete(g.failures, shareID)
		return nil
	}
	g.failures[shareID] = kept
	return kept
}

func (g *shareGuard) grant(shareID string, expires time.Time, now time.Time) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	for value, grant := range g.grants {
		if !grant.expires.After(now) {
			delete(g.grants, value)
		}
	}
	delete(g.failures, shareID)
	value := randomToken()
	g.grants[value] = shareGrant{shareID: shareID, expires: expires}
	return value
}

func (g *shareGuard) granted(value string, shareID string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	grant, ok := g.grants[value]
	return ok && grant.shareID == shareID && grant.expires.After(now)
}

// shareCodeHash ties a code to its link: the stored hash says nothing
// about the code without the token, which isn't stored either.
func shareCodeHash(token string, code string) string {
	return hashSecret(token + ":" + code)
}

func randomDigits(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte('0' + randomIndex(10))
	}
	return string(b)
}

// --- The admin's API ---

// createShare makes a link to a video, or returns the one it already
// has, unchanged: a link someone was sent shouldn't change under them.
func (s *Server) createShare(r *http.Request, body *shareRequest) (any, error) {
	db, err := s.auth.users()
	if err != nil {
		return nil, err
	}
	user := identityOf(r).user
	video, err := db.GetVideo(user.ID, body.VideoID)
	if err != nil {
		return nil, err
	}
	if video.MediaKind != model.MediaVideo {
		return nil, badRequest("поделиться можно только видео")
	}
	term := body.Term
	if term == "" {
		term = defaultShareTerm
	}
	ttl, ok := shareTerms[term]
	if !ok {
		return nil, badRequest("неизвестный срок ссылки")
	}

	now := s.auth.now()
	if existing, err := db.ShareForVideo(user.ID, video.ID, now); err == nil {
		return withPath(existing), nil
	} else if !errors.Is(err, storage.ErrShareNotFound) {
		return nil, err
	}

	token := randomToken()
	share := &model.Share{
		ID:            uuid.NewString(),
		UserID:        user.ID,
		VideoID:       video.ID,
		Title:         video.Title,
		Token:         token,
		AllowDownload: body.AllowDownload,
		CreatedAt:     timestamp(now),
		ExpiresAt:     timestamp(now.Add(ttl)),
	}
	if body.WithCode {
		share.Code = randomDigits(shareCodeDigits)
		share.CodeHash = shareCodeHash(token, share.Code)
		share.HasCode = true
	}
	if err := db.CreateShare(share, hashSecret(token), now.Add(ttl)); err != nil {
		return nil, fmt.Errorf("не удалось создать ссылку: %w", err)
	}
	return withPath(share), nil
}

func (s *Server) listShares(r *http.Request) (any, error) {
	db, err := s.auth.users()
	if err != nil {
		return nil, err
	}
	shares, err := db.ListShares(identityOf(r).user.ID, s.auth.now())
	for _, share := range shares {
		withPath(share)
	}
	return shares, err
}

// withPath fills in the link's path; links made before 2.3 have none.
func withPath(share *model.Share) *model.Share {
	share.Path = ""
	if share.Token != "" {
		share.Path = sharePrefix + share.Token
	}
	return share
}

func (s *Server) removeShare(r *http.Request) (any, error) {
	db, err := s.auth.users()
	if err != nil {
		return nil, err
	}
	return nil, db.DeleteShare(identityOf(r).user.ID, r.PathValue("id"))
}

// --- The public page ---

//go:embed share.html
var shareFiles embed.FS

var shareTemplate = template.Must(template.ParseFS(shareFiles, "share.html"))

// sharePage is what share.html shows: the video, the code form or why
// there is nothing to show.
type sharePage struct {
	Title    string
	Base     string // /s/<token>
	Media    string // the file's name under Base
	Poster   bool
	Download bool
	Left     string // how long the link lasts, "ещё 5 ч"

	AskCode bool
	Error   string
	Missing bool
}

// resolveShare finds the link a public request names, with its video.
func (s *Server) resolveShare(r *http.Request) (*model.Share, *model.VideoRecord, error) {
	db, err := s.auth.users()
	if err != nil {
		return nil, nil, err
	}
	share, err := db.ShareByToken(hashSecret(r.PathValue("token")), s.auth.now())
	if err != nil {
		return nil, nil, err
	}
	video, err := db.GetVideo(share.UserID, share.VideoID)
	if err != nil {
		return nil, nil, err
	}
	return share, video, nil
}

// unlocked tells whether the request may see the link's video: it has
// no code, or this browser entered it.
func (s *Server) unlocked(r *http.Request, share *model.Share) bool {
	if !share.HasCode {
		return true
	}
	cookie, err := r.Cookie(shareCookie)
	return err == nil && s.shares.granted(cookie.Value, share.ID, s.auth.now())
}

func (s *Server) serveSharePage(w http.ResponseWriter, r *http.Request) {
	share, video, err := s.resolveShare(r)
	if err != nil {
		s.renderShare(w, http.StatusNotFound, sharePage{Missing: true})
		return
	}
	base := sharePrefix + r.PathValue("token")
	if !s.unlocked(r, share) {
		s.askShareCode(w, r, share, sharePage{Title: video.Title, Base: base, AskCode: true})
		return
	}
	if db, err := s.auth.users(); err == nil {
		if err := db.CountShareView(share.ID); err != nil {
			log.Printf("ytgrabber-server: share %s: %v", share.ID, err)
		}
	}
	s.renderShare(w, http.StatusOK, sharePage{
		Title:    video.Title,
		Base:     base,
		Media:    "file" + strings.ToLower(filepath.Ext(video.FilePath)),
		Poster:   video.ThumbnailPath != "",
		Download: share.AllowDownload,
		Left:     timeLeft(share.ExpiresAt, s.auth.now()),
	})
}

// askShareCode shows the code form, saying why the last code didn't open
// the link: the form's answers come back here as a redirect (see
// enterShareCode), so reloading the page never sends the form again.
func (s *Server) askShareCode(w http.ResponseWriter, r *http.Request, share *model.Share, page sharePage) {
	if wait := s.shares.lockedFor(share.ID, s.auth.now()); wait > 0 {
		page.Error = fmt.Sprintf("Слишком много неверных попыток. Попробуйте через %d мин.", int(wait.Minutes())+1)
		s.renderShare(w, http.StatusTooManyRequests, page)
		return
	}
	if r.URL.Query().Get("code") == "wrong" {
		page.Error = "Код не подходит"
	}
	s.renderShare(w, http.StatusOK, page)
}

// enterShareCode takes the code form and always answers with a redirect
// to the page (post/redirect/get): the right code sets a cookie for this
// link only, a wrong one comes back as ?code=wrong.
func (s *Server) enterShareCode(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	base := sharePrefix + token
	share, _, err := s.resolveShare(r)
	if err != nil || !share.HasCode {
		http.Redirect(w, r, base, http.StatusSeeOther)
		return
	}

	now := s.auth.now()
	if s.shares.lockedFor(share.ID, now) > 0 {
		http.Redirect(w, r, base, http.StatusSeeOther)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxShareForm)
	code := strings.TrimSpace(r.PostFormValue("code"))
	given := shareCodeHash(token, code)
	if subtle.ConstantTimeCompare([]byte(given), []byte(share.CodeHash)) != 1 {
		s.shares.recordFailure(share.ID, now)
		http.Redirect(w, r, base+"?code=wrong", http.StatusSeeOther)
		return
	}

	expires, err := time.Parse(time.RFC3339, share.ExpiresAt)
	if err != nil {
		expires = now.Add(shareTerms[defaultShareTerm])
	}
	http.SetCookie(w, &http.Cookie{
		Name:     shareCookie,
		Value:    s.shares.grant(share.ID, expires, now),
		Path:     base,
		Expires:  expires,
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		// Lax, not Strict: the link is opened from a messenger, another
		// site, and the cookie has to come along on that navigation.
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, base, http.StatusSeeOther)
}

// serveShareMedia serves the link's video and its cover.
func (s *Server) serveShareMedia(w http.ResponseWriter, r *http.Request) {
	share, video, err := s.resolveShare(r)
	if err != nil || !s.unlocked(r, share) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	core.ServeMedia(w, r, video, r.PathValue("name"), share.AllowDownload)
}

func (s *Server) renderShare(w http.ResponseWriter, status int, page sharePage) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := shareTemplate.Execute(w, page); err != nil {
		log.Printf("ytgrabber-server: share page: %v", err)
	}
}

// sharePageHeaders: the page has no scripts at all and loads only its own
// video and cover; search engines are asked to leave it alone.
func sharePageHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; media-src 'self'; "+
			"style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		h.Set("X-Robots-Tag", "noindex, nofollow")
		next.ServeHTTP(w, r)
	})
}

// timeLeft says how long a link lasts: in days, hours or, at the end,
// minutes.
func timeLeft(expiresAt string, now time.Time) string {
	expires, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return ""
	}
	left := expires.Sub(now)
	switch {
	case left >= 48*time.Hour:
		days := int(left.Hours() / 24)
		return fmt.Sprintf("ещё %d %s", days, pluralRu(days, "день", "дня", "дней"))
	case left >= time.Hour:
		hours := int(left.Hours())
		return fmt.Sprintf("ещё %d %s", hours, pluralRu(hours, "час", "часа", "часов"))
	default:
		minutes := max(1, int(left.Minutes()))
		return fmt.Sprintf("ещё %d %s", minutes, pluralRu(minutes, "минуту", "минуты", "минут"))
	}
}

// pluralRu picks the Russian form for n: 1 день, 2 дня, 5 дней.
func pluralRu(n int, one, few, many string) string {
	switch lastTwo := n % 100; {
	case lastTwo >= 11 && lastTwo <= 14:
		return many
	case lastTwo%10 == 1:
		return one
	case lastTwo%10 >= 2 && lastTwo%10 <= 4:
		return few
	}
	return many
}

// isShareNotFound lets the API answer 404 for a link that is gone.
func isShareNotFound(err error) bool {
	return errors.Is(err, storage.ErrShareNotFound)
}
