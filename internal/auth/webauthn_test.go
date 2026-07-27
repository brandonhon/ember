package auth

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

// newWebAuthn returns a WebAuthn helper over a fresh test store, plus the store
// so tests can seed users and passkeys.
func newWebAuthn(t *testing.T) (*WebAuthn, *store.Store) {
	t.Helper()
	s := store.NewTest(t)
	w, err := NewWebAuthn(s, "https://reader.example.com", "Ember")
	if err != nil {
		t.Fatalf("NewWebAuthn: %v", err)
	}
	return w, s
}

func seedUser(t *testing.T, s *store.Store, name string) models.User {
	t.Helper()
	u, err := s.CreateUser(context.Background(), models.User{
		Username: name, PasswordHash: "x", IsAdmin: false,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return u
}

// seedPasskey inserts a minimally-valid passkey row. AAGUID is NOT NULL in the
// schema, so it must be supplied even for a synthetic credential.
func seedPasskey(t *testing.T, s *store.Store, userID int64, credID, transports string) models.Passkey {
	t.Helper()
	pk, err := s.InsertPasskey(context.Background(), models.Passkey{
		UserID: userID, CredentialID: []byte(credID), PublicKey: []byte("pk"),
		AAGUID: make([]byte, 16), Transports: transports, Name: "test key",
	})
	if err != nil {
		t.Fatalf("InsertPasskey: %v", err)
	}
	return pk
}

// isCeremonyErr reports whether err is the specific ceremony guard rejection
// named by want. Guard tests must not settle for "any error": a malformed
// attestation body fails parsing further down, so `err != nil` alone would hold
// even if the guard were deleted entirely.
func isCeremonyErr(err error, want string) bool {
	return err != nil && strings.Contains(err.Error(), want)
}

func TestNewWebAuthn_URLValidation(t *testing.T) {
	s := store.NewTest(t)
	for _, tc := range []struct {
		name, url string
		wantErr   bool
	}{
		{"empty", "", true},
		{"no host", "https://", true},
		{"not a url", "://nope", true},
		{"https", "https://reader.example.com", false},
		{"with port", "http://localhost:8080", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewWebAuthn(s, tc.url, "Ember")
			if (err != nil) != tc.wantErr {
				t.Fatalf("NewWebAuthn(%q) err = %v, wantErr %v", tc.url, err, tc.wantErr)
			}
		})
	}
}

// The relying-party ID must be the bare hostname (no scheme, no port) while the
// allowed origin keeps both — get this wrong and every browser ceremony fails
// with an opaque origin mismatch.
func TestNewWebAuthn_RelyingPartyDerivedFromURL(t *testing.T) {
	s := store.NewTest(t)
	w, err := NewWebAuthn(s, "https://reader.example.com:8443/", "Ember")
	if err != nil {
		t.Fatalf("NewWebAuthn: %v", err)
	}
	if got := w.Web.Config.RPID; got != "reader.example.com" {
		t.Errorf("RPID = %q, want %q (hostname only, no port)", got, "reader.example.com")
	}
	if got := w.Web.Config.RPOrigins; len(got) != 1 || got[0] != "https://reader.example.com:8443" {
		t.Errorf("RPOrigins = %v, want [https://reader.example.com:8443]", got)
	}
}

func TestWAUser_InterfaceMapping(t *testing.T) {
	u := &waUser{user: models.User{ID: 42, Username: "alice"}}
	if got := string(u.WebAuthnID()); got != "42" {
		t.Errorf("WebAuthnID = %q, want %q", got, "42")
	}
	if got := u.WebAuthnName(); got != "alice" {
		t.Errorf("WebAuthnName = %q, want alice", got)
	}
	if got := u.WebAuthnDisplayName(); got != "alice" {
		t.Errorf("WebAuthnDisplayName = %q, want alice", got)
	}
	if got := u.WebAuthnCredentials(); len(got) != 0 {
		t.Errorf("WebAuthnCredentials = %v, want empty", got)
	}
}

// Transports round-trip through a comma-joined DB column; blanks and stray
// whitespace must not become empty transport entries.
func TestModelToCredential_TransportParsing(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"usb", []string{"usb"}},
		{"usb,nfc", []string{"usb", "nfc"}},
		{" usb , nfc ", []string{"usb", "nfc"}},
		{"usb,,nfc,", []string{"usb", "nfc"}},
	} {
		cred := modelToCredential(models.Passkey{Transports: tc.in, SignCount: 7})
		got := make([]string, 0, len(cred.Transport))
		for _, tr := range cred.Transport {
			got = append(got, string(tr))
		}
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("transports %q -> %v, want %v", tc.in, got, tc.want)
		}
		if cred.Authenticator.SignCount != 7 {
			t.Errorf("sign count not carried through for %q", tc.in)
		}
	}
}

func TestLoadUser_IncludesStoredPasskeys(t *testing.T) {
	w, s := newWebAuthn(t)
	ctx := context.Background()
	u := seedUser(t, s, "alice")
	seedPasskey(t, s, u.ID, "cred-1", "usb")
	wu, err := w.loadUser(ctx, u)
	if err != nil {
		t.Fatalf("loadUser: %v", err)
	}
	if len(wu.credentials) != 1 || string(wu.credentials[0].ID) != "cred-1" {
		t.Fatalf("credentials = %+v, want the one stored passkey", wu.credentials)
	}
}

// BeginRegister must persist a ceremony row keyed by the returned session id,
// tagged "register" and owned by the caller.
func TestBeginRegister_PersistsCeremony(t *testing.T) {
	w, s := newWebAuthn(t)
	ctx := context.Background()
	u := seedUser(t, s, "alice")

	opts, sid, err := w.BeginRegister(ctx, u)
	if err != nil {
		t.Fatalf("BeginRegister: %v", err)
	}
	if sid == "" {
		t.Fatal("empty session id")
	}
	var decoded map[string]any
	if err := json.Unmarshal(opts, &decoded); err != nil {
		t.Fatalf("options are not valid JSON: %v", err)
	}
	if _, ok := decoded["publicKey"]; !ok {
		t.Errorf("options missing publicKey: %v", decoded)
	}

	sess, err := s.TakeWebAuthnSession(ctx, sid)
	if err != nil {
		t.Fatalf("TakeWebAuthnSession: %v", err)
	}
	if sess.Purpose != "register" {
		t.Errorf("purpose = %q, want register", sess.Purpose)
	}
	if !sess.UserID.Valid || sess.UserID.Int64 != u.ID {
		t.Errorf("session user = %+v, want %d", sess.UserID, u.ID)
	}
}

// A user with no registered passkey has nothing to be challenged for; the
// ceremony must refuse rather than emit empty allowCredentials.
func TestBeginLogin_RequiresAnExistingPasskey(t *testing.T) {
	w, s := newWebAuthn(t)
	ctx := context.Background()
	u := seedUser(t, s, "alice")

	if _, _, err := w.BeginLogin(ctx, u, false); err == nil {
		t.Fatal("BeginLogin with no passkeys: want error, got nil")
	}

	seedPasskey(t, s, u.ID, "cred-1", "")
	_, sid, err := w.BeginLogin(ctx, u, false)
	if err != nil {
		t.Fatalf("BeginLogin after registering: %v", err)
	}
	sess, err := s.TakeWebAuthnSession(ctx, sid)
	if err != nil {
		t.Fatalf("TakeWebAuthnSession: %v", err)
	}
	if sess.Purpose != "login" {
		t.Errorf("purpose = %q, want login", sess.Purpose)
	}
}

// The ceremony guards are the security-relevant part of finish: a register
// session must not be usable to finish a login (or vice versa), and a session
// belonging to one user must not be finishable by another.
func TestFinishRegister_RejectsWrongPurposeAndForeignCaller(t *testing.T) {
	w, s := newWebAuthn(t)
	ctx := context.Background()
	alice := seedUser(t, s, "alice")
	mallory := seedUser(t, s, "mallory")

	// A login-purpose session cannot finish a registration.
	if _, _, err := w.BeginLogin(ctx, alice, false); err == nil {
		t.Fatal("precondition: BeginLogin should fail with no passkeys")
	}
	seedPasskey(t, s, alice.ID, "c1", "")
	_, loginSID, err := w.BeginLogin(ctx, alice, false)
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	if _, err := w.FinishRegister(ctx, loginSID, "k", []byte("{}"), alice.ID); !isCeremonyErr(err, "wrong session") {
		t.Errorf("FinishRegister on a login-purpose session: err = %v, want \"wrong session\"", err)
	}

	// A register session belonging to alice cannot be finished by mallory.
	_, regSID, err := w.BeginRegister(ctx, alice)
	if err != nil {
		t.Fatalf("BeginRegister: %v", err)
	}
	if _, err := w.FinishRegister(ctx, regSID, "k", []byte("{}"), mallory.ID); !isCeremonyErr(err, "does not belong to caller") {
		t.Errorf("FinishRegister by a foreign caller: err = %v, want \"does not belong to caller\"", err)
	}
}

// Caller binding must fail closed. No real user has ID 0, so a zero callerID
// (an unauthenticated or buggy caller) has to be rejected rather than treated
// as "no binding requested" — that distinction is why takeCeremony takes a
// pointer instead of using 0 as a sentinel.
func TestFinishRegister_ZeroCallerIDIsRejected(t *testing.T) {
	w, s := newWebAuthn(t)
	ctx := context.Background()
	u := seedUser(t, s, "alice")

	_, sid, err := w.BeginRegister(ctx, u)
	if err != nil {
		t.Fatalf("BeginRegister: %v", err)
	}
	if _, err := w.FinishRegister(ctx, sid, "k", []byte("{}"), 0); !isCeremonyErr(err, "does not belong to caller") {
		t.Errorf("FinishRegister with callerID 0: err = %v, want the caller-binding rejection "+
			"(binding must fail closed, not fall through to attestation parsing)", err)
	}
}

func TestFinishLogin_RejectsWrongPurpose(t *testing.T) {
	w, s := newWebAuthn(t)
	ctx := context.Background()
	u := seedUser(t, s, "alice")

	_, regSID, err := w.BeginRegister(ctx, u)
	if err != nil {
		t.Fatalf("BeginRegister: %v", err)
	}
	if _, err := w.FinishLogin(ctx, regSID, []byte("{}")); !isCeremonyErr(err, "wrong session") {
		t.Errorf("FinishLogin on a register-purpose session: err = %v, want \"wrong session\"", err)
	}
}

// Ceremony sessions are single-use: a replayed session id must not resolve.
func TestCeremonySessionIsSingleUse(t *testing.T) {
	w, s := newWebAuthn(t)
	ctx := context.Background()
	u := seedUser(t, s, "alice")

	_, sid, err := w.BeginRegister(ctx, u)
	if err != nil {
		t.Fatalf("BeginRegister: %v", err)
	}
	if _, err := s.TakeWebAuthnSession(ctx, sid); err != nil {
		t.Fatalf("first take: %v", err)
	}
	if _, err := s.TakeWebAuthnSession(ctx, sid); err == nil {
		t.Error("ceremony session was reusable; want single-use")
	}
}

func TestRandomID_UniqueAndHex(t *testing.T) {
	seen := make(map[string]bool, 64)
	for range 64 {
		id, err := randomID()
		if err != nil {
			t.Fatalf("randomID: %v", err)
		}
		if len(id) != 32 {
			t.Fatalf("randomID len = %d, want 32 hex chars", len(id))
		}
		if seen[id] {
			t.Fatalf("randomID collision on %q", id)
		}
		seen[id] = true
	}
}
