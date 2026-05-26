package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
	wa "github.com/go-webauthn/webauthn/webauthn"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
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
	})
	if err != nil {
		return nil, err
	}
	return &WebAuthn{Web: web, Store: st}, nil
}

// waUser adapts an ember user + their stored passkeys to the webauthn.User
// interface required by the library.
type waUser struct {
	user        models.User
	credentials []wa.Credential
}

func (u *waUser) WebAuthnID() []byte {
	// Stable per-user handle. The library expects bytes — encode the int ID.
	return []byte(fmt.Sprintf("%d", u.user.ID))
}

func (u *waUser) WebAuthnName() string                  { return u.user.Username }
func (u *waUser) WebAuthnDisplayName() string           { return u.user.Username }
func (u *waUser) WebAuthnCredentials() []wa.Credential  { return u.credentials }

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
		UserID:  nullInt64(u.ID),
		Data:    sd,
		Purpose: "register",
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
// persists a new passkey for the user. The caller supplies a friendly name.
func (w *WebAuthn) FinishRegister(ctx context.Context, sessionID, name string, raw []byte) (models.Passkey, error) {
	sess, err := w.Store.TakeWebAuthnSession(ctx, sessionID)
	if err != nil {
		return models.Passkey{}, err
	}
	if sess.Purpose != "register" || !sess.UserID.Valid {
		return models.Passkey{}, errors.New("webauthn: wrong session")
	}
	user, err := w.Store.GetUser(ctx, sess.UserID.Int64)
	if err != nil {
		return models.Passkey{}, err
	}
	wu, err := w.loadUser(ctx, user)
	if err != nil {
		return models.Passkey{}, err
	}
	var sd wa.SessionData
	if err := json.Unmarshal(sess.Data, &sd); err != nil {
		return models.Passkey{}, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBody(strings.NewReader(string(raw)))
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
func (w *WebAuthn) BeginLogin(ctx context.Context, u models.User) ([]byte, string, error) {
	wu, err := w.loadUser(ctx, u)
	if err != nil {
		return nil, "", err
	}
	if len(wu.credentials) == 0 {
		return nil, "", errors.New("webauthn: user has no passkeys")
	}
	options, sessionData, err := w.Web.BeginLogin(wu)
	if err != nil {
		return nil, "", err
	}
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
		UserID:  nullInt64(u.ID),
		Data:    sd,
		Purpose: "login",
	}); err != nil {
		return nil, "", err
	}
	out, err := json.Marshal(options)
	if err != nil {
		return nil, "", err
	}
	return out, sid, nil
}

// FinishLogin verifies the assertion and returns the authenticated user. On
// success the passkey's sign count is updated and the ceremony row consumed.
func (w *WebAuthn) FinishLogin(ctx context.Context, sessionID string, raw []byte) (models.User, error) {
	sess, err := w.Store.TakeWebAuthnSession(ctx, sessionID)
	if err != nil {
		return models.User{}, err
	}
	if sess.Purpose != "login" || !sess.UserID.Valid {
		return models.User{}, errors.New("webauthn: wrong session")
	}
	user, err := w.Store.GetUser(ctx, sess.UserID.Int64)
	if err != nil {
		return models.User{}, err
	}
	wu, err := w.loadUser(ctx, user)
	if err != nil {
		return models.User{}, err
	}
	var sd wa.SessionData
	if err := json.Unmarshal(sess.Data, &sd); err != nil {
		return models.User{}, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBody(strings.NewReader(string(raw)))
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
	if err := w.Store.UpdatePasskeyOnUse(ctx, pk.ID, cred.Authenticator.SignCount); err != nil {
		return models.User{}, err
	}
	return user, nil
}

func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func nullInt64(v int64) (n sqlNullInt64) {
	n.Int64 = v
	n.Valid = true
	return
}

// sqlNullInt64 alias to avoid importing sql in callers that just need the
// helper. Same shape as sql.NullInt64.
type sqlNullInt64 = struct {
	Int64 int64
	Valid bool
}
