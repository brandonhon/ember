package api

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/brandonhon/ember/internal/summarize"
	"github.com/brandonhon/ember/internal/sysinfo"
)

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

func floatToStr(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
func intToStr(i int) string       { return strconv.Itoa(i) }

// llmStatus is the response shape for GET /api/admin/llm. Reports current
// model, recommended model for the host, and what's installed in Ollama.
type llmStatus struct {
	CurrentModel string                     `json:"current_model"`
	Enabled      bool                       `json:"enabled"`
	System       sysinfo.SystemInfo         `json:"system"`
	Recommended  sysinfo.Recommendation     `json:"recommended"`
	Installed    []summarize.InstalledModel `json:"installed"`
	InstalledErr string                     `json:"installed_err,omitempty"`
	Options      summarize.Options          `json:"options"`
}

func (d *Dependencies) handleGetLLM(w http.ResponseWriter, r *http.Request) {
	sysI := sysinfo.Detect()
	resp := llmStatus{
		System:      sysI,
		Recommended: sysinfo.Recommend(sysI),
	}
	if d.Ollama == nil {
		writeData(w, http.StatusOK, resp, nil)
		return
	}
	resp.Enabled = true
	resp.CurrentModel = d.Ollama.Model()
	resp.Options = d.Ollama.Options()
	// Tags can fail if Ollama is down — surface as a soft error rather than
	// 500ing the whole status page.
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	installed, err := d.Ollama.ListInstalled(ctx)
	if err != nil {
		slog.Default().Warn("api: ollama list installed", "err", err)
		resp.InstalledErr = "Ollama unreachable"
	} else {
		resp.Installed = installed
	}
	writeData(w, http.StatusOK, resp, nil)
}

type setModelReq struct {
	Model string `json:"model"`
}

// handleSetLLMModel persists the chosen model in app_settings and swaps it
// into the live summarizer. Admin-only because the model affects every user.
func (d *Dependencies) handleSetLLMModel(w http.ResponseWriter, r *http.Request) {
	if d.Ollama == nil {
		writeError(w, http.StatusServiceUnavailable, "no_summarizer", "summaries are disabled on this server")
		return
	}
	var req setModelReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "model required")
		return
	}
	if !validModelName.MatchString(req.Model) {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid model name")
		return
	}
	if err := d.Store.PutAppSetting(r.Context(), "ollama_model", req.Model); err != nil {
		internalError(w, "internal", err)
		return
	}
	d.Ollama.SetModel(req.Model)
	writeData(w, http.StatusOK, map[string]string{"model": req.Model}, nil)
}

// handleSetLLMOptions persists tunables in app_settings and swaps them into
// the live summarizer. Zero values clear that field.
func (d *Dependencies) handleSetLLMOptions(w http.ResponseWriter, r *http.Request) {
	if d.Ollama == nil {
		writeError(w, http.StatusServiceUnavailable, "no_summarizer", "summaries are disabled on this server")
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
	if err := d.Store.PutAppSetting(r.Context(), "llm_temperature", floatToStr(opts.Temperature)); err != nil {
		internalError(w, "internal", err)
		return
	}
	if err := d.Store.PutAppSetting(r.Context(), "llm_top_p", floatToStr(opts.TopP)); err != nil {
		internalError(w, "internal", err)
		return
	}
	if err := d.Store.PutAppSetting(r.Context(), "llm_num_ctx", intToStr(opts.NumCtx)); err != nil {
		internalError(w, "internal", err)
		return
	}
	d.Ollama.SetOptions(opts)
	writeData(w, http.StatusOK, opts, nil)
}

// handleDeleteLLMModel removes a model from Ollama's local cache. Refuses
// to delete the active model — the caller must switch first.
func (d *Dependencies) handleDeleteLLMModel(w http.ResponseWriter, r *http.Request) {
	if d.Ollama == nil {
		writeError(w, http.StatusServiceUnavailable, "no_summarizer", "summaries are disabled on this server")
		return
	}
	var req setModelReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "model required")
		return
	}
	if !validModelName.MatchString(req.Model) {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid model name")
		return
	}
	if req.Model == d.Ollama.Model() {
		writeError(w, http.StatusConflict, "active_model", "cannot delete the active model — switch first")
		return
	}
	if err := d.Ollama.Delete(r.Context(), req.Model); err != nil {
		slog.Default().Warn("api: ollama delete failed", "model", req.Model, "err", err)
		writeError(w, http.StatusBadGateway, "delete_failed", "Ollama refused the delete (model may not exist)")
		return
	}
	writeData(w, http.StatusOK, map[string]string{"model": req.Model}, nil)
}

// handlePullLLMModel proxies an `ollama pull` for the named model. Blocks
// until done — model downloads can run to minutes. The server's default
// WriteTimeout (90s) is too short for large models, so we bump the write
// deadline via ResponseController for this handler. Admin-only.
func (d *Dependencies) handlePullLLMModel(w http.ResponseWriter, r *http.Request) {
	if d.Ollama == nil {
		writeError(w, http.StatusServiceUnavailable, "no_summarizer", "summaries are disabled on this server")
		return
	}
	var req setModelReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "model required")
		return
	}
	if !validModelName.MatchString(req.Model) {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid model name")
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
	if err := d.Ollama.Pull(ctx, req.Model); err != nil {
		slog.Default().Warn("api: ollama pull failed", "model", req.Model, "err", err)
		writeError(w, http.StatusBadGateway, "pull_failed", "Ollama refused the pull (check model name and network)")
		return
	}
	writeData(w, http.StatusOK, map[string]string{"model": req.Model}, nil)
}
