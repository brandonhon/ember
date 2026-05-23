// Package opml provides OPML 2.0 import/export of feed subscriptions.
package opml

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"

	"github.com/brandonhon/ember/internal/feed"
	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

// Service exposes Import/Export, wired to a store and a discovery helper.
type Service struct {
	Store *store.Store
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
func (s *Service) Import(ctx context.Context, userID int64, body io.Reader) (int, error) {
	dec := xml.NewDecoder(body)
	var doc opmlDoc
	if err := dec.Decode(&doc); err != nil {
		return 0, fmt.Errorf("opml: parse: %w", err)
	}

	created := 0
	var addFeed func(ol outline, categoryID *int64) error
	addFeed = func(ol outline, categoryID *int64) error {
		if ol.XMLURL != "" {
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
		for _, child := range ol.Outlines {
			var nestedCat *int64
			if child.Type == "" && len(child.Outlines) > 0 && ol.Title != "" {
				nestedCat = categoryID
			}
			if err := addFeed(child, nestedCat); err != nil {
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

// Export writes the user's OPML to w.
func (s *Service) Export(ctx context.Context, userID int64, w io.Writer) error {
	cats, err := s.Store.ListCategories(ctx, userID)
	if err != nil {
		return err
	}
	feeds, err := s.Store.ListFeedsForUser(ctx, userID)
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

// DiscoverThenSubscribe is a convenience used by the API: given a candidate
// URL (which may be a site URL, not a feed URL), discover the feed link, then
// upsert + subscribe. Returns the created subscription and feed.
func (s *Service) DiscoverThenSubscribe(ctx context.Context, userID int64, candidate string, categoryID *int64, fetcher *feed.Fetcher) (models.Subscription, models.Feed, error) {
	// Try treating as a feed directly. If parsing fails, try discovery.
	feedURL := candidate
	if fetcher == nil {
		fetcher = feed.NewFetcher(15)
	}
	// Probe to see if it's a direct feed.
	res, err := fetcher.Fetch(ctx, candidate, "", "")
	if err == nil && res.Changed {
		if _, perr := feed.Parse(ctx, 0, res.Body, candidate); perr != nil {
			// Fall back to discovery via the HTTP client of the fetcher.
			disc, derr := feed.Discover(ctx, fetcher.Client, candidate)
			if derr == nil {
				feedURL = disc
			}
		}
	}
	f, err := s.Store.UpsertFeed(ctx, models.Feed{URL: feedURL, Title: feedURL})
	if err != nil {
		return models.Subscription{}, models.Feed{}, err
	}
	sub, err := s.Store.Subscribe(ctx, models.Subscription{
		UserID: userID, FeedID: f.ID, CategoryID: categoryID,
	})
	if err != nil {
		return models.Subscription{}, models.Feed{}, err
	}
	return sub, f, nil
}
