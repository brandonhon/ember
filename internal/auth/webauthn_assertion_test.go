package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

// These tests drive a real ECDSA-signed assertion through ValidateLogin rather
// than stopping at the ceremony bookkeeping. Only a genuine signature exercises
// the signature-counter path, which is where clone detection lives.

const testOrigin = "https://ember.test"
const testRPID = "ember.test"

// fakeAuthenticator is a minimal WebAuthn authenticator: a P-256 key pair, a
// credential ID, and a signature counter it controls.
type fakeAuthenticator struct {
	key    *ecdsa.PrivateKey
	credID []byte
}

func newFakeAuthenticator(t *testing.T) *fakeAuthenticator {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	credID := make([]byte, 16)
	if _, err := rand.Read(credID); err != nil {
		t.Fatal(err)
	}
	return &fakeAuthenticator{key: key, credID: credID}
}

// coseKey returns the credential's public key in the COSE_Key encoding the
// stored passkey row holds.
func (f *fakeAuthenticator) coseKey(t *testing.T) []byte {
	t.Helper()
	// Bytes() yields the uncompressed SEC 1 point 0x04 || X(32) || Y(32); the
	// COSE encoding wants the two coordinates separately.
	point, err := f.key.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if len(point) != 65 {
		t.Fatalf("unexpected P-256 point length %d", len(point))
	}
	k := webauthncose.EC2PublicKeyData{
		PublicKeyData: webauthncose.PublicKeyData{
			KeyType:   int64(webauthncose.EllipticKey),
			Algorithm: int64(webauthncose.AlgES256),
		},
		Curve:  int64(webauthncose.P256),
		XCoord: point[1:33],
		YCoord: point[33:65],
	}
	raw, err := cbor.Marshal(k)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// assert produces a signed assertion response body for the given challenge and
// signature counter, in the JSON shape the browser posts back.
func (f *fakeAuthenticator) assert(t *testing.T, challenge string, counter uint32, userID int64) []byte {
	t.Helper()

	clientData, err := json.Marshal(map[string]any{
		"type": challengeTypeGet, "challenge": challenge, "origin": testOrigin, "crossOrigin": false,
	})
	if err != nil {
		t.Fatal(err)
	}

	// authenticatorData = SHA256(rpID) || flags || signCount.
	// Flags UP|UV; backup-eligible and backup-state stay clear to match the
	// stored credential (ValidateLogin rejects a mismatch).
	rpIDHash := sha256.Sum256([]byte(testRPID))
	authData := make([]byte, 0, 37)
	authData = append(authData, rpIDHash[:]...)
	authData = append(authData, 0x01|0x04)
	authData = binary.BigEndian.AppendUint32(authData, counter)

	// The signature covers authenticatorData || SHA256(clientDataJSON).
	cdHash := sha256.Sum256(clientData)
	signed := append(append([]byte{}, authData...), cdHash[:]...)
	digest := sha256.Sum256(signed)
	sig, err := ecdsa.SignASN1(rand.Reader, f.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}

	b64 := base64.RawURLEncoding.EncodeToString
	body, err := json.Marshal(map[string]any{
		"id":    b64(f.credID),
		"rawId": b64(f.credID),
		"type":  "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64(clientData),
			"authenticatorData": b64(authData),
			"signature":         b64(sig),
			"userHandle":        b64([]byte(strconv.FormatInt(userID, 10))),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

const challengeTypeGet = "webauthn.get"

// assertionFixture wires a user with one registered passkey whose stored sign
// count is storedCount, and returns everything needed to drive FinishLogin.
type assertionFixture struct {
	wa   *WebAuthn
	st   *store.Store
	user models.User
	auth *fakeAuthenticator
	pk   models.Passkey
}

func newAssertionFixture(t *testing.T, storedCount uint32) *assertionFixture {
	t.Helper()
	st := store.NewTest(t)
	w, err := NewWebAuthn(st, testOrigin, "Ember Test")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	u, err := st.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "x"})
	if err != nil {
		t.Fatal(err)
	}
	fa := newFakeAuthenticator(t)
	pk, err := st.InsertPasskey(ctx, models.Passkey{
		UserID:       u.ID,
		CredentialID: fa.credID,
		PublicKey:    fa.coseKey(t),
		// aaguid is NOT NULL; a nil slice trips the constraint.
		AAGUID:    []byte{},
		SignCount: storedCount,
		Name:      "Test Key",
	})
	if err != nil {
		t.Fatal(err)
	}
	return &assertionFixture{wa: w, st: st, user: u, auth: fa, pk: pk}
}

// begin starts a real login ceremony and returns the session ID plus the
// challenge the authenticator must sign.
func (f *assertionFixture) begin(t *testing.T, requireUV bool) (string, string) {
	t.Helper()
	ctx := context.Background()
	_, sid, err := f.wa.BeginLogin(ctx, f.user, requireUV)
	if err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := f.st.DB.QueryRowContext(ctx,
		`SELECT data FROM webauthn_sessions WHERE id = ?`, sid).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var sd struct {
		Challenge string `json:"challenge"`
	}
	if err := json.Unmarshal(raw, &sd); err != nil {
		t.Fatal(err)
	}
	return sid, sd.Challenge
}

func (f *assertionFixture) storedSignCount(t *testing.T) uint32 {
	t.Helper()
	pk, err := f.st.GetPasskeyByCredentialID(context.Background(), f.auth.credID)
	if err != nil {
		t.Fatal(err)
	}
	return pk.SignCount
}

// Baseline: the harness produces an assertion the library actually accepts.
// Without this passing, the rejection tests below would prove nothing — they'd
// pass for any reason at all.
func TestFinishLogin_AcceptsValidAssertion(t *testing.T) {
	f := newAssertionFixture(t, 5)
	sid, challenge := f.begin(t, false)

	got, err := f.wa.FinishLogin(context.Background(), sid,
		f.auth.assert(t, challenge, 6, f.user.ID))
	if err != nil {
		t.Fatalf("valid assertion rejected: %v", err)
	}
	if got.ID != f.user.ID {
		t.Errorf("authenticated user %d, want %d", got.ID, f.user.ID)
	}
	if n := f.storedSignCount(t); n != 6 {
		t.Errorf("stored sign count = %d, want 6", n)
	}
}

// A counter that fails to advance is the spec's clone signal. The library only
// flags it, so ember must refuse the assertion itself.
func TestFinishLogin_RejectsNonAdvancingSignCount(t *testing.T) {
	for _, tc := range []struct {
		name    string
		counter uint32
	}{
		{"replayed same counter", 5},
		{"regressed counter", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAssertionFixture(t, 5)
			sid, challenge := f.begin(t, false)

			_, err := f.wa.FinishLogin(context.Background(), sid,
				f.auth.assert(t, challenge, tc.counter, f.user.ID))
			if !errors.Is(err, ErrClonedAuthenticator) {
				t.Fatalf("got %v, want ErrClonedAuthenticator", err)
			}
			// The stored high-water mark must survive: writing the lower value
			// back would make the next replay look like an advance.
			if n := f.storedSignCount(t); n != 5 {
				t.Errorf("stored sign count = %d after rejection, want 5 (high-water mark overwritten)", n)
			}
		})
	}
}

// Authenticators that never implement a counter report 0 every time. That is
// explicitly not a clone signal, and rejecting it would lock out most platform
// passkeys.
func TestFinishLogin_AllowsAlwaysZeroCounter(t *testing.T) {
	f := newAssertionFixture(t, 0)
	sid, challenge := f.begin(t, false)

	if _, err := f.wa.FinishLogin(context.Background(), sid,
		f.auth.assert(t, challenge, 0, f.user.ID)); err != nil {
		t.Fatalf("zero-counter authenticator rejected: %v", err)
	}
}

// The user-verification requirement chosen at begin is carried in the stored
// session, so a browser cannot negotiate it away at finish.
func TestFinishLogin_UserVerificationRequirementIsEnforced(t *testing.T) {
	f := newAssertionFixture(t, 5)
	ctx := context.Background()

	// Confirm the requirement actually reaches the persisted session data,
	// which is what ValidateLogin consults.
	_, sid, err := f.wa.BeginLogin(ctx, f.user, true)
	if err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := f.st.DB.QueryRowContext(ctx,
		`SELECT data FROM webauthn_sessions WHERE id = ?`, sid).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var sd struct {
		UserVerification string `json:"userVerification"`
	}
	if err := json.Unmarshal(raw, &sd); err != nil {
		t.Fatal(err)
	}
	if sd.UserVerification != "required" {
		t.Errorf("stored userVerification = %q, want %q", sd.UserVerification, "required")
	}

	// And the default ceremony stays at "preferred" rather than empty.
	_, sid2, err := f.wa.BeginLogin(ctx, f.user, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.DB.QueryRowContext(ctx,
		`SELECT data FROM webauthn_sessions WHERE id = ?`, sid2).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	sd.UserVerification = ""
	if err := json.Unmarshal(raw, &sd); err != nil {
		t.Fatal(err)
	}
	if sd.UserVerification != "preferred" {
		t.Errorf("default userVerification = %q, want %q", sd.UserVerification, "preferred")
	}
}
