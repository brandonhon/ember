package summarize

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// The admin API calls ListInstalled/Delete/Pull from HTTP handlers while the
// poller's summary worker calls Summarize. model and options are held in
// atomics for exactly that reason — HTTPClient must be safe to read
// concurrently too, and must never be written on the fly.
func TestOllama_ConcurrentAdminAndSummarizeDoNotRace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[{"name":"m","size":1,"modified_at":"t"}]}`))
		default:
			_, _ = w.Write([]byte(`{"response":"SUMMARY: ok\n\nPOINTS:\n- a\n- b\n- c","done":true}`))
		}
	}))
	defer srv.Close()

	// Constructed WITHOUT NewOllama, so HTTPClient starts nil — the case the
	// lazy initialiser in Summarize exists to cover.
	o := &Ollama{BaseURL: srv.URL, MaxInput: 100}
	o.SetModel("m")

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = o.Summarize(context.Background(), "t", "body text")
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = o.ListInstalled(context.Background())
		}()
	}
	wg.Wait()
}
