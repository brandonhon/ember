package ttrss

import (
	"context"
	"strings"
	"testing"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

const sampleExport = `<articles schema-version="143">
  <article>
    <guid><![CDATA[guid-1]]></guid>
    <title><![CDATA[First starred]]></title>
    <content><![CDATA[<p>Body one</p>]]></content>
    <marked>1</marked>
    <published>0</published>
    <score>0</score>
    <note></note>
    <link><![CDATA[https://example.com/one]]></link>
    <tag_cache><![CDATA[tech,news]]></tag_cache>
    <label_cache><![CDATA[[]]]></label_cache>
    <feed_title><![CDATA[Example Blog]]></feed_title>
    <feed_url><![CDATA[https://example.com/feed.xml]]></feed_url>
    <updated>2021-05-01 13:45:00</updated>
  </article>
  <article>
    <guid><![CDATA[guid-2]]></guid>
    <title><![CDATA[Archived no feed]]></title>
    <content><![CDATA[<p>Body two</p>]]></content>
    <marked>0</marked>
    <link><![CDATA[https://example.com/two]]></link>
    <feed_title></feed_title>
    <feed_url></feed_url>
    <updated>2020-01-02 00:00:00</updated>
  </article>
  <article>
    <guid></guid>
    <title><![CDATA[No guid no link]]></title>
    <content><![CDATA[unusable]]></content>
    <link></link>
  </article>
  <article>
    <guid><![CDATA[guid-js]]></guid>
    <title><![CDATA[Dangerous link]]></title>
    <content><![CDATA[body]]></content>
    <link><![CDATA[javascript:alert(1)]]></link>
  </article>
</articles>`

func TestImport(t *testing.T) {
	s := store.NewTest(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(s)

	res, err := svc.Import(ctx, u.ID, strings.NewReader(sampleExport))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if res.Total != 4 {
		t.Errorf("Total = %d, want 4", res.Total)
	}
	if res.Skipped != 1 { // the no-guid/no-link article
		t.Errorf("Skipped = %d, want 1", res.Skipped)
	}
	if res.Imported != 3 { // two real + the javascript-link one (guid present)
		t.Errorf("Imported = %d, want 3", res.Imported)
	}

	// All three usable articles must surface in the Starred view (proves the
	// import feed is non-muted; the starred query excludes muted feeds).
	starred, err := s.ListArticles(ctx, u.ID, store.ListArticlesQuery{View: "starred", Limit: 50, OnlySummarized: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(starred) != 3 {
		t.Fatalf("starred view has %d articles, want 3", len(starred))
	}
	for _, a := range starred {
		if !a.IsStarred {
			t.Errorf("article %d not starred", a.ID)
		}
		if !a.IsRead {
			t.Errorf("article %d not marked read (migrated history should be read)", a.ID)
		}
	}

	// Body HTML should be reduced to plain text so cards get an excerpt and
	// the article is full-text searchable (e.g. "Body one" from <p>Body one</p>).
	var first *models.ArticleView
	for i := range starred {
		if starred[i].Title == "First starred" {
			first = &starred[i]
		}
	}
	if first == nil {
		t.Fatal("first article missing")
	}
	if !strings.Contains(first.ContentText, "Body one") {
		t.Errorf("content_text not derived from HTML, got %q", first.ContentText)
	}

	// The javascript: link must have been dropped to "" (stored, but href
	// neutralized).
	var jsArticle *models.ArticleView
	for i := range starred {
		if starred[i].Title == "Dangerous link" {
			jsArticle = &starred[i]
		}
	}
	if jsArticle == nil {
		t.Fatal("dangerous-link article missing")
	}
	if jsArticle.URL != "" {
		t.Errorf("javascript: link should be blanked, got %q", jsArticle.URL)
	}
}

func TestImport_Idempotent(t *testing.T) {
	s := store.NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	svc := NewService(s)

	if _, err := svc.Import(ctx, u.ID, strings.NewReader(sampleExport)); err != nil {
		t.Fatal(err)
	}
	// Re-import the same file: no new inserts, same total, still 3 starred.
	res, err := svc.Import(ctx, u.ID, strings.NewReader(sampleExport))
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 0 {
		t.Errorf("re-import Imported = %d, want 0 (dedup)", res.Imported)
	}
	if res.Total != 4 {
		t.Errorf("re-import Total = %d, want 4", res.Total)
	}
	starred, _ := s.ListArticles(ctx, u.ID, store.ListArticlesQuery{View: "starred", Limit: 50, OnlySummarized: true})
	if len(starred) != 3 {
		t.Errorf("after re-import, starred = %d, want 3", len(starred))
	}
}

func TestImport_Malformed(t *testing.T) {
	s := store.NewTest(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, models.User{Username: "u", PasswordHash: "h"})
	svc := NewService(s)

	_, err := svc.Import(ctx, u.ID, strings.NewReader("<articles><article><guid>x</guid"))
	if err == nil {
		t.Error("expected error on malformed XML")
	}
}
