package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"
)

// postJSON sends body to url on cl with the CSRF header echoed from the jar.
// A hand-built request without it gets 403 csrf_mismatch, which masks whatever
// the test was actually checking.
func postJSON(t *testing.T, cl *http.Client, url string, body []byte) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	echoCSRF(cl, url, req)
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

// Issue #161: enabling the daily digest always failed with 400. GET returns
// models.UserDigest (user_id, last_sent_at); the SPA posted that object
// straight back; decodeJSON uses DisallowUnknownFields, so the server rejected
// a body it had just produced itself. The endpoint must accept its own output.
func TestDigest_GetResponseIsPostable(t *testing.T) {
	h := newHarness(t)
	h.seedUser(t, "alice", "correct-horse", false)
	cl := h.login(t, "alice", "correct-horse")
	url := h.srv.URL + "/api/me/digest"

	resp, err := cl.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Guard against a vacuous test: if GET ever stops emitting the
	// server-owned fields, this no longer exercises the round-trip.
	for _, field := range []string{`"user_id"`, `"last_sent_at"`} {
		if !bytes.Contains(env.Data, []byte(field)) {
			t.Fatalf("GET response no longer contains %s — this test would prove nothing: %s", field, env.Data)
		}
	}

	if code, body := postJSON(t, cl, url, env.Data); code != http.StatusOK {
		t.Errorf("POSTing the GET response back = %d, want 200; body: %s", code, body)
	}
}

// The two accepted fields must stay inert. UserID is taken from the session,
// so a body claiming another user's id must not retarget the write — this is
// the risk that comes with accepting a field in order to ignore it.
func TestDigest_BodyUserIDCannotRetargetTheWrite(t *testing.T) {
	h := newHarness(t)
	alice := h.seedUser(t, "alice", "correct-horse", false)
	bob := h.seedUser(t, "bob", "correct-horse", false)
	cl := h.login(t, "alice", "correct-horse")
	url := h.srv.URL + "/api/me/digest"

	// Alice posts Bob's user_id alongside real settings.
	body := []byte(`{"user_id":` + strconv.FormatInt(bob.ID, 10) + `,"last_sent_at":99999,"enabled":true,` +
		`"view_kind":"smart","view_value":"fresh","hour_utc":7,"minute_utc":30,"email_override":""}`)
	if code, out := postJSON(t, cl, url, body); code != http.StatusOK {
		t.Fatalf("post = %d, want 200; body: %s", code, out)
	}

	// Alice's digest changed...
	got, err := h.store.GetDigest(t.Context(), alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.HourUTC != 7 || got.MinuteUTC != 30 {
		t.Errorf("alice's digest not saved: %+v", got)
	}
	if got.UserID != alice.ID {
		t.Errorf("stored UserID = %d, want alice %d — the body retargeted the write", got.UserID, alice.ID)
	}
	// ...and Bob's did not.
	bobDigest, err := h.store.GetDigest(t.Context(), bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bobDigest.Enabled || bobDigest.HourUTC == 7 {
		t.Errorf("bob's digest was modified by alice's request: %+v", bobDigest)
	}
	// last_sent_at is server-owned bookkeeping and must not be settable.
	if got.LastSentAt == 99999 {
		t.Error("last_sent_at was taken from the request body")
	}
}
