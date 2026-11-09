package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"
)

// sessionCookie carries the session of a browser that entered the token:
// <video src> and <img src> can't send an Authorization header, a cookie
// they do send. SameSite=Strict keeps other sites from riding on it.
const sessionCookie = "ytg_session"

// sessionMaxAge is how long a browser stays signed in.
const sessionMaxAge = 365 * 24 * time.Hour

// Login attempts are limited so the token can't be guessed online: after
// maxFailures wrong tokens within failureWindow every attempt is refused
// until the window passes.
const (
	maxFailures   = 10
	failureWindow = time.Minute
)

// auth checks the access token. A request is let through with either
// "Authorization: Bearer <token>" (API clients) or the session cookie,
// whose value is an HMAC of the token: it reveals nothing about the token
// and stops working as soon as the token is changed.
type auth struct {
	token   []byte
	session string
	now     func() time.Time

	mu       sync.Mutex
	failures []time.Time
}

func newAuth(token string) *auth {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte("ytgrabber session v1"))
	return &auth{
		token:   []byte(token),
		session: hex.EncodeToString(mac.Sum(nil)),
		now:     time.Now,
	}
}

func (a *auth) authorized(r *http.Request) bool {
	if bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return a.tokenMatches(bearer)
	}
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		return subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(a.session)) == 1
	}
	return false
}

func (a *auth) tokenMatches(token string) bool {
	return subtle.ConstantTimeCompare([]byte(token), a.token) == 1
}

func (a *auth) require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.authorized(r) {
			writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "нужен вход"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// getSession tells the frontend whether it has to show the login screen.
func (a *auth) getSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, sessionResponse{Authenticated: a.authorized(r)})
}

// login exchanges the token for the session cookie.
func (a *auth) login(w http.ResponseWriter, r *http.Request) {
	if a.limited() {
		writeJSON(w, http.StatusTooManyRequests, errorResponse{Error: "слишком много попыток, подождите минуту"})
		return
	}
	var body loginRequest
	if err := decodeJSON(r, &body); err != nil || !a.tokenMatches(strings.TrimSpace(body.Token)) {
		a.recordFailure()
		writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "неверный токен"})
		return
	}
	http.SetCookie(w, a.cookie(r, a.session, int(sessionMaxAge.Seconds())))
	writeJSON(w, http.StatusOK, sessionResponse{Authenticated: true})
}

func (a *auth) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, a.cookie(r, "", -1))
	w.WriteHeader(http.StatusNoContent)
}

func (a *auth) cookie(r *http.Request, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookie,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		// Behind a TLS reverse proxy (Caddy, nginx, tailscale serve, ...)
		// the browser talks HTTPS even though this server doesn't.
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteStrictMode,
	}
}

func (a *auth) limited() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pruneFailures()
	return len(a.failures) >= maxFailures
}

func (a *auth) recordFailure() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pruneFailures()
	a.failures = append(a.failures, a.now())
}

func (a *auth) pruneFailures() {
	cutoff := a.now().Add(-failureWindow)
	kept := a.failures[:0]
	for _, at := range a.failures {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	a.failures = kept
}
