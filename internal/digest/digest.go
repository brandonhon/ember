// Package digest builds + sends the daily digest email. Pulled out of the
// poller so the SMTP code stays self-contained and testable.
package digest

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

// errBadHeader and errBadAddress flag header-injection-shaped inputs. The
// digest package defends against CRLF in any value that ends up in an SMTP
// header (To/From/Subject) so a future caller that forgets to pre-validate
// can't smuggle Bcc / extra headers / a fake body through.
var (
	errBadHeader  = errors.New("digest: header value contains CR/LF")
	errBadAddress = errors.New("digest: invalid email address")
)

// sanitizeAddress validates an email address for SMTP envelope + To header
// use. Rejects CR/LF (header injection) and anything mail.ParseAddress
// won't accept. Returns the canonical "addr-spec" form so the caller can
// pass it straight to smtp.Rcpt / write into "To: ".
func sanitizeAddress(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", errBadAddress
	}
	if strings.ContainsAny(addr, "\r\n") {
		return "", errBadHeader
	}
	parsed, err := mail.ParseAddress(addr)
	if err != nil {
		return "", fmt.Errorf("%w: %v", errBadAddress, err)
	}
	return parsed.Address, nil
}

// sanitizeHeader strips CR/LF from a free-form header value (Subject, app
// name). Returns an error rather than silently mangling so the caller
// surfaces the bug instead of shipping a broken header.
func sanitizeHeader(v string) (string, error) {
	if strings.ContainsAny(v, "\r\n") {
		return "", errBadHeader
	}
	return v, nil
}

// envelope holds the sanitized values that every outbound message needs. Both
// send paths go through newEnvelope so neither can forget a sanitize* call —
// the CRLF checks are what keep a header-injection payload out of To/From/
// Subject.
type envelope struct {
	from, to, appName string
}

// newEnvelope validates the addresses and app name, defaulting the app name to
// "Ember". It runs before any article fetch or network work so a malformed
// value fails fast.
func newEnvelope(from, to, appName string) (envelope, error) {
	cleanTo, err := sanitizeAddress(to)
	if err != nil {
		return envelope{}, err
	}
	cleanFrom, err := sanitizeAddress(from)
	if err != nil {
		return envelope{}, fmt.Errorf("digest: from: %w", err)
	}
	if appName == "" {
		appName = "Ember"
	}
	cleanApp, err := sanitizeHeader(appName)
	if err != nil {
		return envelope{}, fmt.Errorf("digest: app name: %w", err)
	}
	return envelope{from: cleanFrom, to: cleanTo, appName: cleanApp}, nil
}

// build sanitizes the subject and assembles the multipart payload.
func (e envelope) build(subject, textBody, htmlBody string) ([]byte, error) {
	cleanSubject, err := sanitizeHeader(subject)
	if err != nil {
		return nil, err
	}
	return buildMIME(e.from, e.to, cleanSubject, textBody, htmlBody)
}

// SMTPConfig is what the sender needs to talk to an upstream SMTP relay.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	StartTLS bool
}

// Configured reports whether SMTP has the minimum settings to send. Lets the
// runner skip the work entirely on hosts that don't configure mail.
func (c SMTPConfig) Configured() bool {
	return c.Host != "" && c.Port != 0 && c.From != ""
}

// SendTestMessage sends a one-shot diagnostic email to verify the supplied
// SMTP config end-to-end. Reuses the same transport path as digest delivery
// so a passing test means the digest sender will work. Returns the underlying
// error (network, auth, TLS, etc.) so the admin UI can surface it.
func SendTestMessage(cfg SMTPConfig, to, appName string) error {
	env, err := newEnvelope(cfg.From, to, appName)
	if err != nil {
		return err
	}
	textBody := env.appName + " SMTP test message.\n\nIf you're reading this in your inbox, the relay accepted ember's outbound mail.\n"
	htmlBody := `<!doctype html><html><body style="font-family:Georgia,serif;padding:24px;color:#211d18;background:#f6f2e9;">` +
		`<h1 style="font-weight:500;font-size:20px;margin:0 0 16px;">` + html.EscapeString(env.appName) + ` SMTP test</h1>` +
		`<p style="font-size:14px;line-height:1.55;">If you're reading this in your inbox, the relay accepted ember's outbound mail.</p>` +
		`</body></html>`
	msg, err := env.build(env.appName+" — SMTP test", textBody, htmlBody)
	if err != nil {
		return err
	}
	s := &Sender{SMTP: cfg, AppName: env.appName}
	return s.send(env.from, env.to, msg)
}

// Sender ties together a store (to fetch articles + mark sent) and an SMTP
// config (to deliver).
type Sender struct {
	Store *store.Store
	SMTP  SMTPConfig
	// AppName is the brand surfaced in the subject + footer. Defaults to "Ember".
	AppName string
	// SiteURL is what the email's "View on the web" link points at. Optional.
	SiteURL string
}

// SendForUser builds and sends the digest for a single user. Returns the
// number of articles included; 0 means nothing new since last_sent_at and
// no email was sent.
func (s *Sender) SendForUser(ctx context.Context, u models.User, d models.UserDigest) (int, error) {
	if !s.SMTP.Configured() {
		return 0, errors.New("digest: SMTP not configured")
	}
	to := d.EmailOverride
	if to == "" {
		to = u.Email
	}
	if to == "" {
		return 0, errors.New("digest: user has no email")
	}
	env, err := newEnvelope(s.SMTP.From, to, s.AppName)
	if err != nil {
		return 0, err
	}

	articles, err := s.fetchArticles(ctx, d)
	if err != nil {
		return 0, fmt.Errorf("digest: fetch articles: %w", err)
	}
	if len(articles) == 0 {
		return 0, nil
	}

	msg, err := env.build(
		fmt.Sprintf("%s digest — %d new article%s", env.appName, len(articles), plural(len(articles))),
		renderText(env.appName, s.SiteURL, articles),
		renderHTML(env.appName, s.SiteURL, articles),
	)
	if err != nil {
		return 0, err
	}
	if err := s.send(env.from, env.to, msg); err != nil {
		return 0, fmt.Errorf("digest: send: %w", err)
	}
	return len(articles), nil
}

// fetchArticles pulls the articles in the user's chosen view that landed
// since their last send. Capped at 50 to keep emails reasonable.
func (s *Sender) fetchArticles(ctx context.Context, d models.UserDigest) ([]models.ArticleView, error) {
	q := store.ListArticlesQuery{Limit: 50, OnlySummarized: true}
	switch d.ViewKind {
	case "smart":
		q.View = d.ViewValue
		// Default FreshAfter for smart=fresh: only include the last 24h so
		// the digest doesn't redeliver yesterday's items when the user's
		// inbox skipped a day.
		if d.ViewValue == "fresh" {
			q.FreshAfter = time.Now().Add(-24 * time.Hour).Unix()
		}
	case "feed", "category", "board":
		// All three address a scope by numeric id; an unparseable value yields
		// 0, which the store treats as "no scope" and returns nothing.
		id, _ := strconv.ParseInt(d.ViewValue, 10, 64)
		switch d.ViewKind {
		case "feed":
			q.FeedID = id
		case "category":
			q.CategoryID = id
		case "board":
			q.BoardID = id
		}
	}
	articles, err := s.Store.ListArticles(ctx, d.UserID, q)
	if err != nil {
		return nil, err
	}
	// Filter to "since last send" so a user who turned digest on yesterday
	// doesn't receive a wall of week-old content tomorrow.
	if d.LastSentAt > 0 {
		out := articles[:0]
		for _, a := range articles {
			if a.FetchedAt >= d.LastSentAt {
				out = append(out, a)
			}
		}
		articles = out
	}
	return articles, nil
}

// send delivers msg. from/to must already be sanitized (newEnvelope does it) —
// they become the SMTP envelope, so passing the raw config value here would
// leave the envelope unchecked while the From: header was sanitized.
func (s *Sender) send(from, to string, msg []byte) error {
	addr := fmt.Sprintf("%s:%d", s.SMTP.Host, s.SMTP.Port)
	if !s.SMTP.StartTLS {
		// Plain SMTP sends credentials + message body unencrypted. Only allow
		// it to a loopback host (a local relay / sidecar). For any other host
		// require StartTLS so a misconfiguration can't silently ship the SMTP
		// password and digest content in the clear across the network.
		if !isLoopbackHost(s.SMTP.Host) {
			return errors.New("digest: plain SMTP (StartTLS off) is only allowed to a loopback host; " +
				"enable StartTLS for a remote relay")
		}
		auth := s.smtpAuth()
		return smtp.SendMail(addr, auth, from, []string{to}, msg)
	}
	c, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Hello("ember"); err != nil {
		return err
	}
	// StartTLS:true means "require TLS or fail" — never silently send
	// credentials/body in plaintext. A server that doesn't advertise STARTTLS
	// (or a MitM that stripped it from the EHLO response) must be an error, not
	// a downgrade.
	if ok, _ := c.Extension("STARTTLS"); !ok {
		return errors.New("digest: SMTP server did not offer STARTTLS but StartTLS is required")
	}
	if err := c.StartTLS(&tls.Config{ServerName: s.SMTP.Host, MinVersion: tls.VersionTLS12}); err != nil {
		return err
	}
	if auth := s.smtpAuth(); auth != nil {
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func (s *Sender) smtpAuth() smtp.Auth {
	if s.SMTP.Username == "" {
		return nil
	}
	return smtp.PlainAuth("", s.SMTP.Username, s.SMTP.Password, s.SMTP.Host)
}

// isLoopbackHost reports whether host is the local machine — "localhost"
// (case-insensitive), a loopback IP literal, or a hostname that resolves
// exclusively to loopback addresses (e.g. a Docker service name mapped to
// 127.x). Used to gate plain (non-TLS) SMTP so credentials only ever
// traverse the loopback interface in the clear.
func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(host)
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	// Resolve and check: if every resolved address is loopback, treat as local.
	addrs, err := net.LookupHost(host)
	if err != nil || len(addrs) == 0 {
		return false
	}
	for _, addr := range addrs {
		if ip := net.ParseIP(addr); ip == nil || !ip.IsLoopback() {
			return false
		}
	}
	return true
}

// buildMIME assembles the multipart/alternative payload.
//
// The boundary MUST be unpredictable. Article titles reach the text/plain part
// verbatim (renderText writes them raw), so an attacker who controls a feed and
// can guess the boundary could close the part early and forge MIME sections —
// arbitrary attachments or a spoofed body. Randomness is the only thing
// preventing that, which is why a crypto/rand failure is a hard error here
// rather than a silent fallback to a fixed boundary. Mirrors the fail-closed
// rationale on mustRandHex in internal/api.
func buildMIME(from, to, subject, textBody, htmlBody string) ([]byte, error) {
	var b bytes.Buffer
	var rnd [8]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return nil, fmt.Errorf("digest: generating MIME boundary: %w", err)
	}
	boundary := "ember-" + hex.EncodeToString(rnd[:])
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary)

	fmt.Fprintf(&b, "--%s\r\n", boundary)
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(textBody)
	b.WriteString("\r\n")

	fmt.Fprintf(&b, "--%s\r\n", boundary)
	b.WriteString("Content-Type: text/html; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(htmlBody)
	b.WriteString("\r\n")

	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return b.Bytes(), nil
}

func renderHTML(appName, siteURL string, articles []models.ArticleView) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><body style="font-family:Georgia,serif;max-width:640px;margin:0 auto;padding:24px;background:#f6f2e9;color:#211d18;">`)
	fmt.Fprintf(&b, `<h1 style="font-family:Georgia,serif;font-weight:500;font-size:22px;margin:0 0 18px;">%s digest</h1>`, html.EscapeString(appName))
	fmt.Fprintf(&b, `<p style="color:#6a604f;font-size:13px;margin:0 0 22px;">%d article%s waiting for you.</p>`,
		len(articles), plural(len(articles)))
	for _, a := range articles {
		fmt.Fprintf(&b, `<div style="border-top:1px solid #e2dac9;padding:16px 0;">`)
		linkURL := a.URL
		if linkURL == "" && siteURL != "" {
			linkURL = siteURL
		}
		if linkURL != "" {
			fmt.Fprintf(&b, `<a href="%s" style="color:#211d18;text-decoration:none;"><strong style="font-size:16px;">%s</strong></a>`,
				html.EscapeString(linkURL), html.EscapeString(a.Title))
		} else {
			fmt.Fprintf(&b, `<strong style="font-size:16px;">%s</strong>`, html.EscapeString(a.Title))
		}
		if summary := summaryParagraph(a.Summary); summary != "" {
			fmt.Fprintf(&b, `<p style="font-size:13.5px;line-height:1.55;color:#3f3930;margin:8px 0 0;">%s</p>`, html.EscapeString(summary))
		}
		b.WriteString(`</div>`)
	}
	if siteURL != "" {
		fmt.Fprintf(&b, `<p style="margin-top:28px;font-size:12px;color:#6a604f;"><a style="color:#a93b16;" href="%s">View these on the web</a></p>`, html.EscapeString(siteURL))
	}
	b.WriteString(`<p style="margin-top:8px;font-size:11px;color:#847a68;">Sent by your self-hosted Ember instance.</p>`)
	b.WriteString(`</body></html>`)
	return b.String()
}

func renderText(appName, siteURL string, articles []models.ArticleView) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s digest — %d article%s waiting\n\n", appName, len(articles), plural(len(articles)))
	for _, a := range articles {
		b.WriteString("• ")
		b.WriteString(a.Title)
		b.WriteString("\n")
		if a.URL != "" {
			fmt.Fprintf(&b, "  %s\n", a.URL)
		}
		if summary := summaryParagraph(a.Summary); summary != "" {
			fmt.Fprintf(&b, "  %s\n", summary)
		}
		b.WriteString("\n")
	}
	if siteURL != "" {
		fmt.Fprintf(&b, "View these on the web: %s\n", siteURL)
	}
	return b.String()
}

// summaryParagraph extracts the lead paragraph from the stored summary text
// (paragraph + bullets format). Returns empty for older bullet-only entries.
func summaryParagraph(s string) string {
	if s == "" {
		return ""
	}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "• ") || strings.HasPrefix(line, "- ") {
			return ""
		}
		return line
	}
	return ""
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
