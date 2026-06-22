package opml

import (
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

// FuzzOPMLDecode fuzzes the XML decode that Import runs on an uploaded OPML
// document — the only attacker-controlled step before any DB work. Mirrors
// Import's LimitReader-bounded decode (encoding/xml is XXE-safe by default; the
// DOCTYPE/ENTITY seed guards against a regression). Must never panic.
func FuzzOPMLDecode(f *testing.F) {
	f.Add(`<?xml version="1.0"?><opml version="2.0"><body>` +
		`<outline title="News"><outline type="rss" title="x" xmlUrl="https://e.test/f"/></outline>` +
		`</body></opml>`)
	f.Add(`<opml><body><outline title="a"><outline xmlUrl="`)
	f.Add(`<!DOCTYPE opml [<!ENTITY xxe "boom">]><opml><body><outline text="&xxe;"/></body></opml>`)
	f.Add(``)
	f.Fuzz(func(t *testing.T, s string) {
		var doc opmlDoc
		_ = xml.NewDecoder(io.LimitReader(strings.NewReader(s), maxOPMLBytes)).Decode(&doc)
	})
}
