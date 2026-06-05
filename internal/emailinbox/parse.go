package emailinbox

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"

	"github.com/brandonhon/ember/internal/feed"
	"github.com/brandonhon/ember/internal/models"
)

// ParseMessage turns a raw RFC 5322 message into an Article skeleton.
// FeedID + GUID + FetchedAt are NOT set — the store layer fills those.
// HTML body is preferred; falls back to text/plain. Inline attachments
// are ignored (they'd bloat the DB for marginal value).
func ParseMessage(raw []byte) (models.Article, error) {
	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		return models.Article{}, fmt.Errorf("emailinbox: read message: %w", err)
	}

	title := decodeMimeHeader(msg.Header.Get("Subject"))
	from := decodeMimeHeader(msg.Header.Get("From"))
	if title == "" {
		title = "(no subject)"
	}

	pub := time.Now()
	if d, err := mail.ParseDate(msg.Header.Get("Date")); err == nil && !d.IsZero() {
		pub = d
	}

	bodyHTML, bodyText, err := extractBodies(msg.Header, msg.Body)
	if err != nil {
		return models.Article{}, fmt.Errorf("emailinbox: extract bodies: %w", err)
	}
	// Inbound email is untrusted and the body is rendered via {@html} in the
	// reader. Sanitize before deriving text and storing, mirroring the feed
	// ingest path (feed/parse.go) so the email path isn't a stored-XSS hole.
	bodyHTML = feed.SanitizeHTML(bodyHTML)
	if bodyText == "" && bodyHTML != "" {
		bodyText = feed.HTMLToText(bodyHTML)
	}

	// GUID seed: combine Message-Id (if present) with Subject + Date for a
	// stable per-message id. The store layer's content_hash dedup also
	// catches re-sends with the same Subject+body.
	guid := msg.Header.Get("Message-Id")
	if guid == "" {
		guid = fmt.Sprintf("ember-email-%d-%s", pub.Unix(), title)
	}

	art := models.Article{
		GUID:        strings.Trim(guid, "<>"),
		Title:       title,
		Author:      parseFromAddress(from),
		ContentHTML: bodyHTML,
		ContentText: bodyText,
		PublishedAt: pub.Unix(),
		ContentHash: feed.ContentHash("", title, bodyText),
		// URL stays empty — there's no canonical web URL for an email.
		// Cluster_id stays empty too, which lets every email row pass the
		// dedup predicate (the title-fingerprint branch will still catch
		// duplicate newsletter sends within a 48h window).
		TitleFingerprint: feed.TitleFingerprint(title),
	}
	return art, nil
}

// extractBodies returns (html, text). Either may be empty. Walks
// multipart trees recursively, preferring the first text/html and the
// first text/plain.
func extractBodies(h mail.Header, body io.Reader) (string, string, error) {
	contentType := h.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		// No Content-Type → assume text/plain.
		mediaType = "text/plain"
		params = map[string]string{}
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return "", "", errors.New("multipart without boundary")
		}
		return walkMultipart(body, boundary, 0)
	}

	// Single-part. Decode by transfer-encoding then route by content type.
	raw, err := decodeBody(body, h.Get("Content-Transfer-Encoding"), params["charset"])
	if err != nil {
		return "", "", err
	}
	switch {
	case strings.HasPrefix(mediaType, "text/html"):
		return string(raw), "", nil
	default:
		return "", string(raw), nil
	}
}

// maxMultipartDepth caps multipart nesting. A crafted message can nest
// multipart/* containers thousands deep within the size limit; without a cap
// the recursion below overflows the SMTP goroutine's stack and crashes the
// process. 10 is far beyond any legitimate newsletter.
const maxMultipartDepth = 10

// walkMultipart recurses into multipart bodies, picking the first
// text/html and first text/plain found anywhere. Skips attachments and
// embedded images. depth guards against deeply-nested multipart bombs.
func walkMultipart(body io.Reader, boundary string, depth int) (string, string, error) {
	if depth > maxMultipartDepth {
		return "", "", nil // too deep — stop descending, keep what we have
	}
	mr := multipart.NewReader(body, boundary)
	var html, text string
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return html, text, fmt.Errorf("read part: %w", err)
		}
		mt, params, perr := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if perr != nil {
			mt = "text/plain"
			params = map[string]string{}
		}
		switch {
		case strings.HasPrefix(mt, "multipart/"):
			subB := params["boundary"]
			if subB == "" {
				part.Close()
				continue
			}
			h, t, _ := walkMultipart(part, subB, depth+1)
			if html == "" {
				html = h
			}
			if text == "" {
				text = t
			}
		case strings.HasPrefix(mt, "text/html"):
			if html == "" {
				raw, _ := decodeBody(part, part.Header.Get("Content-Transfer-Encoding"), params["charset"])
				html = string(raw)
			}
		case strings.HasPrefix(mt, "text/plain"):
			if text == "" {
				raw, _ := decodeBody(part, part.Header.Get("Content-Transfer-Encoding"), params["charset"])
				text = string(raw)
			}
		}
		part.Close()
	}
	return html, text, nil
}

// decodeBody applies the message's Content-Transfer-Encoding (mostly
// 7bit / 8bit / quoted-printable / base64). Charsets other than UTF-8
// are read as-is — corrupted display is the only cost and full charset
// transcoding pulls in a heavy dep.
func decodeBody(r io.Reader, encoding, charset string) ([]byte, error) {
	_ = charset // accepted but not used for transcoding
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "quoted-printable":
		return io.ReadAll(quotedprintable.NewReader(r))
	default:
		// 7bit / 8bit / no encoding all read raw. base64 is rare for
		// text/* parts but supported by the standard reader chain;
		// callers can extend here if needed.
		return io.ReadAll(r)
	}
}

// decodeMimeHeader handles RFC 2047 encoded-word headers ("=?utf-8?B?...?=").
// Falls back to the raw string on decode failure.
func decodeMimeHeader(h string) string {
	dec := mime.WordDecoder{}
	out, err := dec.DecodeHeader(h)
	if err != nil {
		return h
	}
	return strings.TrimSpace(out)
}

// parseFromAddress trims a "From: Name <addr@host>" header down to the
// display name when present, falling back to the address. Used as the
// article author so readers can sort/filter by sender.
func parseFromAddress(h string) string {
	addr, err := mail.ParseAddress(h)
	if err != nil {
		return strings.TrimSpace(h)
	}
	if addr.Name != "" {
		return addr.Name
	}
	return addr.Address
}
