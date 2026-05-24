package api

import (
	"context"
	"net/http"
	"time"

	"github.com/brandonhon/ember/internal/summarize"
	"github.com/brandonhon/ember/internal/sysinfo"
)

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
