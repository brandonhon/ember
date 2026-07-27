package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
	wa "github.com/go-webauthn/webauthn/webauthn"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

// Ceremony kinds, stored on the webauthn_sessions row so a register session
// can never be spent finishing a login (or vice versa).
const (
	purposeRegister = "register"
	purposeLogin    = "login"
)

// WebAuthn wraps the go-webauthn library with ember's storage. The relying
// party config is derived from a public-facing origin URL (e.g. the value of
// EMBER_PUBLIC_URL or the request host).
type WebAuthn struct {
	Web   *wa.WebAuthn
	Store *store.Store
}

// NewWebAuthn builds a WebAuthn helper. publicURL is the canonical origin
// users hit (scheme://host[:port]). displayName is shown in the platform UI.
func NewWebAuthn(st *store.Store, publicURL, displayName string) (*WebAuthn, error) {
	if publicURL == "" {
		return nil, errors.New("auth: webauthn requires a public URL")
	}
	u, err := url.Parse(publicURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("auth: invalid public URL %q", publicURL)
	}
	host := u.Hostname()
	origin := strings.TrimRight(u.Scheme+"://"+u.Host, "/")
	web, err := wa.New(&wa.Config{
		RPDisplayName: displayName,
		RPID:          host,
		RPOrigins:     []string{origin},
		// State the user-verification stance explicitly rather than inheriting
		// the empty value. "preferred" is what an unset field already resolves
		// to in the browser, so this changes no behaviour — it makes the
		// posture a decision in the code instead of an accident, and gives
		// RequireUserVerification something to override.
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			UserVerification: protocol.VerificationPreferred,
		},
	})
	if err != nil {
		return nil, err
	}
	return &WebAuthn{Web: web, Store: st}, nil
}

// ErrClonedAuthenticator is returned when an assertion's signature counter did
// not advance past the stored value. Per the WebAuthn spec that is the signal
// of a duplicated authenticator — someone replaying or having extracted the
// credential — so the assertion is refused rather than merely noted.
var ErrClonedAuthenticator = errors.New("webauthn: authenticator signature counter did not advance")

// waUser adapts an ember user + their stored passkeys to the webauthn.User
// interface required by the library.
type waUser struct {
	user        models.User
	credentials []wa.Credential
}

func (u *waUser) WebAuthnID() []byte {
	// Stable per-user handle. The library expects bytes — encode the int ID.
	return []byte(strconv.FormatInt(u.user.ID, 10))
}

func (u *waUser) WebAuthnName() string                 { return u.user.Username }
func (u *waUser) WebAuthnDisplayName() string          { return u.user.Username }
func (u *waUser) WebAuthnCredentials() []wa.Credential { return u.credentials }

// loadUser materializes the webauthn.User for the given account, including all
// of their currently-registered passkeys.
func (w *WebAuthn) loadUser(ctx context.Context, u models.User) (*waUser, error) {
	pks, err := w.Store.ListPasskeys(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	creds := make([]wa.Credential, 0, len(pks))
	for _, p := range pks {
		creds = append(creds, modelToCredential(p))
	}
	return &waUser{user: u, credentials: creds}, nil
}

func modelToCredential(p models.Passkey) wa.Credential {
	var trs []protocol.AuthenticatorTransport
	for _, t := range strings.Split(p.Transports, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			trs = append(trs, protocol.AuthenticatorTransport(t))
		}
	}
	return wa.Credential{
		ID:              p.CredentialID,
		PublicKey:       p.PublicKey,
		AttestationType: p.AttestationTyp,
		Transport:       trs,
		Authenticator: wa.Authenticator{
			AAGUID:    p.AAGUID,
			SignCount: p.SignCount,
		},
		Flags: wa.CredentialFlags{
			BackupEligible: p.BackupEligible,
			BackupState:    p.BackupState,
		},
	}
}

// BeginRegister starts a registration ceremony. Returns the options JSON for
// the browser plus a session ID the client must echo back on finish.
func (w *WebAuthn) BeginRegister(ctx context.Context, u models.User) ([]byte, string, error) {
	wu, err := w.loadUser(ctx, u)
	if err != nil {
		return nil, "", err
	}
	options, sessionData, err := w.Web.BeginRegistration(wu)
	if err != nil {
		return nil, "", err
	}
	return w.startCeremony(ctx, u.ID, purposeRegister, options, sessionData)
}

// startCeremony persists an in-flight ceremony and returns the browser options
// plus the session ID the client echoes back on finish. Shared by both begin
// paths — they differ only in which library call produced the options.
func (w *WebAuthn) startCeremony(ctx context.Context, userID int64, purpose string, options any, sessionData *wa.SessionData) ([]byte, string, error) {
	sd, err := json.Marshal(sessionData)
	if err != nil {
		return nil, "", err
	}
	sid, err := randomID()
	if err != nil {
		return nil, "", err
	}
	if err := w.Store.PutWebAuthnSession(ctx, store.WebAuthnSession{
		ID:      sid,
		UserID:  sql.NullInt64{Int64: userID, Valid: true},
		Data:    sd,
		Purpose: purpose,
	}); err != nil {
		return nil, "", err
	}
	out, err := json.Marshal(options)
	if err != nil {
		return nil, "", err
	}
	return out, sid, nil
}

// FinishRegister consumes the ceremony, parses the attestation response, and
// persists a new passkey for the user. callerID must match the session's
// stored user — prevents a logged-in user from finishing another user's
// ceremony by supplying a foreign session ID.
func (w *WebAuthn) FinishRegister(ctx context.Context, sessionID, name string, raw []byte, callerID int64) (models.Passkey, error) {
	user, wu, sd, err := w.takeCeremony(ctx, sessionID, purposeRegister, &callerID)
	if err != nil {
		return models.Passkey{}, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(raw))
	if err != nil {
		return models.Passkey{}, err
	}
	cred, err := w.Web.CreateCredential(wu, sd, parsed)
	if err != nil {
		return models.Passkey{}, err
	}
	trs := make([]string, 0, len(cred.Transport))
	for _, t := range cred.Transport {
		trs = append(trs, string(t))
	}
	if strings.TrimSpace(name) == "" {
		name = "Passkey"
	}
	return w.Store.InsertPasskey(ctx, models.Passkey{
		UserID:         user.ID,
		CredentialID:   cred.ID,
		PublicKey:      cred.PublicKey,
		AttestationTyp: cred.AttestationType,
		AAGUID:         cred.Authenticator.AAGUID,
		SignCount:      cred.Authenticator.SignCount,
		Transports:     strings.Join(trs, ","),
		BackupEligible: cred.Flags.BackupEligible,
		BackupState:    cred.Flags.BackupState,
		Name:           name,
	})
}

// BeginLogin starts an assertion ceremony bound to a specific user (the user
// types their username first, then is challenged for a passkey).
//
// requireUV escalates user verification from "preferred" to "required" for
// this ceremony. The requirement is written into the stored session data, so
// FinishLogin enforces whatever was demanded at begin — the browser cannot
// negotiate it down. Left off by default because a passkey enrolled on a
// hardware key with no PIN configured would stop working; an operator turns
// it on once every enrolled credential is known to verify.
func (w *WebAuthn) BeginLogin(ctx context.Context, u models.User, requireUV bool) ([]byte, string, error) {
	wu, err := w.loadUser(ctx, u)
	if err != nil {
		return nil, "", err
	}
	if len(wu.credentials) == 0 {
		return nil, "", errors.New("webauthn: user has no passkeys")
	}
	var opts []wa.LoginOption
	if requireUV {
		opts = append(opts, wa.WithUserVerification(protocol.VerificationRequired))
	}
	options, sessionData, err := w.Web.BeginLogin(wu, opts...)
	if err != nil {
		return nil, "", err
	}
	return w.startCeremony(ctx, u.ID, purposeLogin, options, sessionData)
}

// FinishLogin verifies the assertion and returns the authenticated user. On
// success the passkey's sign count is updated and the ceremony row consumed.
func (w *WebAuthn) FinishLogin(ctx context.Context, sessionID string, raw []byte) (models.User, error) {
	// nil = no caller binding: a login ceremony is finished by an as-yet-
	// unauthenticated browser, so there is no session user to match against.
	user, wu, sd, err := w.takeCeremony(ctx, sessionID, purposeLogin, nil)
	if err != nil {
		return models.User{}, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(raw))
	if err != nil {
		return models.User{}, err
	}
	cred, err := w.Web.ValidateLogin(wu, sd, parsed)
	if err != nil {
		return models.User{}, err
	}
	pk, err := w.Store.GetPasskeyByCredentialID(ctx, cred.ID)
	if err != nil {
		return models.User{}, err
	}
	if pk.UserID != user.ID {
		return models.User{}, errors.New("webauthn: credential / user mismatch")
	}
	// ValidateLogin records a counter regression as a flag instead of an error,
	// so an unchecked call happily authenticates a cloned credential. Refuse
	// it, and refuse it *before* UpdatePasskeyOnUse — writing the lower count
	// back would overwrite the stored high-water mark and leave the next
	// replay undetectable. Authenticators that always report 0 (most platform
	// passkeys) never set this flag, so they are unaffected.
	if cred.Authenticator.CloneWarning {
		return models.User{}, ErrClonedAuthenticator
	}
	if err := w.Store.UpdatePasskeyOnUse(ctx, pk.ID, cred.Authenticator.SignCount); err != nil {
		return models.User{}, err
	}
	return user, nil
}

// takeCeremony consumes a stored ceremony (single-use), enforces that it is of
// the expected kind, and rehydrates everything the finish step needs. When
// callerID is non-nil the ceremony must also belong to that user, which stops
// a signed-in user from finishing someone else's registration with a borrowed
// session ID. Pass nil only for login, where the browser isn't authenticated
// yet. A pointer rather than a 0 sentinel so an accidental zero caller ID
// fails the comparison (no real user has ID 0) instead of skipping the check.
func (w *WebAuthn) takeCeremony(ctx context.Context, sessionID, purpose string, callerID *int64) (models.User, *waUser, wa.SessionData, error) {
	var sd wa.SessionData
	sess, err := w.Store.TakeWebAuthnSession(ctx, sessionID)
	if err != nil {
		return models.User{}, nil, sd, err
	}
	if sess.Purpose != purpose || !sess.UserID.Valid {
		return models.User{}, nil, sd, errors.New("webauthn: wrong session")
	}
	if callerID != nil && sess.UserID.Int64 != *callerID {
		return models.User{}, nil, sd, errors.New("webauthn: session does not belong to caller")
	}
	user, err := w.Store.GetUser(ctx, sess.UserID.Int64)
	if err != nil {
		return models.User{}, nil, sd, err
	}
	wu, err := w.loadUser(ctx, user)
	if err != nil {
		return models.User{}, nil, sd, err
	}
	if err := json.Unmarshal(sess.Data, &sd); err != nil {
		return models.User{}, nil, sd, err
	}
	return user, wu, sd, nil
}

func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
