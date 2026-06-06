package emailinbox

import (
	"strings"
	"testing"
)

const samplePlainText = `From: Substack Newsletter <noreply@substack.com>
To: 01234ABCDEFG@mail.example.com
Subject: Weekly digest #42
Date: Mon, 02 Jan 2026 10:00:00 +0000
Content-Type: text/plain; charset=UTF-8
Content-Transfer-Encoding: quoted-printable

Hi there,

This week=E2=80=99s top story is a long one.

Cheers!
`

func TestParseMessage_PlainText(t *testing.T) {
	art, err := ParseMessage([]byte(samplePlainText))
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if art.Title != "Weekly digest #42" {
		t.Errorf("title = %q", art.Title)
	}
	if art.Author != "Substack Newsletter" {
		t.Errorf("author = %q", art.Author)
	}
	if !strings.Contains(art.ContentText, "Hi there") {
		t.Errorf("missing body in ContentText: %q", art.ContentText)
	}
	if !strings.Contains(art.ContentText, "’") {
		t.Errorf("quoted-printable not decoded; got %q", art.ContentText)
	}
	if art.PublishedAt == 0 {
		t.Error("PublishedAt not parsed from Date header")
	}
}

const sampleMultipart = `From: Beehiiv <noreply@beehiiv.com>
To: 01234ABCDEFG@mail.example.com
Subject: Test
Date: Mon, 02 Jan 2026 10:00:00 +0000
MIME-Version: 1.0
Content-Type: multipart/alternative; boundary=BOUNDARY

--BOUNDARY
Content-Type: text/plain; charset=UTF-8

Plain version here.

--BOUNDARY
Content-Type: text/html; charset=UTF-8

<html><body><h1>HTML version</h1></body></html>

--BOUNDARY--
`

func TestParseMessage_Multipart(t *testing.T) {
	art, err := ParseMessage([]byte(sampleMultipart))
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if !strings.Contains(art.ContentHTML, "HTML version") {
		t.Errorf("expected HTML body, got %q", art.ContentHTML)
	}
	if !strings.Contains(art.ContentText, "Plain version") {
		t.Errorf("expected plain body, got %q", art.ContentText)
	}
}

const sampleNoSubject = `From: sender@example.com
To: 01234ABCDEFG@mail.example.com
Date: Mon, 02 Jan 2026 10:00:00 +0000
Content-Type: text/plain

Body only.
`

func TestParseMessage_SanitizesHTML(t *testing.T) {
	msg := "From: Evil <evil@attacker.test>\r\n" +
		"To: 01234ABCDEFG@mail.example.com\r\n" +
		"Subject: hi\r\n" +
		"Date: Mon, 02 Jan 2026 10:00:00 +0000\r\n" +
		"Content-Type: text/html; charset=UTF-8\r\n" +
		"\r\n" +
		`<p>hello</p><script>alert(document.cookie)</script>` +
		`<img src=x onerror="steal()"><a href="javascript:evil()">x</a>`
	art, err := ParseMessage([]byte(msg))
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	for _, bad := range []string{"<script", "alert(", "onerror", "javascript:"} {
		if strings.Contains(art.ContentHTML, bad) {
			t.Errorf("email HTML not sanitized, still contains %q: %q", bad, art.ContentHTML)
		}
	}
	if !strings.Contains(art.ContentHTML, "<p>hello</p>") {
		t.Errorf("benign content dropped: %q", art.ContentHTML)
	}
}

func TestParseMessage_Base64HTML(t *testing.T) {
	// "<p>hi <b>there</b></p>" base64-encoded, MIME line-wrapped.
	body := "PHA+aGkgPGI+dGhlcmU8L2I+PC9wPg=="
	msg := "From: News <n@s.test>\r\n" +
		"To: 01234ABCDEFG@mail.example.com\r\n" +
		"Subject: b64\r\n" +
		"Date: Mon, 02 Jan 2026 10:00:00 +0000\r\n" +
		"Content-Type: text/html; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" + body
	art, err := ParseMessage([]byte(msg))
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if !strings.Contains(art.ContentHTML, "<b>there</b>") {
		t.Errorf("base64 HTML not decoded: %q", art.ContentHTML)
	}
}

func TestParseMessage_NoSubject(t *testing.T) {
	art, err := ParseMessage([]byte(sampleNoSubject))
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if art.Title != "(no subject)" {
		t.Errorf("title = %q, want fallback", art.Title)
	}
}

func TestParseMessage_EncodedSubject(t *testing.T) {
	raw := "From: a@b\r\nSubject: =?utf-8?B?SGVsbG8sIFdvcmxkIQ==?=\r\nDate: Mon, 02 Jan 2026 10:00:00 +0000\r\nContent-Type: text/plain\r\n\r\nx"
	art, err := ParseMessage([]byte(raw))
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if art.Title != "Hello, World!" {
		t.Errorf("title = %q, want decoded", art.Title)
	}
}
