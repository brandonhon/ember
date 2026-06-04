package ttrss

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

func writeEnv(w http.ResponseWriter, status int, content any) {
	w.Header().Set("Content-Type", "application/json")
	b, _ := json.Marshal(content)
	_, _ = fmt.Fprintf(w, `{"seq":0,"status":%d,"content":%s}`, status, b)
}

func headlineItem(n int) map[string]any {
	return map[string]any{
		"title":   fmt.Sprintf("Article %d", n),
		"link":    fmt.Sprintf("https://example.com/a/%d", n),
		"content": fmt.Sprintf("<p>body %d</p>", n),
		"author":  "Tester",
		"updated": 1717459200 + n,
	}
}

// fakeTTRSS serves a minimal TT-RSS JSON API: starred (feed -1) has 2 items,
// archived (feed 0) has 1, each on a single page. It asserts show_content.
func fakeTTRSS(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		switch req["op"] {
		case "login":
			writeEnv(w, 0, map[string]any{"session_id": "sess-1"})
		case "getHeadlines":
			if req["show_content"] != true {
				t.Errorf("show_content should be true, got %v", req["show_content"])
			}
			feedID, _ := req["feed_id"].(string)
			skip := int(req["skip"].(float64))
			var items []map[string]any
			if skip == 0 {
				switch feedID {
				case "-1":
					items = []map[string]any{headlineItem(1), headlineItem(2)}
				case "0":
					items = []map[string]any{headlineItem(3)}
				}
			}
			writeEnv(w, 0, items)
		case "logout":
			writeEnv(w, 0, map[string]any{"status": "OK"})
		default:
			writeEnv(w, 1, map[string]any{"error": "UNKNOWN_METHOD"})
		}
	})
	return httptest.NewTLSServer(mux)
}

func newAPISvc(t *testing.T, srv *httptest.Server) (*Service, models.User) {
	t.Helper()
	s := store.NewTest(t)
	u, err := s.CreateUser(context.Background(), models.User{Username: "u", PasswordHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(s)
	svc.HTTPClient = srv.Client()
	// allow-all validator so the httptest 127.0.0.1 endpoint passes
	svc.ValidateURL = func(context.Context, string) error { return nil }
	return svc, u
}

func TestImportFromAPI(t *testing.T) {
	srv := fakeTTRSS(t)
	defer srv.Close()
	svc, u := newAPISvc(t, srv)
	ctx := context.Background()

	res, err := svc.ImportFromAPI(ctx, u.ID, APIOptions{
		BaseURL: srv.URL, Username: "alice", Password: "pw",
		ImportStarred: true, ImportArchived: true,
	})
	if err != nil {
		t.Fatalf("ImportFromAPI: %v", err)
	}
	if res.Total != 3 || res.Imported != 3 || res.Skipped != 0 {
		t.Errorf("res = %+v, want Total=3 Imported=3 Skipped=0", res)
	}

	starred, err := svc.Store.ListArticles(ctx, u.ID, store.ListArticlesQuery{View: "starred", Limit: 50, OnlySummarized: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(starred) != 3 {
		t.Fatalf("starred view = %d, want 3", len(starred))
	}
	for _, a := range starred {
		if !a.IsStarred || !a.IsRead {
			t.Errorf("article %q not starred+read", a.Title)
		}
		if a.ContentHTML == "" {
			t.Errorf("article %q has no content (show_content not honored?)", a.Title)
		}
	}
}

func TestImportFromAPI_OnlyStarred(t *testing.T) {
	srv := fakeTTRSS(t)
	defer srv.Close()
	svc, u := newAPISvc(t, srv)

	res, err := svc.ImportFromAPI(context.Background(), u.ID, APIOptions{
		BaseURL: srv.URL, Username: "alice", Password: "pw",
		ImportStarred: true, ImportArchived: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 2 { // only the two starred, archived skipped
		t.Errorf("Imported = %d, want 2 (starred only)", res.Imported)
	}
}

func TestImportFromAPI_Idempotent(t *testing.T) {
	srv := fakeTTRSS(t)
	defer srv.Close()
	svc, u := newAPISvc(t, srv)
	ctx := context.Background()
	opt := APIOptions{BaseURL: srv.URL, Username: "a", Password: "p", ImportStarred: true, ImportArchived: true}

	if _, err := svc.ImportFromAPI(ctx, u.ID, opt); err != nil {
		t.Fatal(err)
	}
	res, err := svc.ImportFromAPI(ctx, u.ID, opt)
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 0 {
		t.Errorf("re-import Imported = %d, want 0 (dedup)", res.Imported)
	}
}

func TestImportFromAPI_Paginates(t *testing.T) {
	// Starred returns a full page (headlineLimit) then a partial page, so the
	// pull must request skip=0 and skip=headlineLimit before stopping.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch req["op"] {
		case "login":
			writeEnv(w, 0, map[string]any{"session_id": "s"})
		case "getHeadlines":
			skip := int(req["skip"].(float64))
			var items []map[string]any
			switch skip {
			case 0:
				for i := 0; i < headlineLimit; i++ {
					items = append(items, headlineItem(i))
				}
			case headlineLimit:
				items = []map[string]any{headlineItem(headlineLimit), headlineItem(headlineLimit + 1)}
			}
			writeEnv(w, 0, items)
		default:
			writeEnv(w, 0, map[string]any{})
		}
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()
	svc, u := newAPISvc(t, srv)

	res, err := svc.ImportFromAPI(context.Background(), u.ID, APIOptions{
		BaseURL: srv.URL, Username: "a", Password: "p", ImportStarred: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != headlineLimit+2 {
		t.Errorf("Imported = %d, want %d (two pages)", res.Imported, headlineLimit+2)
	}
}

func TestImportFromAPI_APIError(t *testing.T) {
	// Login returns status=1 (e.g. API access disabled) — surfaced to caller.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeEnv(w, 1, map[string]any{"error": "API_DISABLED"})
	}))
	defer srv.Close()
	svc, u := newAPISvc(t, srv)

	_, err := svc.ImportFromAPI(context.Background(), u.ID, APIOptions{
		BaseURL: srv.URL, Username: "a", Password: "p", ImportStarred: true,
	})
	if err == nil || !strings.Contains(err.Error(), "API_DISABLED") {
		t.Errorf("want API_DISABLED error, got %v", err)
	}
}

func TestImportFromAPI_SSRFRejected(t *testing.T) {
	srv := fakeTTRSS(t)
	defer srv.Close()
	svc, u := newAPISvc(t, srv)
	blocked := errors.New("blocked by SSRF guard")
	svc.ValidateURL = func(context.Context, string) error { return blocked }

	_, err := svc.ImportFromAPI(context.Background(), u.ID, APIOptions{
		BaseURL: srv.URL, Username: "a", Password: "p", ImportStarred: true,
	})
	if err == nil || !errors.Is(err, blocked) {
		t.Errorf("want SSRF rejection, got %v", err)
	}
}

func TestImportFromAPI_NothingSelected(t *testing.T) {
	srv := fakeTTRSS(t)
	defer srv.Close()
	svc, u := newAPISvc(t, srv)
	_, err := svc.ImportFromAPI(context.Background(), u.ID, APIOptions{
		BaseURL: srv.URL, Username: "a", Password: "p",
	})
	if err == nil {
		t.Error("expected error when neither starred nor archived selected")
	}
}
