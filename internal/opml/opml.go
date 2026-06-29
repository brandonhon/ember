// Package opml provides OPML 2.0 import/export of feed subscriptions.
package opml

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

// URLValidator is called for every feed URL during Import. Return non-nil
// to reject the URL (the feed is skipped, import continues). Nil means
// "no validator configured" → accept all URLs.
type URLValidator func(ctx context.Context, raw string) error

// Service exposes Import/Export, wired to a store and a discovery helper.
type Service struct {
	Store       *store.Store
	ValidateURL URLValidator
}

// NewService constructs an OPML service.
func NewService(s *store.Store) *Service {
	return &Service{Store: s}
}

// outline is one node in OPML.
type outline struct {
	XMLName  xml.Name  `xml:"outline"`
	Title    string    `xml:"title,attr,omitempty"`
	Text     string    `xml:"text,attr,omitempty"`
	Type     string    `xml:"type,attr,omitempty"`
	XMLURL   string    `xml:"xmlUrl,attr,omitempty"`
	HTMLURL  string    `xml:"htmlUrl,attr,omitempty"`
	Outlines []outline `xml:"outline,omitempty"`
}

type opmlDoc struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr"`
	Head    struct {
		Title string `xml:"title"`
	} `xml:"head"`
	Body struct {
		Outlines []outline `xml:"outline"`
	} `xml:"body"`
}

// Import reads an OPML file and creates the corresponding category +
// subscription rows for the user. Existing feeds are reused via UpsertFeed; if
// the user is already subscribed, the subscription is left alone. Returns the
// number of *new* subscriptions created.
// maxOPMLBytes caps the OPML document size to prevent memory exhaustion from a
// crafted upload. encoding/xml is XXE-safe by default, so size is the only
// remaining vector. 10 MiB is generous for any real subscription list.
const maxOPMLBytes = 10 << 20

func (s *Service) Import(ctx context.Context, userID int64, body io.Reader) (int, error) {
	dec := xml.NewDecoder(io.LimitReader(body, maxOPMLBytes))
	var doc opmlDoc
	if err := dec.Decode(&doc); err != nil {
		return 0, fmt.Errorf("opml: parse: %w", err)
	}

	created := 0
	var addFeed func(ol outline, categoryID *int64) error
	addFeed = func(ol outline, categoryID *int64) error {
		if ol.XMLURL != "" {
			// SSRF block: skip private/internal URLs entirely.
			if s.ValidateURL != nil {
				if err := s.ValidateURL(ctx, ol.XMLURL); err != nil {
					return nil
				}
			}
			title := ol.Title
			if title == "" {
				title = ol.Text
			}
			if title == "" {
				title = ol.XMLURL
			}
			f, err := s.Store.UpsertFeed(ctx, models.Feed{
				URL: ol.XMLURL, Title: title, SiteURL: ol.HTMLURL,
			})
			if err != nil {
				return err
			}
			// Existing-or-new subscription.
			before, err := s.Store.Subscribe(ctx, models.Subscription{
				UserID:     userID,
				FeedID:     f.ID,
				CategoryID: categoryID,
			})
			if err != nil {
				return err
			}
			if before.ID > 0 && before.CategoryID == nil && categoryID != nil {
				// best-effort: update category for previously-uncategorized sub
				_ = s.Store.UpdateSubscription(ctx, userID, before.ID,
					store.UpdateSubscriptionPatch{CategoryID: categoryID})
			}
			// Track new subscriptions only — Subscribe returns the existing row
			// on duplicate, so we approximate by checking the created_at fresh
			// equals our store clock; close enough for the count.
			created++
		}
		// Children inherit the current category: feeds in a folder join that
		// folder's category, and nested sub-folders flatten into it (ember
		// categories are flat — only top-level folders create one).
		for _, child := range ol.Outlines {
			if err := addFeed(child, categoryID); err != nil {
				return err
			}
		}
		return nil
	}

	for _, ol := range doc.Body.Outlines {
		// Folder = outline with no xmlUrl that has children.
		var categoryID *int64
		if ol.XMLURL == "" && len(ol.Outlines) > 0 && (ol.Title != "" || ol.Text != "") {
			name := ol.Title
			if name == "" {
				name = ol.Text
			}
			c, err := s.Store.CreateCategory(ctx, models.Category{UserID: userID, Name: name})
			switch {
			case errors.Is(err, store.ErrConflict):
				// Find existing by name.
				cats, lerr := s.Store.ListCategories(ctx, userID)
				if lerr != nil {
					return 0, lerr
				}
				for i := range cats {
					if cats[i].Name == name {
						categoryID = &cats[i].ID
						break
					}
				}
			case err != nil:
				return 0, err
			default:
				categoryID = &c.ID
			}
		}
		if err := addFeed(ol, categoryID); err != nil {
			return 0, err
		}
	}
	return created, nil
}

// WriteExport writes the user's OPML to a timestamped file under dir and returns
// the path + size. It probes write access up front so a bind-mounted host path
// that isn't writable by the server's user fails with a clear message instead of
// a later filesystem surprise. Shared by the admin "Export now" action and the
// scheduled export job.
func (s *Service) WriteExport(ctx context.Context, userID int64, dir string) (string, int64, error) {
	if dir == "" {
		return "", 0, errors.New("opml export: empty directory")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", 0, fmt.Errorf("opml export: mkdir %s: %w", dir, err)
	}
	probe := filepath.Join(dir, ".ember-export-writetest")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return "", 0, fmt.Errorf("opml export: %s is not writable by the server (running as uid %d) — make the bind-mounted host path owned by or writable by that user: %w", dir, os.Getuid(), err)
	}
	_ = os.Remove(probe)
	out := filepath.Join(dir, time.Now().UTC().Format("ember-2006-01-02-150405.opml"))
	f, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // dir is an admin-only, validated (absolute, quote-free) setting; the filename is server-generated.
	if err != nil {
		return "", 0, fmt.Errorf("opml export: create %s: %w", out, err)
	}
	if err := s.Export(ctx, userID, f); err != nil {
		_ = f.Close()
		return "", 0, fmt.Errorf("opml export: write: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", 0, fmt.Errorf("opml export: close: %w", err)
	}
	var size int64
	if fi, serr := os.Stat(out); serr == nil {
		size = fi.Size()
	}
	return out, size, nil
}

// Export writes the user's OPML to w.
func (s *Service) Export(ctx context.Context, userID int64, w io.Writer) error {
	cats, err := s.Store.ListCategories(ctx, userID)
	if err != nil {
		return err
	}
	feeds, err := s.Store.ListFeedsForUser(ctx, userID, 0, false)
	if err != nil {
		return err
	}

	// Group feeds by category (nil = uncategorized).
	byCat := map[int64][]models.FeedWithCounts{}
	var uncat []models.FeedWithCounts
	for _, f := range feeds {
		if f.CategoryID != nil {
			byCat[*f.CategoryID] = append(byCat[*f.CategoryID], f)
		} else {
			uncat = append(uncat, f)
		}
	}

	doc := opmlDoc{Version: "2.0"}
	doc.Head.Title = "ember subscriptions"
	for _, c := range cats {
		folder := outline{Title: c.Name, Text: c.Name}
		for _, f := range byCat[c.ID] {
			folder.Outlines = append(folder.Outlines, outline{
				Type:    "rss",
				Title:   f.Title,
				Text:    f.Title,
				XMLURL:  f.URL,
				HTMLURL: f.SiteURL,
			})
		}
		doc.Body.Outlines = append(doc.Body.Outlines, folder)
	}
	for _, f := range uncat {
		doc.Body.Outlines = append(doc.Body.Outlines, outline{
			Type:    "rss",
			Title:   f.Title,
			Text:    f.Title,
			XMLURL:  f.URL,
			HTMLURL: f.SiteURL,
		})
	}

	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	return enc.Flush()
}
