package models

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// mustBeHidden lists every field that MUST carry `json:"-"`. These are either
// credentials (a password hash, an API token, raw WebAuthn key material) or
// internal dedup keys that clients have no business seeing. Dropping the tag
// on any of them silently starts serializing it — /api/me returns models.User
// wholesale, and the feed list returns models.Feed embedded in
// FeedWithCounts, so a removed tag reaches real clients immediately.
var mustBeHidden = map[string][]string{
	"User":    {"PasswordHash", "FeverToken"},
	"Article": {"CanonicalURL", "ClusterID", "TitleFingerprint"},
	"Passkey": {"CredentialID", "PublicKey", "AttestationTyp", "AAGUID", "SignCount"},
}

var samples = map[string]any{
	"User":    User{},
	"Article": Article{},
	"Passkey": Passkey{},
}

func TestSecretFieldsAreTaggedHidden(t *testing.T) {
	for name, fields := range mustBeHidden {
		typ := reflect.TypeOf(samples[name])
		for _, f := range fields {
			sf, ok := typ.FieldByName(f)
			if !ok {
				t.Errorf("%s.%s no longer exists — update mustBeHidden", name, f)
				continue
			}
			if tag := sf.Tag.Get("json"); tag != "-" {
				t.Errorf("%s.%s has json tag %q, want \"-\" (it would be serialized to clients)", name, f, tag)
			}
		}
	}
}

// Belt and braces: populate every hidden field with a sentinel, marshal, and
// require the sentinel to be absent. Catches cases a tag check alone would
// miss — notably struct embedding, where ArticleView embeds Article and
// FeedWithCounts embeds Feed and Go promotes the inner fields.
func TestSecretValuesNeverAppearInJSON(t *testing.T) {
	const secret = "SENTINEL-MUST-NOT-APPEAR"

	u := User{ID: 1, Username: "alice", PasswordHash: secret, FeverToken: secret}
	a := Article{ID: 1, Title: "t", CanonicalURL: secret, ClusterID: secret, TitleFingerprint: secret}
	pk := Passkey{
		ID: 1, Name: "key",
		CredentialID: []byte(secret), PublicKey: []byte(secret),
		AttestationTyp: secret, AAGUID: []byte(secret),
	}

	for _, tc := range []struct {
		name string
		v    any
	}{
		{"User", u},
		{"Article", a},
		{"Passkey", pk},
		// Embedded forms — these are the shapes actually returned by the API.
		{"ArticleView (embeds Article)", ArticleView{Article: a, IsRead: true}},
		{"FeedWithCounts (embeds Feed)", FeedWithCounts{Feed: Feed{ID: 1, URL: "u"}, Unread: 3}},
		{"slice of ArticleView", []ArticleView{{Article: a}}},
		{"map wrapper", map[string]any{"user": u, "articles": []Article{a}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.v)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if strings.Contains(string(b), secret) {
				t.Errorf("hidden field leaked into JSON: %s", b)
			}
		})
	}
}

// The API relies on these being present — a renamed json tag is a silent
// breaking change for the SPA, which reads them by name.
func TestWireNamesAreStable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		v     any
		want  []string
		avoid []string
	}{
		{"User", User{}, []string{"id", "username", "is_admin", "settings_json", "created_at"}, []string{"password_hash", "fever_token"}},
		{"ArticleView", ArticleView{}, []string{"id", "feed_id", "title", "is_read", "is_starred", "is_later", "dup_count"}, []string{"canonical_url", "cluster_id", "title_fingerprint"}},
		{"FeedWithCounts", FeedWithCounts{}, []string{"id", "url", "title", "subscription_id", "muted", "position", "unread"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.v)
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatal(err)
			}
			for _, k := range tc.want {
				if _, ok := got[k]; !ok {
					t.Errorf("wire field %q missing from %s: %s", k, tc.name, b)
				}
			}
			for _, k := range tc.avoid {
				if _, ok := got[k]; ok {
					t.Errorf("unexpected wire field %q in %s", k, tc.name)
				}
			}
		})
	}
}
