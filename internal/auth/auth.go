// Package auth provides password hashing (argon2id), session management
// (signed cookies backed by the sessions table), and chi middleware for
// requiring auth and admin.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/securecookie"
	"golang.org/x/crypto/argon2"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

// CookieName is the session cookie name. Lowercase, no underscores.
const CookieName = "ember_session"

// DefaultSessionTTL is the default lifetime of a fresh session. Operators
// override via EMBER_SESSION_TTL in cfg. 24h matches "log in once per day"
// expectations and is short enough that stolen cookies have a small window
// while staying long enough that the average reader doesn't bounce to login
// every visit.
const DefaultSessionTTL = 24 * time.Hour

// Params holds argon2id parameters. Defaults are interactive-friendly. Tests
// override these for speed.
type Params struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultParams are reasonable for a small self-hosted service.
var DefaultParams = Params{
	Memory:      64 * 1024,
	Iterations:  3,
	Parallelism: 2,
	SaltLength:  16,
	KeyLength:   32,
}

// ErrInvalidCredentials is returned for any login failure.
var ErrInvalidCredentials = errors.New("auth: invalid credentials")

// ErrSession is returned for tampered, expired, or unknown sessions.
var ErrSession = errors.New("auth: invalid session")

// Auth wires together store + cookie signing + argon2 params.
// MinSessionTTL and MaxSessionTTL bound the values accepted by
// SetSessionTTL — both the admin-UI handler and the boot-time
// app_settings loader go through the same validation. 5 minutes prevents
// foot-guns from typos; 90 days keeps the upper bound within the original
// hardcoded ceiling.
const (
	MinSessionTTL = 5 * time.Minute
	MaxSessionTTL = 90 * 24 * time.Hour
)

// ErrSessionTTLOutOfRange is returned by SetSessionTTL when the requested
// duration falls outside [MinSessionTTL, MaxSessionTTL]. Callers can
// surface this as a 400 to the admin UI or log + skip at boot.
var ErrSessionTTLOutOfRange = errors.New("auth: session TTL out of range")

type Auth struct {
	Store  *store.Store
	Cookie *securecookie.SecureCookie
	Params Params
	Now    func() time.Time
	// SecureCookies sets the Secure flag on issued cookies. Defaults to true.
	// Set to false in test mode where the server runs over plain HTTP.
	SecureCookies bool
	// mu guards SessionTTL. CreateSession reads it on every login while
	// the admin handler can write it via SetSessionTTL — without this lock
	// the access pattern is a formal data race under Go's memory model
	// (the race detector catches it during -race tests).
	mu sync.RWMutex
	// SessionTTL is how long a freshly-issued session remains valid. Defaults
	// to DefaultSessionTTL; main.go can override from EMBER_SESSION_TTL.
	// Access through the lock (see EffectiveSessionTTL).
	SessionTTL time.Duration
	// sc keeps a reference so SessionTTL changes after New() take effect on
	// the securecookie MaxAge guard.
	sc *securecookie.SecureCookie
}

// EffectiveSessionTTL reads SessionTTL under the read lock. All callers
// outside Auth itself MUST use this rather than touching the SessionTTL
// field directly — direct reads race SetSessionTTL writes.
func (a *Auth) EffectiveSessionTTL() time.Duration {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.SessionTTL
}

// SetSessionTTL adjusts the active session lifetime. Affects newly-issued
// cookies; existing sessions in the DB keep their original expires_at until
// they expire on their own or are swept.
//
// Returns ErrSessionTTLOutOfRange if d falls outside
// [MinSessionTTL, MaxSessionTTL]; the existing TTL is left untouched in
// that case.
func (a *Auth) SetSessionTTL(d time.Duration) error {
	if d < MinSessionTTL || d > MaxSessionTTL {
		return ErrSessionTTLOutOfRange
	}
	a.mu.Lock()
	a.SessionTTL = d
	a.mu.Unlock()
	if a.sc != nil {
		a.sc.MaxAge(int(d.Seconds()))
	}
	return nil
}

// New constructs an Auth instance. sessionKey must be at least 32 bytes. The
// store is required.
func New(st *store.Store, sessionKey string) (*Auth, error) {
	if len(sessionKey) < 32 {
		return nil, errors.New("auth: session key must be at least 32 bytes")
	}
	// Use the same key for both the hash and the (block) cipher. We do not
	// encrypt the value (just sign), so the second key argument is nil.
	sc := securecookie.New([]byte(sessionKey), nil)
	sc.MaxAge(int(DefaultSessionTTL.Seconds()))
	return &Auth{
		Store:         st,
		Cookie:        sc,
		Params:        DefaultParams,
		Now:           time.Now,
		SecureCookies: true,
		SessionTTL:    DefaultSessionTTL,
		sc:            sc,
	}, nil
}

// HashPassword returns an argon2id-encoded password string in the standard
// PHC-style format: `$argon2id$v=19$m=...,t=...,p=...$salt$hash`.
func (a *Auth) HashPassword(plain string) (string, error) {
	salt := make([]byte, a.Params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(plain), salt,
		a.Params.Iterations, a.Params.Memory, a.Params.Parallelism, a.Params.KeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, a.Params.Memory, a.Params.Iterations, a.Params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash)), nil
}

// VerifyPassword checks plain against an argon2id-encoded hash. Returns nil on
// match, ErrInvalidCredentials on mismatch or malformed input.
func (a *Auth) VerifyPassword(plain, encoded string) error {
	p, salt, hash, err := decodeArgon2id(encoded)
	if err != nil {
		return ErrInvalidCredentials
	}
	got := argon2.IDKey([]byte(plain), salt, p.Iterations, p.Memory, p.Parallelism, p.KeyLength)
	if subtle.ConstantTimeCompare(got, hash) != 1 {
		return ErrInvalidCredentials
	}
	return nil
}

// equalizeTiming runs a throwaway argon2id derivation with the live cost
// params so the user-not-found path costs the same as a real VerifyPassword.
// The result is discarded — only the elapsed time matters.
func (a *Auth) equalizeTiming(password string) {
	var salt [16]byte // fixed; the derivation is never compared, only timed
	_ = argon2.IDKey([]byte(password), salt[:],
		a.Params.Iterations, a.Params.Memory, a.Params.Parallelism, a.Params.KeyLength)
}

func decodeArgon2id(encoded string) (Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return Params{}, nil, nil, errors.New("not argon2id")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Params{}, nil, nil, errors.New("argon2id version mismatch")
	}
	var p Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Iterations, &p.Parallelism); err != nil {
		return Params{}, nil, nil, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return Params{}, nil, nil, err
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return Params{}, nil, nil, err
	}
	p.SaltLength = uint32(len(salt))
	p.KeyLength = uint32(len(hash))
	return p, salt, hash, nil
}

// CreateSession inserts a session row and writes a signed cookie identifying
// it to the response.
func (a *Auth) CreateSession(ctx context.Context, w http.ResponseWriter, r *http.Request, userID int64) (models.Session, error) {
	idBytes := make([]byte, 32)
	if _, err := rand.Read(idBytes); err != nil {
		return models.Session{}, err
	}
	sessionID := hex.EncodeToString(idBytes)
	now := a.Now()
	// Snapshot the TTL under the read lock once, then reuse the value
	// across the three places it's needed (DB row, cookie Expires, cookie
	// MaxAge). Avoids triple-locking and keeps the row + cookie consistent
	// even if SetSessionTTL fires mid-call.
	ttl := a.EffectiveSessionTTL()
	sess := models.Session{
		ID:        sessionID,
		UserID:    userID,
		CreatedAt: now.Unix(),
		ExpiresAt: now.Add(ttl).Unix(),
		UserAgent: r.UserAgent(),
	}
	if _, err := a.Store.DB.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, created_at, expires_at, user_agent)
		VALUES (?, ?, ?, ?, ?)`,
		sess.ID, sess.UserID, sess.CreatedAt, sess.ExpiresAt, sess.UserAgent); err != nil {
		return models.Session{}, err
	}
	encoded, err := a.Cookie.Encode(CookieName, sessionID)
	if err != nil {
		return models.Session{}, err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    encoded,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.SecureCookies,
		SameSite: http.SameSiteStrictMode,
		Expires:  now.Add(ttl),
		MaxAge:   int(ttl.Seconds()),
	})
	return sess, nil
}

// VerifySession reads the cookie, validates the signature, and looks up the
// session row. Returns the user on success.
func (a *Auth) VerifySession(ctx context.Context, r *http.Request) (models.User, error) {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return models.User{}, ErrSession
	}
	var sessionID string
	if err := a.Cookie.Decode(CookieName, cookie.Value, &sessionID); err != nil {
		return models.User{}, ErrSession
	}
	var userID, expiresAt int64
	err = a.Store.DB.QueryRowContext(ctx, `
		SELECT user_id, expires_at FROM sessions WHERE id = ?`, sessionID).Scan(&userID, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return models.User{}, ErrSession
	}
	if err != nil {
		return models.User{}, err
	}
	if a.Now().Unix() >= expiresAt {
		// Best-effort cleanup; ignore errors.
		_, _ = a.Store.DB.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sessionID)
		return models.User{}, ErrSession
	}
	return a.Store.GetUser(ctx, userID)
}

// DestroySession deletes the current session row and clears the cookie.
func (a *Auth) DestroySession(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	cookie, err := r.Cookie(CookieName)
	if err == nil {
		var sessionID string
		if a.Cookie.Decode(CookieName, cookie.Value, &sessionID) == nil {
			if _, err := a.Store.DB.ExecContext(ctx,
				`DELETE FROM sessions WHERE id = ?`, sessionID); err != nil {
				return err
			}
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.SecureCookies,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	return nil
}

// CleanupExpiredSessions deletes session rows whose expiry has passed.
func (a *Auth) CleanupExpiredSessions(ctx context.Context) (int64, error) {
	res, err := a.Store.DB.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at <= ?`, a.Now().Unix())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// Context key type to avoid collisions.
type ctxKey int

const userCtxKey ctxKey = 1

// FromContext returns the authenticated user attached by RequireAuth.
func FromContext(ctx context.Context) (models.User, bool) {
	u, ok := ctx.Value(userCtxKey).(models.User)
	return u, ok
}

// withUser stores the user on a context. Exported only for tests.
func withUser(ctx context.Context, u models.User) context.Context {
	return context.WithValue(ctx, userCtxKey, u)
}

// RequireAuth returns chi middleware that requires a valid session.
func (a *Auth) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := a.VerifySession(r.Context(), r)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), u)))
	})
}

// RequireAdmin returns chi middleware that requires an authenticated admin.
func (a *Auth) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := a.VerifySession(r.Context(), r)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !u.IsAdmin {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), u)))
	})
}

// BootstrapAdmin creates the first admin user from the configured credentials
// when the users table is empty. Returns the user (zero value when nothing was
// done) and a boolean indicating whether a user was created.
func (a *Auth) BootstrapAdmin(ctx context.Context, username, password string) (models.User, bool, error) {
	n, err := a.Store.CountUsers(ctx)
	if err != nil {
		return models.User{}, false, err
	}
	if n > 0 {
		return models.User{}, false, nil
	}
	if username == "" || password == "" {
		return models.User{}, false, errors.New("auth: bootstrap admin requires EMBER_ADMIN_USER and EMBER_ADMIN_PASSWORD")
	}
	hash, err := a.HashPassword(password)
	if err != nil {
		return models.User{}, false, err
	}
	u, err := a.Store.CreateUser(ctx, models.User{
		Username:     username,
		PasswordHash: hash,
		IsAdmin:      true,
	})
	if err != nil {
		return models.User{}, false, err
	}
	return u, true, nil
}

// Login is a convenience: verify credentials and create a session. Destroys
// any prior session cookie on the request first (session-fixation defense
// in depth — the cookie value is signed + random so pre-planting one is
// already infeasible, but this prevents any inherited state from carrying
// across the login boundary).
func (a *Auth) Login(ctx context.Context, w http.ResponseWriter, r *http.Request, username, password string) (models.User, error) {
	u, err := a.Store.GetUserByUsername(ctx, username)
	if errors.Is(err, store.ErrNotFound) {
		// Equalize timing with the found-user path: without a throwaway argon2
		// derivation, a missing username returns in ~1ms while a real one takes
		// ~100ms, leaking account existence via a timing side channel.
		a.equalizeTiming(password)
		return models.User{}, ErrInvalidCredentials
	}
	if err != nil {
		return models.User{}, err
	}
	if err := a.VerifyPassword(password, u.PasswordHash); err != nil {
		return models.User{}, ErrInvalidCredentials
	}
	_ = a.DestroySession(ctx, w, r)
	if _, err := a.CreateSession(ctx, w, r, u.ID); err != nil {
		return models.User{}, err
	}
	return u, nil
}

// DeleteUserSessions removes every session row for a user. Called after a
// password change so any other browser/tab carrying the old credentials
// gets logged out.
func (a *Auth) DeleteUserSessions(ctx context.Context, userID int64) error {
	_, err := a.Store.DB.ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}
