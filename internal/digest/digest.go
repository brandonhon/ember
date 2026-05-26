// Package digest builds + sends the daily digest email. Pulled out of the
// poller so the SMTP code stays self-contained and testable.
package digest

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"html"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/brandonhon/ember/internal/models"
	"github.com/brandonhon/ember/internal/store"
)

// SMTPConfig is what the sender needs to talk to an upstream SMTP relay.
type SMTPConfig struct {
	Host      string
	Port      int
	Username  string
	Password  string
	From      string
	StartTLS  bool
}

// Configured reports whether SMTP has the minimum settings to send. Lets the
// runner skip the work entirely on hosts that don't configure mail.
func (c SMTPConfig) Configured() bool {
	return c.Host != "" && c.Port != 0 && c.From != ""
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

	articles, err := s.fetchArticles(ctx, d)
	if err != nil {
		return 0, fmt.Errorf("digest: fetch articles: %w", err)
	}
	if len(articles) == 0 {
		return 0, nil
	}
	appName := s.AppName
	if appName == "" {
		appName = "Ember"
	}
	subject := fmt.Sprintf("%s digest — %d new article%s",
		appName, len(articles), plural(len(articles)))

	htmlBody := renderHTML(appName, s.SiteURL, articles)
	textBody := renderText(appName, s.SiteURL, articles)
	msg := buildMIME(s.SMTP.From, to, subject, textBody, htmlBody)

	if err := s.send(to, msg); err != nil {
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
	case "feed":
		id, _ := strconv.ParseInt(d.ViewValue, 10, 64)
		q.FeedID = id
	case "category":
		id, _ := strconv.ParseInt(d.ViewValue, 10, 64)
		q.CategoryID = id
	case "board":
		id, _ := strconv.ParseInt(d.ViewValue, 10, 64)
		q.BoardID = id
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

func (s *Sender) send(to string, msg []byte) error {
	addr := fmt.Sprintf("%s:%d", s.SMTP.Host, s.SMTP.Port)
	if !s.SMTP.StartTLS {
		// Plain SMTP — only sensible on the same host or inside a VPN.
		auth := s.smtpAuth()
		return smtp.SendMail(addr, auth, s.SMTP.From, []string{to}, msg)
	}
	c, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Hello("ember"); err != nil {
		return err
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: s.SMTP.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if auth := s.smtpAuth(); auth != nil {
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if err := c.Mail(s.SMTP.From); err != nil {
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

func buildMIME(from, to, subject, textBody, htmlBody string) []byte {
	var b bytes.Buffer
	boundary := fmt.Sprintf("ember-%d", time.Now().UnixNano())
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
	return b.Bytes()
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
