package emailinbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/emersion/go-smtp"
)

// Ingester is the store-facing surface the SMTP server needs. The real
// implementation in internal/store satisfies it.
type Ingester interface {
	// ResolveInbox returns the user_id + feed_id behind a handle, or
	// (0, 0, false) if no active inbox matches.
	ResolveInbox(ctx context.Context, handle string) (userID, feedID int64, ok bool, err error)
	// IngestEmail stores a parsed message as an article on the feed.
	IngestEmail(ctx context.Context, userID, feedID int64, raw []byte) error
}

// Config controls the SMTP listener. Domain is required; without it
// the listener doesn't start and the inbox feature is disabled.
type Config struct {
	Domain      string        // e.g. "mail.example.com"
	ListenAddr  string        // e.g. ":2525" — default if empty
	MaxBytes    int64         // per-message cap; default 25 MiB
	ReadTimeout time.Duration // default 30s
}

// Server is a thin wrapper around go-smtp configured to accept mail
// only for known inbox handles.
type Server struct {
	cfg    Config
	store  Ingester
	logger *slog.Logger
	smtp   *smtp.Server
}

// NewServer builds the listener. Call Start to run it; Stop to halt.
// Returns nil if cfg.Domain is empty (feature disabled).
func NewServer(cfg Config, store Ingester, logger *slog.Logger) *Server {
	if cfg.Domain == "" {
		return nil
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":2525"
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = 25 * 1024 * 1024
	}
	if cfg.ReadTimeout <= 0 {
		cfg.ReadTimeout = 30 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{cfg: cfg, store: store, logger: logger.With("component", "emailinbox")}
	be := &backend{s: s}
	srv := smtp.NewServer(be)
	srv.Addr = cfg.ListenAddr
	srv.Domain = cfg.Domain
	srv.MaxMessageBytes = cfg.MaxBytes
	srv.MaxRecipients = 5
	srv.ReadTimeout = cfg.ReadTimeout
	srv.WriteTimeout = cfg.ReadTimeout
	srv.AllowInsecureAuth = true // we don't require AUTH at all
	s.smtp = srv
	return s
}

// Start blocks listening for inbound connections until Stop is called.
// Logs and returns nil on graceful shutdown.
func (s *Server) Start() error {
	s.logger.Info("smtp listener starting", "addr", s.cfg.ListenAddr, "domain", s.cfg.Domain)
	if err := s.smtp.ListenAndServe(); err != nil && !errors.Is(err, smtp.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("emailinbox: serve: %w", err)
	}
	return nil
}

// Stop terminates the listener.
func (s *Server) Stop() { _ = s.smtp.Close() }

// backend implements smtp.Backend. Stateless per connection.
type backend struct{ s *Server }

func (b *backend) NewSession(_ *smtp.Conn) (smtp.Session, error) {
	return &session{s: b.s}, nil
}

// session holds per-connection state. go-smtp creates a new one per
// connection.
type session struct {
	s        *Server
	rcptUser int64
	rcptFeed int64
}

func (s *session) AuthPlain(string, string) error { return nil }

func (s *session) Mail(_ string, _ *smtp.MailOptions) error {
	// We accept any sender (the per-recipient address check below is the
	// real gate). Bouncing on MAIL FROM would reject legitimate forwarders.
	s.rcptUser = 0
	s.rcptFeed = 0
	return nil
}

func (s *session) Rcpt(to string, _ *smtp.RcptOptions) error {
	handle, ok := extractHandle(to, s.s.cfg.Domain)
	if !ok {
		return smtpError(550, "5.1.1", "no such mailbox")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	userID, feedID, found, err := s.s.store.ResolveInbox(ctx, handle)
	if err != nil {
		s.s.logger.Error("resolve inbox", "handle", handle, "err", err)
		return smtpError(451, "4.7.0", "temporary lookup failure")
	}
	if !found {
		return smtpError(550, "5.1.1", "no such mailbox")
	}
	s.rcptUser = userID
	s.rcptFeed = feedID
	return nil
}

func (s *session) Data(r io.Reader) error {
	if s.rcptUser == 0 || s.rcptFeed == 0 {
		return smtpError(503, "5.5.1", "RCPT required before DATA")
	}
	buf, err := io.ReadAll(io.LimitReader(r, s.s.cfg.MaxBytes+1))
	if err != nil {
		return smtpError(451, "4.7.0", "read body")
	}
	if int64(len(buf)) > s.s.cfg.MaxBytes {
		return smtpError(552, "5.3.4", "message too large")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.s.store.IngestEmail(ctx, s.rcptUser, s.rcptFeed, buf); err != nil {
		s.s.logger.Error("ingest email", "user_id", s.rcptUser, "feed_id", s.rcptFeed, "err", err)
		return smtpError(451, "4.7.0", "ingest failed")
	}
	return nil
}

func (s *session) Reset()        {}
func (s *session) Logout() error { return nil }

// extractHandle returns the handle from an envelope-To address that
// matches <handle>@<domain>. Case-insensitive on the domain, exact on
// the handle (handle alphabet is case-preserving).
func extractHandle(addr, domain string) (string, bool) {
	at := strings.LastIndexByte(addr, '@')
	if at < 1 {
		return "", false
	}
	if !strings.EqualFold(addr[at+1:], domain) {
		return "", false
	}
	h := addr[:at]
	if !ValidHandle(h) {
		return "", false
	}
	return h, true
}

func smtpError(code int, enh, msg string) *smtp.SMTPError {
	return &smtp.SMTPError{Code: code, EnhancedCode: parseEnhanced(enh), Message: msg}
}

func parseEnhanced(s string) smtp.EnhancedCode {
	// go-smtp's EnhancedCode is [3]int. Parse "x.y.z".
	var c smtp.EnhancedCode
	parts := strings.Split(s, ".")
	for i := 0; i < len(parts) && i < 3; i++ {
		// Best-effort; on malformed input the slot stays zero.
		_, _ = fmt.Sscanf(parts[i], "%d", &c[i])
	}
	return c
}
