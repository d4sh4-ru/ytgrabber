package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"ytgrabber/internal/core"
	"ytgrabber/internal/model"
	"ytgrabber/internal/storage"
)

// sessionCookie carries a browser's device token: <video src> and
// <img src> can't send an Authorization header, a cookie they do send.
// It is HttpOnly, so the page's scripts never see the token, and
// SameSite=Strict keeps other sites from riding on it.
const sessionCookie = "ytg_session"

// sessionMaxAge is how long a browser stays signed in without coming
// back; revoking the device ends it sooner.
const sessionMaxAge = 365 * 24 * time.Hour

// Wrong codes are limited so they can't be guessed online: after
// maxFailures within failureWindow every attempt is refused until the
// window passes.
const (
	maxFailures   = 10
	failureWindow = time.Minute
)

// How long one-time codes last: a device link is entered right away on
// the device at hand, an invite or a link made by an admin is sent to
// someone first.
const (
	ownLinkTTL   = 10 * time.Minute
	sentCodeTTL  = 24 * time.Hour
	touchEvery   = time.Minute
	maxNameRunes = 40
)

// codeAlphabet has no look-alikes (0/O, 1/I/L); ten characters of it are
// 50 bits, far beyond what the attempt limit lets anyone try.
const (
	codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	codeLength   = 10
)

// auth signs devices in and tells whose a request is. A device is known by
// a random token, sent as "Authorization: Bearer <token>" (API clients) or
// in the session cookie (browsers); the database keeps only its hash.
//
// The very first sign-in uses the setup code (the old access token from
// the configuration or the data dir) and creates the admin, who owns all
// data that existed before there were users. After that the setup code
// is worthless: new users come with invites, new devices of a user with
// a link code made on one of their devices.
type auth struct {
	app       *core.App
	events    *Hub
	setupCode []byte
	now       func() time.Time

	mu       sync.Mutex
	failures []time.Time

	seenMu   sync.Mutex
	lastSeen map[string]time.Time
}

func newAuth(app *core.App, events *Hub, setupCode string) *auth {
	return &auth{
		app:       app,
		events:    events,
		setupCode: []byte(setupCode),
		now:       time.Now,
		lastSeen:  make(map[string]time.Time),
	}
}

// identity is who a request comes from.
type identity struct {
	user   *model.User
	device *model.Device
}

type identityKey struct{}

func identityOf(r *http.Request) *identity {
	id, _ := r.Context().Value(identityKey{}).(*identity)
	return id
}

// requestUser is the core user of a signed-in request.
func requestUser(r *http.Request) core.UserID {
	if id := identityOf(r); id != nil {
		return core.UserID(id.user.ID)
	}
	return "" // never reached: every route that needs a user is protected
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func (a *auth) users() (*storage.DB, error) {
	return a.app.Users()
}

// identify finds the device the request's token belongs to; nil if it
// has none or a revoked one.
func (a *auth) identify(r *http.Request) *identity {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			return nil
		}
		token = cookie.Value
	}
	if token == "" {
		return nil
	}
	db, err := a.users()
	if err != nil {
		return nil
	}
	device, err := db.DeviceByToken(hashSecret(token))
	if err != nil {
		return nil
	}
	user, err := db.GetUser(device.UserID)
	if err != nil {
		return nil
	}
	a.touch(db, device)
	return &identity{user: user, device: device}
}

// touch records when a device was last seen, at most once a minute.
func (a *auth) touch(db *storage.DB, device *model.Device) {
	now := a.now()
	a.seenMu.Lock()
	stale := now.Sub(a.lastSeen[device.ID]) >= touchEvery
	if stale {
		a.lastSeen[device.ID] = now
	}
	a.seenMu.Unlock()
	if stale {
		device.LastSeenAt = now.UTC().Format(time.RFC3339)
		if err := db.TouchDevice(device.ID, device.LastSeenAt); err != nil {
			log.Printf("ytgrabber-server: device %s: %v", device.ID, err)
		}
	}
}

func (a *auth) require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := a.identify(r)
		if id == nil {
			writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "нужен вход"})
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, id)))
	})
}

// requireAdmin goes inside require.
func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := identityOf(r); id == nil || id.user.Role != model.RoleAdmin {
			writeJSON(w, http.StatusForbidden, errorResponse{Error: "это может только администратор"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- Session: the login screen ---

// getSession tells the frontend whether it has to show the login screen,
// and whether that screen sets the server up.
func (a *auth) getSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.sessionOf(a.identify(r)))
}

func (a *auth) sessionOf(id *identity) sessionResponse {
	if id != nil {
		return sessionResponse{Authenticated: true, User: id.user, DeviceID: id.device.ID}
	}
	setup := false
	if db, err := a.users(); err == nil {
		n, err := db.CountUsers()
		setup = err == nil && n == 0
	}
	return sessionResponse{SetupRequired: setup}
}

// checkCode says what a code would do, without using it up, so the login
// screen knows whether to ask for a name.
func (a *auth) checkCode(w http.ResponseWriter, r *http.Request) {
	if a.limited() {
		writeJSON(w, http.StatusTooManyRequests, errorResponse{Error: "слишком много попыток, подождите минуту"})
		return
	}
	var body codeRequest
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	code, err := a.findCode(body.Code, false)
	if err != nil {
		a.refuse(w, err)
		return
	}
	response := codeResponse{Kind: code.Kind}
	if code.Kind == model.CodeLink {
		if db, err := a.users(); err == nil {
			if user, err := db.GetUser(code.UserID); err == nil {
				response.UserName = user.Name
			}
		}
	}
	writeJSON(w, http.StatusOK, response)
}

// login uses a code up and signs this browser in as a new device: of the
// first admin (setup code), of a new user (invite) or of the user the
// link was made for.
func (a *auth) login(w http.ResponseWriter, r *http.Request) {
	if a.limited() {
		writeJSON(w, http.StatusTooManyRequests, errorResponse{Error: "слишком много попыток, подождите минуту"})
		return
	}
	var body loginRequest
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	db, err := a.users()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: err.Error()})
		return
	}
	code, err := a.findCode(body.Code, true)
	if err != nil {
		a.refuse(w, err)
		return
	}

	user, err := a.userForCode(db, code, body.Name)
	if err != nil {
		// The code wasn't spent on anything: give it back.
		if code.Hash != "" {
			if err := db.SaveCode(code); err != nil {
				log.Printf("ytgrabber-server: restoring a code: %v", err)
			}
		}
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}

	device, token, err := a.newDevice(db, user, body.DeviceName)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}
	log.Printf("ytgrabber-server: %s signed in on a new device %q (%s code)", user.Name, device.Name, code.Kind)
	http.SetCookie(w, a.cookie(r, token, int(sessionMaxAge.Seconds())))
	writeJSON(w, http.StatusOK, a.sessionOf(&identity{user: user, device: device}))
}

func (a *auth) userForCode(db *storage.DB, code *model.Code, name string) (*model.User, error) {
	switch code.Kind {
	case model.CodeLink:
		return db.GetUser(code.UserID)
	case model.CodeSetup, model.CodeInvite:
		name, err := cleanName(name)
		if err != nil {
			return nil, err
		}
		user := &model.User{Name: name, Role: code.Role, CreatedAt: timestamp(a.now())}
		if code.Kind == model.CodeSetup {
			// The first admin is the local user: everything downloaded
			// before there were users is theirs.
			user.ID = string(core.LocalUser)
			created, err := db.CreateFirstUser(user)
			if err == nil && !created {
				err = errors.New("сервер уже настроен: войдите по приглашению или коду устройства")
			}
			return user, err
		}
		user.ID = uuid.NewString()
		return user, db.CreateUser(user)
	}
	return nil, storage.ErrCodeNotFound
}

// logout revokes this device: its token stops working for good.
func (a *auth) logout(w http.ResponseWriter, r *http.Request) {
	if id := a.identify(r); id != nil {
		if db, err := a.users(); err == nil {
			if err := db.DeleteDevice(id.user.ID, id.device.ID); err != nil {
				log.Printf("ytgrabber-server: signing out %s: %v", id.device.ID, err)
			}
		}
		a.events.closeDevice(id.device.ID)
	}
	http.SetCookie(w, a.cookie(r, "", -1))
	w.WriteHeader(http.StatusNoContent)
}

// findCode recognises the setup code (while there are no users) and
// stored codes; take uses a stored code up.
func (a *auth) findCode(raw string, take bool) (*model.Code, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errWrongCode
	}
	db, err := a.users()
	if err != nil {
		return nil, err
	}
	if len(a.setupCode) > 0 && subtle.ConstantTimeCompare([]byte(raw), a.setupCode) == 1 {
		n, err := db.CountUsers()
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return &model.Code{Kind: model.CodeSetup, Role: model.RoleAdmin}, nil
		}
		return nil, errWrongCode
	}
	hash := hashSecret(normalizeCode(raw))
	var code *model.Code
	if take {
		code, err = db.TakeCode(hash, a.now())
	} else {
		code, err = db.PeekCode(hash, a.now())
	}
	if errors.Is(err, storage.ErrCodeNotFound) {
		return nil, errWrongCode
	}
	return code, err
}

var errWrongCode = errors.New("код не подходит или истёк")

// refuse answers a failed code; only wrong codes count as attempts.
func (a *auth) refuse(w http.ResponseWriter, err error) {
	if errors.Is(err, errWrongCode) {
		a.recordFailure()
		writeJSON(w, http.StatusUnauthorized, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: err.Error()})
}

// --- Devices and codes ---

// newDevice signs user in on a new device and returns its token, the
// only time it is ever seen.
func (a *auth) newDevice(db *storage.DB, user *model.User, name string) (*model.Device, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Устройство"
	}
	if utf8.RuneCountInString(name) > 60 {
		name = string([]rune(name)[:60])
	}
	token := randomToken()
	now := timestamp(a.now())
	device := &model.Device{ID: uuid.NewString(), UserID: user.ID, Name: name, CreatedAt: now, LastSeenAt: now}
	if err := db.CreateDevice(device, hashSecret(token)); err != nil {
		return nil, "", err
	}
	return device, token, nil
}

// issueCode stores a one-time code and returns it as the user types it.
func (a *auth) issueCode(db *storage.DB, code model.Code, ttl time.Duration) (codeIssued, error) {
	plain := randomCode()
	code.Hash = hashSecret(normalizeCode(plain))
	code.ExpiresAt = a.now().Add(ttl)
	if err := db.SaveCode(&code); err != nil {
		return codeIssued{}, err
	}
	return codeIssued{Code: plain, ExpiresAt: code.ExpiresAt.UTC().Format(time.RFC3339)}, nil
}

func randomToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// randomCode is codeLength characters of codeAlphabet as XXXXX-XXXXX.
func randomCode() string {
	var b strings.Builder
	for i := range codeLength {
		if i == codeLength/2 {
			b.WriteByte('-')
		}
		b.WriteByte(codeAlphabet[randomIndex(len(codeAlphabet))])
	}
	return b.String()
}

// randomIndex is uniform in [0, n) for n <= 256.
func randomIndex(n int) int {
	limit := 256 - 256%n
	var b [1]byte
	for {
		rand.Read(b[:])
		if int(b[0]) < limit {
			return int(b[0]) % n
		}
	}
}

// normalizeCode forgives what people do when retyping a code: case,
// spaces and dashes.
func normalizeCode(code string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '-', '\t':
			return -1
		}
		return r
	}, strings.ToUpper(code))
}

func cleanName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "", errors.New("укажите имя")
	}
	if utf8.RuneCountInString(name) > maxNameRunes {
		return "", errors.New("имя слишком длинное")
	}
	return name, nil
}

func timestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func (a *auth) cookie(r *http.Request, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookie,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		// Behind a TLS reverse proxy (Caddy, nginx, a VPN's own HTTPS, ...)
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
