package api

import (
	"net/http"
)

// handleDrainSummaryQueue finalizes every article the summarizer has not
// stamped, marking it 'disabled' so it becomes visible immediately and drops
// out of the "Summarizing N articles" indicator.
//
// This is the supported recovery path for a runaway backlog. Before it existed
// the only way out was hand-written SQL against ember.db (issue #198), and the
// obvious guess — setting summary IS NOT NULL — did not work, because the state
// lives in summary_model, not in the summary text. See docs/summarization.md.
//
// Destructive only in the sense that the drained articles will not be
// summarized unless an admin requeues them; no text is lost, because a pending
// article has no summary yet by definition.
func (d *Dependencies) handleDrainSummaryQueue(w http.ResponseWriter, r *http.Request) {
	n, err := d.Store.MarkUnsummarizedDisabled(r.Context())
	if err != nil {
		internalError(w, "internal", err)
		return
	}
	writeData(w, http.StatusOK, map[string]int64{"drained": n}, nil)
}

// handleRequeueSummaries clears the 'disabled' marker left by a drain (or by a
// spell with summaries switched off) and puts those articles back on the queue.
// The inverse of handleDrainSummaryQueue. Articles that genuinely failed
// ('skipped') or were opted out per-feed ('excluded') are deliberately left
// alone — resummarize-all and the per-feed action cover those.
func (d *Dependencies) handleRequeueSummaries(w http.ResponseWriter, r *http.Request) {
	ids, err := d.Store.ResetDisabledSummaries(r.Context())
	if err != nil {
		internalError(w, "internal", err)
		return
	}
	writeData(w, http.StatusOK, map[string]int{
		"reset":    len(ids),
		"enqueued": d.enqueueSummaries(ids),
	}, nil)
}
