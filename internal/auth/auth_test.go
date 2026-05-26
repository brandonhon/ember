package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

const testKey = "0123456789abcdef0123456789abcdef" // 32 bytes

func newAuth(t *testing.T) *Auth {
	t.Helper()
	s := store.NewTest(t)
	a, err := New(s, testKey)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Speed up argon2 for tests.
	a.Params = Params{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16}
	return a
}

func TestNew_RejectsShortKey(t *testing.T) {
	s := store.NewTest(t)
	if _, err := New(s, "too-short"); err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestHashAndVerify(t *testing.T) {
	a := newAuth(t)
	plain := "correcthorsebatterystaple"
	enc, err := a.HashPassword(plain)
	if err != nil {
		t.Fatal(err)
	}
	if enc == plain {
		t.Error("encoded == plaintext")
	}
	if !strings.HasPrefix(enc, "$argon2id$") {
		t.Errorf("encoded missing prefix: %q", enc)
	}
	if err := a.VerifyPassword(plain, enc); err != nil {
		t.Errorf("good password did not verify: %v", err)
	}
	if err := a.VerifyPassword("wrong", enc); err == nil {
		t.Error("wrong password verified OK")
	}
	if err := a.VerifyPassword(plain, "not-a-hash"); err == nil {
		t.Error("malformed hash accepted")
	}
}

func TestHash_ParamsHonored(t *testing.T) {
	a := newAuth(t)
	a.Params.Iterations = 7
	enc, err := a.HashPassword("x")
	if err != nil {
		t.Fatal(err)
	}
	// PHC string contains t=7
	if !strings.Contains(enc, "t=7") {
		t.Errorf("iterations not encoded: %q", enc)
	}
	// Round-trip with same params.
	if err := a.VerifyPassword("x", enc); err != nil {
		t.Errorf("verify with custom params: %v", err)
	}
}

func TestSession_CreateVerifyDestroy(t *testing.T) {
	a := newAuth(t)
	ctx := context.Background()
	u, _ := a.Store.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "x"})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("User-Agent", "test/1.0")

	sess, err := a.CreateSession(ctx, w, r, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sess.UserID != u.ID {
		t.Errorf("session userid = %d", sess.UserID)
	}

	cookie := extractCookie(t, w)
	if cookie == nil {
		t.Fatal("no cookie set")
	}

	// Verify on a new request carrying that cookie.
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.AddCookie(cookie)
	got, err := a.VerifySession(ctx, r2)
	if err != nil {
		t.Fatalf("VerifySession: %v", err)
	}
	if got.ID != u.ID {
		t.Errorf("verify returned user %d, want %d", got.ID, u.ID)
	}

	// Destroy.
	wDel := httptest.NewRecorder()
	if err := a.DestroySession(ctx, wDel, r2); err != nil {
		t.Fatal(err)
	}
	// After destroy, verify must fail.
	if _, err := a.VerifySession(ctx, r2); err == nil {
		t.Error("session still valid after destroy")
	}
}

func TestSession_TamperedCookieRejected(t *testing.T) {
	a := newAuth(t)
	ctx := context.Background()
	u, _ := a.Store.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "x"})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, err := a.CreateSession(ctx, w, r, u.ID); err != nil {
		t.Fatal(err)
	}
	cookie := extractCookie(t, w)

	// Tamper with the last byte.
	tampered := *cookie
	if len(tampered.Value) > 1 {
		buf := []byte(tampered.Value)
		buf[len(buf)-1] ^= 0xFF
		tampered.Value = string(buf)
	}

	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.AddCookie(&tampered)
	if _, err := a.VerifySession(ctx, r2); err == nil {
		t.Error("tampered cookie was accepted")
	}
}

func TestSession_ExpiredRejected(t *testing.T) {
	a := newAuth(t)
	ctx := context.Background()
	u, _ := a.Store.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "x"})

	// Issue, then fast-forward.
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, err := a.CreateSession(ctx, w, r, u.ID); err != nil {
		t.Fatal(err)
	}
	cookie := extractCookie(t, w)

	// Move the clock past the TTL. EffectiveSessionTTL reads under the
	// lock — direct a.SessionTTL access would race SetSessionTTL writes
	// when -race is enabled (production matters even though this test
	// is single-goroutine).
	a.Now = func() time.Time { return time.Now().Add(a.EffectiveSessionTTL() + time.Hour) }
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.AddCookie(cookie)
	if _, err := a.VerifySession(ctx, r2); err == nil {
		t.Error("expired session accepted")
	}
}

func TestRequireAuth_RejectsAndAccepts(t *testing.T) {
	a := newAuth(t)
	ctx := context.Background()
	u, _ := a.Store.CreateUser(ctx, models.User{Username: "alice", PasswordHash: "x"})

	called := false
	handler := a.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		got, ok := FromContext(r.Context())
		if !ok || got.ID != u.ID {
			t.Errorf("FromContext = %+v, ok=%v", got, ok)
		}
		w.WriteHeader(http.StatusOK)
	}))

	// No cookie → 401.
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("no cookie: %d", w.Code)
	}
	if called {
		t.Error("handler invoked despite 401")
	}

	// Bad cookie → 401.
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.AddCookie(&http.Cookie{Name: CookieName, Value: "garbage"})
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, r2)
	if w2.Code != http.StatusUnauthorized {
		t.Errorf("bad cookie: %d", w2.Code)
	}

	// Good cookie → 200.
	wIssue := httptest.NewRecorder()
	rIssue := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, err := a.CreateSession(ctx, wIssue, rIssue, u.ID); err != nil {
		t.Fatal(err)
	}
	cookie := extractCookie(t, wIssue)
	r3 := httptest.NewRequest(http.MethodGet, "/", nil)
	r3.AddCookie(cookie)
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, r3)
	if w3.Code != http.StatusOK {
		t.Errorf("good cookie: %d", w3.Code)
	}
	if !called {
		t.Error("handler not invoked")
	}
}

func TestRequireAdmin_GatesNonAdmin(t *testing.T) {
	a := newAuth(t)
	ctx := context.Background()
	nonAdmin, _ := a.Store.CreateUser(ctx, models.User{Username: "u", PasswordHash: "x"})
	admin, _ := a.Store.CreateUser(ctx, models.User{Username: "root", PasswordHash: "x", IsAdmin: true})

	handler := a.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	makeReq := func(userID int64) *http.Request {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if _, err := a.CreateSession(ctx, w, r, userID); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(extractCookie(t, w))
		return req
	}

	// Non-admin → 403.
	wNA := httptest.NewRecorder()
	handler.ServeHTTP(wNA, makeReq(nonAdmin.ID))
	if wNA.Code != http.StatusForbidden {
		t.Errorf("non-admin: %d, want 403", wNA.Code)
	}

	// Admin → 200.
	wA := httptest.NewRecorder()
	handler.ServeHTTP(wA, makeReq(admin.ID))
	if wA.Code != http.StatusOK {
		t.Errorf("admin: %d, want 200", wA.Code)
	}

	// Unauthenticated → 401.
	wAnon := httptest.NewRecorder()
	handler.ServeHTTP(wAnon, httptest.NewRequest(http.MethodGet, "/", nil))
	if wAnon.Code != http.StatusUnauthorized {
		t.Errorf("anon: %d, want 401", wAnon.Code)
	}
}

func TestBootstrapAdmin(t *testing.T) {
	a := newAuth(t)
	ctx := context.Background()

	u, created, err := a.BootstrapAdmin(ctx, "root", "rootpass")
	if err != nil {
		t.Fatal(err)
	}
	if !created || !u.IsAdmin || u.Username != "root" {
		t.Errorf("bootstrap returned: created=%v user=%+v", created, u)
	}

	// Second call is a no-op.
	_, created2, err := a.BootstrapAdmin(ctx, "second", "x")
	if err != nil {
		t.Fatal(err)
	}
	if created2 {
		t.Error("bootstrap ran twice")
	}

	// Missing creds on first run → error.
	a2 := newAuth(t)
	if _, _, err := a2.BootstrapAdmin(ctx, "", ""); err == nil {
		t.Error("empty creds should fail")
	}
}

func TestLogin(t *testing.T) {
	a := newAuth(t)
	ctx := context.Background()
	hash, _ := a.HashPassword("hunter2")
	u, _ := a.Store.CreateUser(ctx, models.User{Username: "alice", PasswordHash: hash})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/login", nil)

	got, err := a.Login(ctx, w, r, "alice", "hunter2")
	if err != nil {
		t.Fatalf("Login good: %v", err)
	}
	if got.ID != u.ID {
		t.Errorf("user id mismatch")
	}
	if extractCookie(t, w) == nil {
		t.Error("Login did not set cookie")
	}

	if _, err := a.Login(ctx, httptest.NewRecorder(), r, "alice", "wrong"); err != ErrInvalidCredentials {
		t.Errorf("wrong pass: %v", err)
	}
	if _, err := a.Login(ctx, httptest.NewRecorder(), r, "ghost", "x"); err != ErrInvalidCredentials {
		t.Errorf("missing user: %v", err)
	}
}

func TestCleanupExpiredSessions(t *testing.T) {
	a := newAuth(t)
	ctx := context.Background()
	u, _ := a.Store.CreateUser(ctx, models.User{Username: "u", PasswordHash: "x"})

	// Manually insert two sessions: one expired, one valid.
	if _, err := a.Store.DB.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, created_at, expires_at, user_agent)
		VALUES (?, ?, ?, ?, '')`,
		"expired", u.ID, 1, 2); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour).Unix()
	if _, err := a.Store.DB.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, created_at, expires_at, user_agent)
		VALUES (?, ?, ?, ?, '')`,
		"valid", u.ID, time.Now().Unix(), future); err != nil {
		t.Fatal(err)
	}

	n, err := a.CleanupExpiredSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("cleaned %d, want 1", n)
	}
}

// TestSetSessionTTL_NoRaceWithCreateSession exercises the read/write
// path that go-review flagged: SetSessionTTL writes a.SessionTTL while
// CreateSession reads it. Pre-fix this would fail under `go test -race`.
func TestSetSessionTTL_NoRaceWithCreateSession(t *testing.T) {
	a := newAuth(t)
	ctx := context.Background()
	u, _ := a.Store.CreateUser(ctx, models.User{Username: "u", PasswordHash: "x"})

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Writer: bounce the TTL between two values 100 times.
	wg.Add(1)
	go func() {
		defer wg.Done()
		ttls := []time.Duration{30 * time.Minute, 12 * time.Hour}
		for i := 0; i < 100; i++ {
			if err := a.SetSessionTTL(ttls[i%2]); err != nil {
				t.Errorf("SetSessionTTL: %v", err)
				return
			}
		}
		close(stop)
	}()

	// Reader: keep issuing sessions until the writer signals done.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if _, err := a.CreateSession(ctx, w, r, u.ID); err != nil {
				t.Errorf("CreateSession: %v", err)
				return
			}
		}
	}()

	wg.Wait()
}

// TestSetSessionTTL_RangeValidation exercises the bounds enforced by
// SetSessionTTL — single source of truth for both the admin handler and
// the boot-time app_settings loader.
func TestSetSessionTTL_RangeValidation(t *testing.T) {
	a := newAuth(t)
	prior := a.EffectiveSessionTTL()

	cases := []struct {
		name    string
		d       time.Duration
		wantErr bool
	}{
		{"below min", MinSessionTTL - time.Second, true},
		{"at min", MinSessionTTL, false},
		{"in range", time.Hour, false},
		{"at max", MaxSessionTTL, false},
		{"above max", MaxSessionTTL + time.Second, true},
		{"zero", 0, true},
		{"negative", -time.Hour, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := a.EffectiveSessionTTL()
			err := a.SetSessionTTL(tc.d)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for %v, got nil", tc.d)
				}
				if a.EffectiveSessionTTL() != before {
					t.Errorf("TTL changed on rejected input: %v → %v", before, a.EffectiveSessionTTL())
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error for %v: %v", tc.d, err)
			}
			if a.EffectiveSessionTTL() != tc.d {
				t.Errorf("TTL not applied: %v", a.EffectiveSessionTTL())
			}
		})
	}
	_ = prior
}

// extractCookie pulls the ember session cookie from a recorder.
func extractCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == CookieName {
			return c
		}
	}
	return nil
}
