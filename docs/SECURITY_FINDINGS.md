# Ember Security Findings Tracker

**Audit date:** 2026-06-06  
**Frameworks:** MITRE ATT&CK v14 · NIST CSF 2.0 · MITRE ATLAS · MITRE D3FEND · NIST AI RMF  
**Status key:** `open` · `in_progress` · `fixed` · `wont_fix` · `accepted_risk`

---

## CRITICAL (2)

### C-1 — DNS-Rebinding TOCTOU: Discovery Client Missing `GuardedTransport`
**Status:** `fixed`  
**File:** `internal/api/feed_handlers.go:63–68, 124–128`  
**ATT&CK:** T1090.003 | **NIST CSF:** PR.DS-5, ID.RA-1 | **D3FEND:** D3-NTA, D3-OAM

Both `handleAddFeed` and `handleDiscoverFeeds` build an `http.Client` with only `CheckRedirect` set, using `http.DefaultTransport`. `urlcheck.Check` resolves the hostname at pre-flight, but the standard transport re-resolves independently at dial time. An attacker who controls DNS can return a public IP to the pre-flight and `169.254.169.254` (or any private address) to the actual dial — bypassing the SSRF guard entirely.

The poller path at `poller.go:528` correctly uses `urlcheck.GuardedTransport`. The fix is identical:

```go
disco := &http.Client{
    Timeout:       10 * time.Second,
    Transport:     urlcheck.GuardedTransport(d.AllowPrivateURLs),
    CheckRedirect: feed.RedirectGuard(...),
}
```

---

### C-2 — Content-Type Confusion: `text/xml`/`application/xml` Accepted as Feed → Stored XSS Surface
**Status:** `fixed`  
**File:** `internal/feed/discover.go:244–251`  
**ATT&CK:** T1059.007, T1190 | **NIST CSF:** PR.DS-2, PR.IP-1 | **D3FEND:** D3-SAOR, D3-SAN

`isFeedContentType` accepts `application/xml` and `text/xml`, which also match SVG, XHTML, SOAP, and arbitrary XML APIs. A malicious site serves crafted XML under `text/xml` that `gofeed` parses with attacker-controlled `Title`, `SiteURL`, `Author`, and `Tags` fields — none of which are sanitized before storage (only `ContentHTML` goes through bluemonday).

Fix: Remove `application/xml` and `text/xml` from `isFeedContentType`; keep only `application/rss+xml`, `application/atom+xml`, and `application/feed+json`.

---

## HIGH (13)

### H-1 — Prompt Injection via Feed Content into Ollama LLM
**Status:** `open`  
**File:** `internal/summarize/ollama.go:242–245`  
**ATT&CK:** T1059.007 | **ATLAS:** AML.T0051, AML.T0048 | **NIST CSF:** GV.SC-07, PR.DS-02, DE.CM-09 | **D3FEND:** D3-IBAR, D3-OAM | **AI RMF:** GOVERN 1.2, MAP 1.5, MEASURE 2.5

Article `title` and `text` are inserted directly into `promptTemplate` via `fmt.Sprintf` with no structural separation. An attacker who controls a feed can craft content with embedded LLM instructions. Because the CLEANED section output is stored and rendered by the SPA, a successful injection can escalate to stored XSS even though bluemonday runs afterward — the attacker controls the model's output, not the sanitization input.

Fix:
1. Inject article content via an XML wrapper the prompt designates as inviolable (e.g. `<article>…</article>`).
2. Before paragraphizing model output: if `res.Cleaned` contains `<`, apply `html.EscapeString` to the entire string — treat model output as plaintext unconditionally.

---

### H-2 — TT-RSS Live-Pull API Client Missing `GuardedTransport` + Credential Exfil
**Status:** `fixed`  
**File:** `internal/ttrss/api.go:100–111`  
**ATT&CK:** T1190, T1078 | **NIST CSF:** PR.DS-5, PR.AC-3 | **D3FEND:** D3-NTA, D3-OAM

`apiClient()` uses `http.DefaultTransport` in production — same DNS-rebinding TOCTOU as C-1. Critically, the TT-RSS import sends a plaintext `password` field to the resolved address, making this a credential-exfiltration vector in addition to SSRF.

Fix: `Transport: urlcheck.GuardedTransport(false)` (or thread `allowPrivate` through `Service`) in `apiClient`.

---

### H-3 — SSRF via Aggregator Feed Body External Links
**Status:** `open`  
**File:** `internal/poller/poller.go:501–519`  
**ATT&CK:** T1090.001, T1210 | **NIST CSF:** PR.PS-05, PR.IR-01 | **D3FEND:** D3-UA, D3-NTF

`enrichWithReadability` extracts URLs from raw feed HTML using a regex and fetches them for readability processing. Feed publishers control these URLs. When `AllowPrivateURLs=true` (common homelab config) the `urlcheck` guard and `GuardedTransport` are disabled — an attacker-controlled aggregator feed causes Ember to fetch any LAN address.

Fix: Replace `hrefRE` with `golang.org/x/net/html` tokenizer for href extraction; add WARN-level logging when aggregator fallback triggers.

---

### H-4 — TT-RSS SSRF Guard Nil-Default Is Unsafe
**Status:** `open`  
**File:** `internal/ttrss/ttrss.go:52–55`, `internal/ttrss/api.go:97–110`  
**ATT&CK:** T1090.001, T1602 | **NIST CSF:** PR.PS-05, ID.RA-01 | **D3FEND:** D3-NTF, D3-UA

`Service.ValidateURL` is nil by default, which disables the redirect guard entirely. The production wiring sets it, but the type's zero-value is unsafe-by-default. A future caller constructing `&ttrss.Service{Store: s}` without setting `ValidateURL` would have no SSRF guard.

Fix: Invert the default — make SSRF blocking unconditional; add an explicit `AllowAllURLs bool` opt-in instead of nil-means-skip.

---

### H-5 — TT-RSS `h.Link` Not Sanitized via `SafeHTTPURL` → `javascript:` URL Stored
**Status:** `fixed`  
**File:** `internal/ttrss/api.go:126–133`  
**ATT&CK:** T1059.007 | **NIST CSF:** PR.DS-2, PR.IP-1 | **D3FEND:** D3-SAN

The live TT-RSS API pull path stores `h.Link` verbatim without calling `feed.SafeHTTPURL`. A TT-RSS server returning a `javascript:` or `data:` scheme in the link field stores it as an `<a href>` rendered by the SPA.

Fix: `link: feed.SafeHTTPURL(h.Link)` in the `normItem` construction.

---

### H-6 — `urlcheck` Error Strings Returned to Clients — Internal IP Oracle
**Status:** `open`  
**File:** `internal/api/feed_handlers.go:54, 74, 119, 135`  
**ATT&CK:** T1590, T1046 | **NIST CSF:** PR.DS-5, DE.CM-1 | **D3FEND:** D3-EHB

`err.Error()` from `urlcheck.Check` (which includes resolved IPs like `"urlcheck: URL resolves to private address: 10.0.0.5"`) is forwarded verbatim to the API response. Any authenticated user can enumerate private IP space by observing rejection messages.

Fix: Return `"URL is not allowed"` to the client; log the full error server-side.

---

### H-7 — Ollama BaseURL Not SSRF-Validated
**Status:** `open`  
**File:** `internal/config/config.go:117–119`, `internal/summarize/ollama.go:42–51`  
**ATT&CK:** T1090.002, T1602 | **NIST CSF:** PR.AA-05, ID.RA-01 | **D3FEND:** D3-NTF, D3-UA

`EMBER_OLLAMA_URL` is stored verbatim. An admin or compromised config can point it at `http://169.254.169.254/` or `http://127.0.0.1:2375/` (Docker API). All pull/generate/delete/list operations hit that address.

Fix: At config load time, parse the URL and validate the scheme is `http`/`https`. For non-homelab deployments, apply the same private-IP block as `urlcheck` unless `AllowPrivateURLs` is set.

---

### H-8 — Model Pull Endpoint: 30-Minute Blocking Call, No Concurrency Guard
**Status:** `open`  
**File:** `internal/api/llm_handlers.go:176–213`  
**ATT&CK:** T1499.001 | **NIST CSF:** PR.IR-04, DE.CM-01 | **D3FEND:** D3-RTSD

Each admin-triggered `handlePullLLMModel` blocks for up to 30 minutes. No mutex prevents concurrent pulls. The handler extends the connection deadline to 35 minutes, bypassing the global 60-second middleware timeout.

Fix: Atomic in-progress flag; return HTTP 409 if a pull is already running; expose status in `handleGetLLM`.

---

### H-9 — Fever Authentication Uses MD5-Derived Token
**Status:** `accepted_risk`  
**File:** `internal/api/fever.go:17`  
**ATT&CK:** T1110.002, T1552.001 | **NIST CSF:** PR.AA-03, PR.DS-02 | **D3FEND:** D3-SPP, D3-MFA

The Fever protocol mandates `md5(username:password)` as the API key. MD5 is broken — a compromised DB token is trivially reversible offline. This cannot be fixed without breaking protocol compatibility.

Mitigations: (1) Mandate TLS at the edge when Fever is enabled. (2) Add a per-user Fever enable/disable toggle. (3) Document as accepted protocol-level risk in `docs/security.md`.

---

### H-10 — WebAuthn `TakeWebAuthnSession` TOCTOU Race
**Status:** `open`  
**File:** `internal/store/passkeys.go:171`  
**ATT&CK:** T1550 | **NIST CSF:** PR.AA-05 | **D3FEND:** D3-OTP

SELECT then DELETE is not atomic. Two concurrent requests with the same session ID can both read the row before either DELETE fires, enabling ceremony replay. `DELETE … RETURNING` (SQLite 3.35+, supported by `modernc.org/sqlite` v1.50+) makes this atomic.

---

### H-11 — `FinishRegister` Resolves Target User from Session, Not Auth Context
**Status:** `open`  
**File:** `internal/auth/webauthn.go:141–188`, `internal/api/passkey_handlers.go:93–121`  
**ATT&CK:** T1556, T1550 | **NIST CSF:** PR.AA-05 | **D3FEND:** D3-UAM

`handlePasskeyRegisterFinish` is behind `RequireAuth` but never verifies the authenticated caller's ID matches `sess.UserID`. Session ID entropy (128-bit) makes guessing infeasible, but the binding check is architecturally absent.

Fix: Pass `callerID` from the auth context into `FinishRegister` and assert `sess.UserID.Int64 == callerID`.

---

### H-12 — Summary Backfill on Restart Can Saturate LLM
**Status:** `open`  
**File:** `internal/poller/poller.go:596–615`  
**ATT&CK:** T1499.002 | **NIST CSF:** PR.IR-04, DE.CM-01 | **AI RMF:** GOVERN 1.2, MAP 1.5

`enqueuePendingSummaries` at startup enqueues up to 256 articles. With a 90-second Ollama timeout each, this is potentially 6+ hours of sustained load against a shared Ollama instance with no rate limiting or backpressure.

Fix: Token-bucket rate limiter in the summary worker loop; log the backfill count at startup.

---

### H-13 — LLM-Cleaned HTML Stored Without Structural Output Validation
**Status:** `open`  
**File:** `internal/poller/poller.go:653–661`  
**ATLAS:** AML.T0048, AML.T0056 | **AI RMF:** MEASURE 2.5, MANAGE 2.4 | **D3FEND:** D3-OAM, D3-IBAR

`paragraphizePlain` applies `htmlEscape` to individual lines but not to paragraph-split boundaries. A prompt-injected model output containing `</p><script>…</script><p>` across a chunk boundary can survive `htmlEscape` and reach bluemonday as the sole XSS defense.

Fix: If `res.Cleaned` contains `<`, apply `html.EscapeString` to the entire string before paragraphizing — treat model output as plaintext unconditionally.

---

## MEDIUM (16)

| ID | Status | File | Issue | ATT&CK |
|----|--------|------|-------|--------|
| M-1 | `open` | `internal/api/render.go:96` | JSON decode errors returned verbatim — Go struct field names leaked to unauthenticated callers. Fix: return `"invalid request body"`, log detail server-side. | T1592 |
| M-2 | `open` | `internal/api/auth_handlers.go:159` | `settings_json` stored without `json.Valid()` check or size cap (max 64 KiB recommended). | T1565.001 |
| M-3 | `open` | `internal/api/user_handlers.go:102` | Admin `PATCH /api/users/{id}` bypasses the 8-char password minimum enforced on create/change-password paths. | T1098 |
| M-4 | `open` | `internal/api/user_handlers.go:111` | Admin can self-demote (set own `is_admin=false`) with no guard — causes permanent lockout if sole admin. | T1078.003 |
| M-5 | `open` | `internal/api/middleware.go:255` | CSRF: passkey login relies on implicit bypass (no session cookie) rather than explicit path allowlist. `ember_session` hardcoded instead of `auth.CookieName`. | T1606 |
| M-6 | `open` | `internal/api/middleware.go:44–53` | CSP: no explicit `script-src` directive; `data:` in `img-src` (tracking-pixel exfil); `'unsafe-inline'` in `style-src` (CSS injection). | T1059.007 |
| M-7 | `open` | `internal/api/share_handlers.go:44` | Unbounded `limit` parameter on inbox endpoint — no upper cap. Add `const maxInboxLimit = 200`. | T1499.002 |
| M-8 | `open` | `internal/store/dbops.go:38` | `VACUUM INTO` uses `fmt.Sprintf` with single-quote escape — safe today since `dir` is a constant, fragile if `dir` is ever exposed to user input. Add an assertion that `dir` is server-controlled. | T1190 |
| M-9 | `open` | `internal/feed/sanitize.go:17` | bluemonday UGC policy allows `target="_blank"` without requiring `rel="noopener noreferrer"` — enables tab-napping. | T1185 |
| M-10 | `open` | `internal/ttrss/api.go:173` | TT-RSS credentials transmitted without TLS enforcement — explicit `http://` URLs accepted without error. | T1040, T1557 |
| M-11 | `open` | `internal/digest/digest.go:294` | MIME boundary is timestamp-derived (`time.Now().UnixNano()`), not cryptographically random. Fix: `"ember-" + hex.EncodeToString(rand16bytes)`. | — |
| M-12 | `open` | `internal/digest/digest.go:283` | `isLoopbackHost` checks literal string/IP only — does not resolve hostnames. A Docker service name resolving to `127.x` is not caught. | — |
| M-13 | `open` | `internal/poller/poller.go:409` | `hrefRE` regex parses raw HTML for href extraction instead of an HTML parser — bypassable with single-quoted attrs, comments, userinfo-in-host trick. | T1190 |
| M-14 | `open` | `internal/summarize/noop.go` | Noop summarizer has no guard against accidental production use — add a startup log line identifying the active Summarizer implementation. | — (AI RMF: GOVERN 1.7) |
| M-15 | `open` | `internal/ttrss/api.go:100` | Redirect guard inherits import request context — cancellation produces misleading SSRF rejection logs. Use a separate background context with per-redirect timeout. | — |
| M-16 | `open` | `internal/api/fever.go:85–113` | Fever items endpoint: `since_id`/`max_id` pagination cursors not honored; store errors silently swallowed with `_, _`. | T1530 |

---

## LOW (8)

| ID | Status | File | Issue |
|----|--------|------|-------|
| L-1 | `open` | `internal/api/middleware.go:30` | HSTS header omits `preload` — first-visit MITM window for public deployments. Make configurable via `EMBER_HSTS_PRELOAD=true`. |
| L-2 | `open` | `internal/api/middleware.go:255` | CSRF exemption for passkey login is implicit (no-session bypass) rather than explicit path allowlist — fragile if a pre-auth cookie is added later. |
| L-3 | `open` | `internal/api/fever.go:140` | `feverFindUser` does full-table scan loading all `password_hash` rows on every Fever request. Add `UNIQUE INDEX ON users(fever_token)` and query directly. |
| L-4 | `open` | `internal/store/admin_settings.go:117` | SMTP password stored plaintext in SQLite — readable if DB file is compromised. Acceptable for homelab; verify `ember.db` is `0600`; document in `docs/security.md`. |
| L-5 | `open` | `internal/api/health.go:13` | `/readyz` returns HTTP 503 to unauthenticated probes when DB is down — binary oracle for availability state. |
| L-6 | `open` | `cmd/ember/db_maintenance.go:93` | `os.Create` for OPML exports inherits process umask — may create world-readable files. Fix: `os.OpenFile(out, os.O_CREATE\|os.O_WRONLY\|os.O_TRUNC, 0o600)`. |
| L-7 | `open` | `internal/feed/discover.go:38, 120` | `http.DefaultClient` fallback in `Discover`/`DiscoverAll` when `c == nil` — no timeout, no guarded transport. Return an error instead of silently using the default client. |
| L-8 | `open` | `internal/feed/discover.go:318–321` | `<?xml` body sniff matches any XML document. Tighten to `<rss`, `<feed`, `<rdf:rdf` only. |

---

## Fix Priority Order

```
IMMEDIATE — fix before next release:
  C-1   GuardedTransport on feed discovery HTTP client
  C-2   Remove text/xml and application/xml from isFeedContentType
  H-2   GuardedTransport on TT-RSS apiClient
  H-5   SafeHTTPURL on h.Link in TT-RSS live pull

HIGH — next sprint:
  H-1   Structural separation in Ollama prompt (prompt injection)
  H-13  Treat LLM CLEANED output as plaintext before paragraphizing
  H-3   Replace hrefRE with html.Parse in enrichWithReadability
  H-4   Invert TT-RSS SSRF guard to fail-safe default
  H-6   Redact urlcheck error strings from API responses
  H-7   Validate EMBER_OLLAMA_URL scheme at config load
  H-8   Concurrency guard on model pull endpoint (HTTP 409)
  H-10  Atomic TakeWebAuthnSession via DELETE...RETURNING
  H-11  Bind FinishRegister caller ID from auth context
  H-12  Rate-limit summary backfill worker

MEDIUM — hardening sprint:
  M-1, M-2, M-3, M-4, M-5   Input validation and admin path gaps
  M-6                         CSP tightening (explicit script-src, remove data: from img-src)
  M-7                         Inbox limit cap
  M-9                         bluemonday: require noopener on target=_blank
  M-10                        Enforce HTTPS for TT-RSS credential transmission
  M-16                        Fever pagination and error handling
  M-11, M-12, M-13, M-14, M-15   Remaining medium items

LOW — opportunistic:
  L-1 through L-8
```

---

## Positive Security Baseline (no action needed)

- **argon2id** — correct params (64 MiB / 3 iter / 2 parallel), `crypto/rand` salt, constant-time verify, timing equalization on missing user
- **Session management** — 256-bit random IDs, HMAC-signed gorilla cookies, `HttpOnly` + `SameSiteStrict`, server-side expiry, session rotation on login
- **CSRF** — double-submit cookie with constant-time compare; Fever shim correctly excluded
- **Parameterized SQL everywhere** — zero string concatenation in query paths; FTS5 injection handled correctly
- **SSRF on primary poller path** — `GuardedTransport` with DNS pinning, redirect guard, scheme allowlist
- **HTML sanitization** — bluemonday on all ingested content; `SafeHTTPURL` on all feed-supplied links in the main parse path
- **SMTP hardening** — CRLF-stripping on all headers, StartTLS enforced for non-loopback relays, TLS 1.2 minimum
- **Admin route protection** — all `/api/admin/*` and `/metrics` behind `RequireAdmin`
- **IDOR prevention** — every store method scopes to `user_id`; no object reference leakage found
- **Password hash never serialized** — `PasswordHash` and `FeverToken` both tagged `json:"-"`
