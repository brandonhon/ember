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
	"log/slog"
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

// DefaultSessionTTL is the idle timeout of a session: the maximum gap between
// requests before it expires. Each authenticated request slides the deadline
// forward (see VerifySession), so an active reader stays logged in; only
// inactivity for a full TTL logs them out. Operators override via
// EMBER_SESSION_TTL. 24h matches "log in once per day" expectations and keeps
// the stolen-cookie idle window small.
const DefaultSessionTTL = 24 * time.Hour

// DefaultMaxSessionLifetime caps how long a session may be renewed. Even a
// continuously-active user must re-authenticate once a session reaches this
// age (measured from its original login), bounding the blast radius of a
// stolen persistent cookie. 30 days is a normal "remember me" horizon and
// stays well under MaxSessionTTL. Not operator-configurable by design.
const DefaultMaxSessionLifetime = 30 * 24 * time.Hour

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
	// SessionTTL is the idle timeout for a session — how long it survives
	// between requests before expiring. Defaults to DefaultSessionTTL;
	// main.go can override from EMBER_SESSION_TTL. Access through the lock
	// (see EffectiveSessionTTL).
	SessionTTL time.Duration
}

// EffectiveSessionTTL reads SessionTTL under the read lock. All callers
// outside Auth itself MUST use this rather than touching the SessionTTL
// field directly — direct reads race SetSessionTTL writes.
func (a *Auth) EffectiveSessionTTL() time.Duration {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.SessionTTL
}

// SetSessionTTL adjusts the active idle timeout. Affects the deadline written
// to newly-issued and renewed sessions; existing DB rows keep their current
// expires_at until their next renewal, natural expiry, or sweep.
//
// The securecookie MaxAge guard is intentionally NOT tied to this value — it
// stays fixed at MaxSessionTTL so a renewed cookie keeps decoding across a
// long idle window (see New). The DB expires_at is the real validity gate.
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
	// Fix the signature-age ceiling at the absolute maximum, not the idle
	// TTL: sessions slide via re-encoded cookies, and a long admin-set idle
	// window must not outlive the securecookie age check. The sessions-table
	// expires_at remains the authoritative validity gate.
	sc.MaxAge(int(MaxSessionTTL.Seconds()))
	return &Auth{
		Store:         st,
		Cookie:        sc,
		Params:        DefaultParams,
		Now:           time.Now,
		SecureCookies: true,
		SessionTTL:    DefaultSessionTTL,
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
	// Shift the user's login timestamps so the unread window can anchor on the
	// previous visit. Best-effort: a failure here must not block login.
	if err := a.Store.RecordLogin(ctx, userID); err != nil {
		slog.Default().Warn("record login time failed", "user_id", userID, "err", err)
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
// session row. On success it slides the session's idle deadline forward (see
// maybeRenew), writing a refreshed cookie to w, and returns the user.
//
// w may be nil for read-only checks that don't want to issue a renewed cookie
// (renewal is simply skipped in that case).
func (a *Auth) VerifySession(ctx context.Context, w http.ResponseWriter, r *http.Request) (models.User, error) {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return models.User{}, ErrSession
	}
	var sessionID string
	if err := a.Cookie.Decode(CookieName, cookie.Value, &sessionID); err != nil {
		return models.User{}, ErrSession
	}
	var userID, createdAt, expiresAt int64
	err = a.Store.DB.QueryRowContext(ctx, `
		SELECT user_id, created_at, expires_at FROM sessions WHERE id = ?`, sessionID).Scan(&userID, &createdAt, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return models.User{}, ErrSession
	}
	if err != nil {
		return models.User{}, err
	}
	now := a.Now()
	if now.Unix() >= expiresAt {
		// Best-effort cleanup; ignore errors.
		_, _ = a.Store.DB.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sessionID)
		return models.User{}, ErrSession
	}
	if w != nil {
		a.maybeRenew(ctx, w, sessionID, createdAt, expiresAt, now)
	}
	return a.Store.GetUser(ctx, userID)
}

// maybeRenew slides an active session's idle deadline forward, capped at the
// absolute lifetime measured from original login. It is best-effort: any
// failure leaves the still-valid session untouched and is logged, never
// surfaced to the caller.
//
// Writes are throttled to the second half of each idle window so a busy
// session costs at most ~2 UPDATE + Set-Cookie per day rather than one per
// request, while still refreshing the browser's persistent cookie long before
// it lapses.
func (a *Auth) maybeRenew(ctx context.Context, w http.ResponseWriter, sessionID string, createdAt, expiresAt int64, now time.Time) {
	idle := a.EffectiveSessionTTL()
	// Absolute ceiling from original login. Never cap below the configured
	// idle window (guards the odd case of an admin idle TTL exceeding 30d).
	maxLife := DefaultMaxSessionLifetime
	if idle > maxLife {
		maxLife = idle
	}
	absoluteDeadline := createdAt + int64(maxLife.Seconds())

	// Only renew once past the idle window's halfway mark.
	if expiresAt-now.Unix() >= int64(idle.Seconds())/2 {
		return
	}
	newExpiry := now.Add(idle).Unix()
	if newExpiry > absoluteDeadline {
		newExpiry = absoluteDeadline
	}
	if newExpiry <= expiresAt {
		// Already pinned to the absolute ceiling; nothing to extend.
		return
	}
	if _, err := a.Store.DB.ExecContext(ctx,
		`UPDATE sessions SET expires_at = ? WHERE id = ?`, newExpiry, sessionID); err != nil {
		slog.Default().Warn("session renewal failed", "session", sessionID, "err", err)
		return
	}
	// Refresh the cookie so the browser's persistent-cookie expiry tracks the
	// renewed server deadline; re-encoding also resets the securecookie
	// timestamp so the signed value keeps decoding.
	encoded, err := a.Cookie.Encode(CookieName, sessionID)
	if err != nil {
		slog.Default().Warn("session cookie re-encode failed", "session", sessionID, "err", err)
		return
	}
	exp := time.Unix(newExpiry, 0)
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    encoded,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.SecureCookies,
		SameSite: http.SameSiteStrictMode,
		Expires:  exp,
		MaxAge:   int(exp.Sub(now).Seconds()),
	})
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
		u, err := a.VerifySession(r.Context(), w, r)
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
		u, err := a.VerifySession(r.Context(), w, r)
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
	// Enforce the same 8-char floor as the create-user/change-password paths so
	// the first-run admin can't be seeded with a trivially weak password.
	if len(password) < 8 {
		return models.User{}, false, errors.New("auth: EMBER_ADMIN_PASSWORD must be at least 8 characters")
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
