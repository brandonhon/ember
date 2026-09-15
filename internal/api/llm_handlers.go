package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/brandonhon/ember/internal/store"
	"github.com/brandonhon/ember/internal/summarize"
	"github.com/brandonhon/ember/internal/sysinfo"
)

// keyOllamaModel is the app_settings row the model picker has always written.
// The Ollama backend keeps reading it — rather than the newer summarize_model
// — so a model chosen there survives both a backend save and a restart.
const keyOllamaModel = "ollama_model"

// BackendFallbacks are the env-derived boot defaults for the summarization
// backend. Which env vars supply the URL and the model depends on which
// backend is active: Ollama has always taken them from EMBER_OLLAMA_URL /
// EMBER_OLLAMA_MODEL, the hosted backends take them from
// EMBER_SUMMARY_BASE_URL / EMBER_SUMMARY_MODEL. Both pairs are carried here
// rather than one being frozen at boot because the active backend now changes
// at runtime — an admin who tries OpenAI and then switches back to Ollama must
// land on the local daemon again, not on an Ollama client pointed at the
// provider's URL.
type BackendFallbacks struct {
	// Backend is EMBER_SUMMARY_BACKEND: the boot-time default selection.
	Backend string
	// APIKey is EMBER_SUMMARY_API_KEY. Only the hosted backends use it, and
	// the persisted summarize_api_key row overrides it once an admin sets one.
	APIKey string
	// OllamaBaseURL / OllamaModel are EMBER_OLLAMA_URL and EMBER_OLLAMA_MODEL.
	OllamaBaseURL string
	OllamaModel   string
	// HostedBaseURL / HostedModel are EMBER_SUMMARY_BASE_URL and
	// EMBER_SUMMARY_MODEL.
	HostedBaseURL string
	HostedModel   string
}

// forBackend returns the fallback that applies to the named backend.
func (f BackendFallbacks) forBackend(backend string) store.BackendSettings {
	out := store.BackendSettings{Backend: backend, APIKey: f.APIKey}
	if backend == store.BackendOllama {
		out.BaseURL, out.Model = f.OllamaBaseURL, f.OllamaModel
		return out
	}
	out.BaseURL, out.Model = f.HostedBaseURL, f.HostedModel
	return out
}

// ResolveBackend returns the live backend configuration: the persisted
// summarize_* rows overlaid on the env defaults for whichever backend is
// selected. Exported because cmd/ember calls it once at boot with the same
// fallbacks it hands to Dependencies, so boot and the admin handler cannot
// disagree about what "the current backend" means.
func ResolveBackend(ctx context.Context, st *store.Store, f BackendFallbacks) store.BackendSettings {
	fb := f.forBackend(st.ResolveBackendChoice(ctx, f.Backend))
	if fb.Backend == store.BackendOllama {
		if v, _ := st.GetAppSetting(ctx, keyOllamaModel); v != "" {
			fb.Model = v
		}
	}
	return st.ResolveBackendSettings(ctx, fb)
}

// BuildBackend constructs the summarizer for the resolved settings. It returns
// (nil, nil) when the selected backend is not usable yet — no URL, no key —
// which the caller stores in the Switcher, turning the summary gate off until
// an admin finishes configuring it rather than queueing every incoming article
// at an endpoint that cannot answer.
//
// The second return value is the *Ollama for the model-management endpoints
// and is non-nil only for the Ollama backend: pull and delete have no
// equivalent on a hosted provider (issue #200).
func BuildBackend(s store.BackendSettings) (summarize.Summarizer, *summarize.Ollama) {
	switch s.Backend {
	case store.BackendOpenAI:
		if s.BaseURL == "" || s.Model == "" {
			return nil, nil
		}
		return summarize.NewOpenAI(s.BaseURL, s.APIKey, s.Model), nil
	case store.BackendAnthropic:
		if s.APIKey == "" {
			return nil, nil
		}
		return summarize.NewClaude(s.APIKey, s.Model), nil
	default: // store.BackendOllama
		if s.BaseURL == "" || s.Model == "" {
			return nil, nil
		}
		o := summarize.NewOllama(s.BaseURL, s.Model)
		return o, o
	}
}

// ollamaHolder guards the live *summarize.Ollama. The backend-change handler
// reassigns it while the other admin LLM handlers read it, so a bare struct
// field would be a data race — the same class of bug that already bit the
// Ollama client itself (see internal/summarize/client_race_test.go). The
// atomic lives behind a pointer rather than directly on Dependencies because
// NewRouter takes Dependencies by value: an atomic.Pointer field would trip
// go vet's copylocks, and every copy would diverge. One holder, shared by
// every copy of the struct.
type ollamaHolder struct {
	p atomic.Pointer[summarize.Ollama]
}

// ollama returns the live Ollama client, or nil when the active backend is not
// Ollama. Falls back to the Dependencies field for tests that build a
// Dependencies and call handlers without going through NewRouter.
func (d *Dependencies) ollama() *summarize.Ollama {
	if d.ollamaLive != nil {
		return d.ollamaLive.p.Load()
	}
	return d.Ollama
}

// setOllama swaps the live Ollama client. Called only from the backend-change
// handler, which runs after NewRouter has installed the holder.
func (d *Dependencies) setOllama(o *summarize.Ollama) {
	if d.ollamaLive != nil {
		d.ollamaLive.p.Store(o)
	}
}

// validModelName bounds an Ollama model reference to its documented shape
// ([registry/][namespace/]name[:tag]) and printable ASCII. Even though these
// endpoints are admin-only, validating the name stops a compromised/rogue admin
// from coaxing the Ollama daemon into pulling from an arbitrary registry or
// dereferencing path-traversal components.
var validModelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$`)

// pullInProgress prevents concurrent model pulls. A single pull can block for
// up to 30 minutes; allowing concurrent pulls would saturate Ollama and exhaust
// server goroutines.
var pullInProgress atomic.Bool

// requireSummarizer reports whether a live Ollama client is wired up, writing
// the 503 when it isn't. Model management — the installed list, pull, delete,
// the active-model switch and the generation tunables — is Ollama-only by
// design: a hosted provider has no local model cache to manage, so these
// endpoints stand down instead of pretending (issue #200).
//
// It returns the client rather than a bare bool so callers hold the exact
// instance they were cleared to use: the backend-change handler can swap it
// out between the check and the call, and re-reading the holder would leave a
// nil-dereference window.
func (d *Dependencies) requireSummarizer(w http.ResponseWriter) (*summarize.Ollama, bool) {
	o := d.ollama()
	if o == nil {
		writeError(w, http.StatusServiceUnavailable, "not_ollama",
			"model management is only available with the Ollama backend")
		return nil, false
	}
	return o, true
}

// modelFromRequest is the shared preamble of the set/pull/delete model
// endpoints: require a summarizer, decode the body, and validate the model
// reference. Writes the error response and returns ok=false on any failure.
func (d *Dependencies) modelFromRequest(w http.ResponseWriter, r *http.Request) (*summarize.Ollama, string, bool) {
	o, ok := d.requireSummarizer(w)
	if !ok {
		return nil, "", false
	}
	var req setModelReq
	if !decodeJSON(w, r, &req) {
		return nil, "", false
	}
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "model required")
		return nil, "", false
	}
	if !validModelName.MatchString(req.Model) {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid model name")
		return nil, "", false
	}
	return o, req.Model, true
}

// llmStatus is the response shape for GET /api/admin/llm. Reports the selected
// backend and how to reach it, the recommended model for the host, and what's
// installed in Ollama.
//
// APIKeySet is a boolean, never the key: the stored credential is a paid
// secret and echoing it would put it in every admin's browser session and in
// any log of this response. Same treatment as the SMTP password.
type llmStatus struct {
	CurrentModel string                     `json:"current_model"`
	Enabled      bool                       `json:"enabled"`
	Backend      string                     `json:"backend"`
	BaseURL      string                     `json:"base_url"`
	APIKeySet    bool                       `json:"api_key_set"`
	Model        string                     `json:"model"`
	System       sysinfo.SystemInfo         `json:"system"`
	Recommended  sysinfo.Recommendation     `json:"recommended"`
	Installed    []summarize.InstalledModel `json:"installed"`
	InstalledErr string                     `json:"installed_err,omitempty"`
	Options      summarize.Options          `json:"options"`
}

func (d *Dependencies) handleGetLLM(w http.ResponseWriter, r *http.Request) {
	sysI := sysinfo.Detect()
	live := ResolveBackend(r.Context(), d.Store, d.BackendFallbacks)
	resp := llmStatus{
		System:      sysI,
		Recommended: sysinfo.Recommend(sysI),
		Backend:     live.Backend,
		BaseURL:     live.BaseURL,
		APIKeySet:   live.APIKey != "",
		Model:       live.Model,
		// Enabled now means "a backend is actually wired up and answerable",
		// which is false while an admin has selected a hosted backend but not
		// yet supplied its key. The Backend card is rendered regardless — it is
		// the only way out of that state.
		Enabled: d.Backend != nil && d.Backend.Configured(),
	}
	o := d.ollama()
	if o == nil {
		// Not the Ollama backend: there is no local model cache to list and no
		// generation tunables to report. The backend fields above are the
		// whole answer.
		writeData(w, http.StatusOK, resp, nil)
		return
	}
	resp.CurrentModel = o.Model()
	resp.Options = o.Options()
	// Tags can fail if Ollama is down — surface as a soft error rather than
	// 500ing the whole status page.
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	installed, err := o.ListInstalled(ctx)
	if err != nil {
		slog.Default().Warn("api: ollama list installed", "err", err)
		resp.InstalledErr = "Ollama unreachable"
	} else {
		resp.Installed = installed
	}
	writeData(w, http.StatusOK, resp, nil)
}

// setLLMBackendReq is the one-shot backend change: transport, endpoint,
// credential and model together, because they are only meaningful as a set —
// a base URL from one provider with another's key summarizes nothing.
//
// APIKey follows the SMTP password's rules: an empty string means "keep the
// stored key" (the SPA never receives it, so it cannot send it back), and
// ClearAPIKey is the only way to erase one.
type setLLMBackendReq struct {
	Backend     string `json:"backend"`
	BaseURL     string `json:"base_url"`
	APIKey      string `json:"api_key"`
	ClearAPIKey bool   `json:"clear_api_key"`
	Model       string `json:"model"`
}

// httpScheme reports whether raw parses as an http/https URL. The base URL is
// an admin-set outbound request target that we send an API key to, so anything
// else — file:, gopher:, a bare host with no scheme — is refused here rather
// than at the first article. Mirrors the EMBER_OLLAMA_URL check in
// internal/config.
func httpScheme(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// handleSetLLMBackend persists the summarization backend and swaps it into the
// live Switcher, so the change applies to the next article without a restart.
// Admin-only: the backend decides where every user's article text is sent.
func (d *Dependencies) handleSetLLMBackend(w http.ResponseWriter, r *http.Request) {
	var req setLLMBackendReq
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Backend = strings.TrimSpace(req.Backend)
	req.BaseURL = strings.TrimSpace(req.BaseURL)
	req.Model = strings.TrimSpace(req.Model)

	if !store.ValidBackend(req.Backend) {
		writeError(w, http.StatusBadRequest, "bad_request",
			"backend must be one of ollama, openai, anthropic")
		return
	}
	if req.BaseURL != "" && !httpScheme(req.BaseURL) {
		writeError(w, http.StatusBadRequest, "bad_request",
			"base_url must be an http or https URL")
		return
	}
	// Refuse a backend that cannot answer. Storing one would leave every
	// incoming article queued at a dead endpoint until an admin noticed.
	// Ollama is exempt from the base-URL requirement because it falls back to
	// EMBER_OLLAMA_URL, which every existing install already has.
	if req.Backend == store.BackendOpenAI && req.BaseURL == "" {
		writeError(w, http.StatusBadRequest, "bad_request",
			"base_url is required for the OpenAI-compatible backend")
		return
	}
	// A model is just as load-bearing as the URL for the OpenAI backend —
	// /v1/chat/completions has no server-side default — so it gets the same
	// treatment rather than being saved and then reported unusable one field
	// away. Anthropic is exempt: NewClaude falls back to DefaultClaudeModel,
	// so a missing model there really is harmless.
	if req.Backend == store.BackendOpenAI && req.Model == "" {
		writeError(w, http.StatusBadRequest, "bad_request",
			"model is required for the OpenAI-compatible backend")
		return
	}
	// The stored key counts: an admin re-saving the model on an already
	// configured Claude backend sends an empty api_key meaning "unchanged".
	ctx := r.Context()
	current := ResolveBackend(ctx, d.Store, d.BackendFallbacks)
	haveKey := req.APIKey != "" || (!req.ClearAPIKey && current.APIKey != "")
	if req.Backend == store.BackendAnthropic && !haveKey {
		writeError(w, http.StatusBadRequest, "bad_request",
			"api_key is required for the Claude backend")
		return
	}

	// The Ollama backend owns neither of these rows: its endpoint is
	// EMBER_OLLAMA_URL and its model is the ollama_model row the model picker
	// writes. Persisting whatever the caller sent would build
	// NewOllama("https://api.groq.com/openai/v1", "llama-3.3-70b") on the way
	// back from a hosted provider — a client that is non-nil, so Configured()
	// is true and the readiness gate never fires, and that then fails every
	// article and stamps each one 'skipped', which is terminal. It would also
	// override the model picker's ollama_model row across restarts. Blanking
	// here rather than trusting the caller covers curl and every other non-SPA
	// client, not just the Settings form.
	//
	// Anthropic blanks BaseURL for the same non-nil-client reason — Claude has
	// no SetBaseURL, so a leftover URL is inert for requests but would still
	// build NewClaude(key, "llama-3.3-70b") from a stale foreign model id and
	// report a stale hosted URL beside backend: "anthropic". Model is left
	// alone there because, unlike Ollama, a model id is meaningful to Claude.
	switch req.Backend {
	case store.BackendOllama:
		req.BaseURL, req.Model = "", ""
	case store.BackendAnthropic:
		req.BaseURL = ""
	}

	// BaseURL and Model are written unconditionally (empty = "inherit the env
	// default") rather than patched: this endpoint always sends the complete
	// set, and leaving a previous provider's URL behind is how switching back
	// to Ollama would end up pointed at a hosted endpoint.
	if err := d.Store.PutBackendSettings(ctx, store.BackendUpdate{
		Backend:     &req.Backend,
		BaseURL:     &req.BaseURL,
		APIKey:      &req.APIKey,
		ClearAPIKey: req.ClearAPIKey,
		Model:       &req.Model,
	}); err != nil {
		internalError(w, "internal", err)
		return
	}

	live := ResolveBackend(ctx, d.Store, d.BackendFallbacks)
	sum, oll := BuildBackend(live)
	if d.Backend != nil {
		d.Backend.Set(sum)
	}
	d.setOllama(oll)
	// Never log the key — not the value, not its length. The backend and the
	// endpoint are useful in an audit trail; the credential is not.
	slog.Default().Info("api: summarization backend changed",
		"backend", live.Backend, "base_url", live.BaseURL, "model", live.Model,
		"configured", sum != nil)

	writeData(w, http.StatusOK, llmBackendResp{
		Backend:   live.Backend,
		BaseURL:   live.BaseURL,
		APIKeySet: live.APIKey != "",
		Model:     live.Model,
		Enabled:   sum != nil,
	}, nil)
}

// llmBackendResp echoes the saved backend. It carries api_key_set, never the
// key itself.
type llmBackendResp struct {
	Backend   string `json:"backend"`
	BaseURL   string `json:"base_url"`
	APIKeySet bool   `json:"api_key_set"`
	Model     string `json:"model"`
	Enabled   bool   `json:"enabled"`
}

type setModelReq struct {
	Model string `json:"model"`
}

// handleSetLLMModel persists the chosen model in app_settings and swaps it
// into the live summarizer. Admin-only because the model affects every user.
func (d *Dependencies) handleSetLLMModel(w http.ResponseWriter, r *http.Request) {
	o, model, ok := d.modelFromRequest(w, r)
	if !ok {
		return
	}
	if err := d.Store.PutAppSetting(r.Context(), keyOllamaModel, model); err != nil {
		internalError(w, "internal", err)
		return
	}
	o.SetModel(model)
	writeData(w, http.StatusOK, map[string]string{"model": model}, nil)
}

// handleSetLLMOptions persists tunables in app_settings and swaps them into
// the live summarizer. Zero values clear that field.
func (d *Dependencies) handleSetLLMOptions(w http.ResponseWriter, r *http.Request) {
	o, ok := d.requireSummarizer(w)
	if !ok {
		return
	}
	var opts summarize.Options
	if !decodeJSON(w, r, &opts) {
		return
	}
	// Basic clamps to avoid nonsense.
	if opts.Temperature < 0 {
		opts.Temperature = 0
	}
	if opts.Temperature > 2 {
		opts.Temperature = 2
	}
	if opts.TopP < 0 {
		opts.TopP = 0
	}
	if opts.TopP > 1 {
		opts.TopP = 1
	}
	if opts.NumCtx < 0 {
		opts.NumCtx = 0
	}
	if opts.NumCtx > 32768 {
		opts.NumCtx = 32768
	}
	if !d.putAppSettings(r.Context(), w, []appSetting{
		{"llm_temperature", strconv.FormatFloat(opts.Temperature, 'f', -1, 64)},
		{"llm_top_p", strconv.FormatFloat(opts.TopP, 'f', -1, 64)},
		{"llm_num_ctx", strconv.Itoa(opts.NumCtx)},
	}) {
		return
	}
	o.SetOptions(opts)
	writeData(w, http.StatusOK, opts, nil)
}

// handleDeleteLLMModel removes a model from Ollama's local cache. Refuses
// to delete the active model — the caller must switch first.
func (d *Dependencies) handleDeleteLLMModel(w http.ResponseWriter, r *http.Request) {
	o, model, ok := d.modelFromRequest(w, r)
	if !ok {
		return
	}
	if model == o.Model() {
		writeError(w, http.StatusConflict, "active_model", "cannot delete the active model — switch first")
		return
	}
	if err := o.Delete(r.Context(), model); err != nil {
		slog.Default().Warn("api: ollama delete failed", "model", model, "err", err)
		writeError(w, http.StatusBadGateway, "delete_failed", "Ollama refused the delete (model may not exist)")
		return
	}
	writeData(w, http.StatusOK, map[string]string{"model": model}, nil)
}

// handlePullLLMModel proxies an `ollama pull` for the named model. Blocks
// until done — model downloads can run to minutes. The server's default
// WriteTimeout (90s) is too short for large models, so we bump the write
// deadline via ResponseController for this handler. Admin-only.
func (d *Dependencies) handlePullLLMModel(w http.ResponseWriter, r *http.Request) {
	o, model, ok := d.modelFromRequest(w, r)
	if !ok {
		return
	}
	if !pullInProgress.CompareAndSwap(false, true) {
		writeError(w, http.StatusConflict, "pull_in_progress", "a model pull is already running")
		return
	}
	defer pullInProgress.Store(false)
	// Override per-connection deadlines so the response can stay open for
	// the duration of the pull. http.NewResponseController is the modern
	// way to do this; errors mean the server doesn't support it (very old
	// stdlib) and we fall back to whatever the default is.
	rc := http.NewResponseController(w)
	deadline := time.Now().Add(35 * time.Minute)
	_ = rc.SetWriteDeadline(deadline)
	_ = rc.SetReadDeadline(deadline)
	// Detach from the request context so the pull survives the browser tab
	// closing mid-download, but derive from the process background context (not
	// context.Background()) so SIGTERM still cancels it and graceful shutdown
	// isn't blocked for up to 30 minutes. Cap at 30 minutes total.
	ctx, cancel := context.WithTimeout(d.backgroundCtx(), 30*time.Minute)
	defer cancel()
	if err := o.Pull(ctx, model); err != nil {
		slog.Default().Warn("api: ollama pull failed", "model", model, "err", err)
		writeError(w, http.StatusBadGateway, "pull_failed", "Ollama refused the pull (check model name and network)")
		return
	}
	writeData(w, http.StatusOK, map[string]string{"model": model}, nil)
}
