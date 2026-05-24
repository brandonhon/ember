package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// handleMetrics emits the poller's atomic counters in Prometheus text format
// (suitable for scraping by Prometheus or just `curl`-grep'ing).
//
// Counters are namespaced 'ember_*'. No labels yet — this is the simplest
// possible thing that works.
func (d *Dependencies) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	if d.Metrics == nil {
		// Still emit a build-info gauge so scrapers don't 404.
		_, _ = fmt.Fprintln(w, "# HELP ember_build_info Build version of the running ember binary.")
		_, _ = fmt.Fprintln(w, "# TYPE ember_build_info gauge")
		_, _ = fmt.Fprintf(w, "ember_build_info{version=%q} 1\n", Version)
		return
	}
	snapshot := d.Metrics.MetricsSnapshot()
	// Stable order for deterministic output.
	keys := make([]string, 0, len(snapshot))
	for k := range snapshot {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString("# HELP ember_build_info Build version of the running ember binary.\n")
	b.WriteString("# TYPE ember_build_info gauge\n")
	fmt.Fprintf(&b, "ember_build_info{version=%q} 1\n", Version)

	for _, k := range keys {
		metric := "ember_" + strings.ToLower(k)
		fmt.Fprintf(&b, "# TYPE %s counter\n", metric)
		fmt.Fprintf(&b, "%s %d\n", metric, snapshot[k])
	}
	_, _ = w.Write([]byte(b.String()))
}
