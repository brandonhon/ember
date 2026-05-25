package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/brandonhon/ember/internal/summarize"
	"github.com/brandonhon/ember/internal/sysinfo"
)

func floatToStr(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
func intToStr(i int) string       { return strconv.Itoa(i) }

// llmStatus is the response shape for GET /api/admin/llm. Reports current
// model, recommended model for the host, and what's installed in Ollama.
type llmStatus struct {
	CurrentModel string                       `json:"current_model"`
	BaseURL      string                       `json:"base_url"`
	Enabled      bool                         `json:"enabled"`
	System       sysinfo.SystemInfo           `json:"system"`
	Recommended  sysinfo.Recommendation       `json:"recommended"`
	Installed    []summarize.InstalledModel   `json:"installed"`
	InstalledErr string                       `json:"installed_err,omitempty"`
	Options      summarize.Options            `json:"options"`
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
	resp.BaseURL = d.Ollama.BaseURL
	resp.Options = d.Ollama.Options()
	// Tags can fail if Ollama is down — surface as a soft error rather than
	// 500ing the whole status page.
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	installed, err := d.Ollama.ListInstalled(ctx)
	if err != nil {
		resp.InstalledErr = err.Error()
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
	if err := d.Store.PutAppSetting(r.Context(), "ollama_model", req.Model); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
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
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if err := d.Store.PutAppSetting(r.Context(), "llm_top_p", floatToStr(opts.TopP)); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if err := d.Store.PutAppSetting(r.Context(), "llm_num_ctx", intToStr(opts.NumCtx)); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
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
	if req.Model == d.Ollama.Model() {
		writeError(w, http.StatusConflict, "active_model", "cannot delete the active model — switch first")
		return
	}
	if err := d.Ollama.Delete(r.Context(), req.Model); err != nil {
		writeError(w, http.StatusBadGateway, "delete_failed", err.Error())
		return
	}
	writeData(w, http.StatusOK, map[string]string{"model": req.Model}, nil)
}

// handlePullLLMModel proxies an `ollama pull` for the named model. Blocks
// until done — model downloads run to minutes, so the client should expect
// a long-poll. Admin-only.
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
	// Detach from the request context: the pull should still complete if the
	// browser tab closes mid-download. Cap at 30 minutes total.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if err := d.Ollama.Pull(ctx, req.Model); err != nil {
		writeError(w, http.StatusBadGateway, "pull_failed", err.Error())
		return
	}
	writeData(w, http.StatusOK, map[string]string{"model": req.Model}, nil)
}
