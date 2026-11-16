package feed

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A direct feed URL served with a generic XML content type must be returned
// as-is. Previously it fell through to HTML scraping and then the site-root
// fallbacks, so /tags/go/index.xml came back as the DIFFERENT feed /index.xml.
// Covers both content types, the three feed roots, a prolog (declaration,
// stylesheet PI, comment, BOM) and both entry points.
func TestDiscover_GenericXMLFeedReturnsTarget(t *testing.T) {
	bodies := map[string]string{
		"rss": `<?xml version="1.0" encoding="UTF-8"?>
<?xml-stylesheet type="text/xsl" href="/rss.xsl"?>
<!-- generated -->
<rss version="2.0"><channel><title>Go tag</title></channel></rss>`,
		"atom": `<FEED xmlns="http://www.w3.org/2005/Atom"><title>Go tag</title></FEED>`,
		"rdf":  "\xef\xbb\xbf<?xml version=\"1.0\"?>\n<rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\"></rdf:RDF>",
	}
	for _, ct := range []string{"application/xml", "text/xml; charset=utf-8"} {
		for name, body := range bodies {
			mux := http.NewServeMux()
			mux.HandleFunc("/tags/go/index.xml", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", ct)
				_, _ = w.Write([]byte(body))
			})
			mux.HandleFunc("/index.xml", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/rss+xml")
				_, _ = w.Write([]byte(`<rss version="2.0"><channel><title>Site</title></channel></rss>`))
			})
			srv := httptest.NewServer(mux)
			target := srv.URL + "/tags/go/index.xml"

			got, err := Discover(context.Background(), srv.Client(), target, allowAll)
			if err != nil || got != target {
				t.Errorf("%s/%s: Discover = %q, %v; want the target itself", ct, name, got, err)
			}
			all, err := DiscoverAll(context.Background(), srv.Client(), target, allowAll)
			if err != nil || len(all) != 1 || all[0].URL != target {
				t.Errorf("%s/%s: DiscoverAll = %+v, %v; want just the target", ct, name, all, err)
			}
			srv.Close()
		}
	}
}

// An HTML page served as XML (XHTML) is not a feed: sniffing must look at the
// root element, not merely find "<feed" somewhere in the body.
func TestDiscover_XHTMLIsNotSniffedAsFeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><!DOCTYPE html><html><body><p>&lt;feed&gt; <rss></p></body></html>`))
	}))
	defer srv.Close()
	if got, err := Discover(context.Background(), srv.Client(), srv.URL+"/", allowAll); !errors.Is(err, ErrNoFeed) {
		t.Errorf("Discover = %q, %v; want ErrNoFeed", got, err)
	}
}
