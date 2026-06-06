package emailinbox

import "testing"

// FuzzParseMessage hammers the inbound-email parser with arbitrary bytes.
// ParseMessage runs on mail delivered by untrusted external senders, so it
// must never panic, hang, or exhaust memory regardless of input (malformed
// MIME, deep multipart nesting, bad encodings, truncated headers).
func FuzzParseMessage(f *testing.F) {
	f.Add([]byte(samplePlainText))
	f.Add([]byte(sampleMultipart))
	f.Add([]byte("From: a@b.test\r\nSubject: x\r\n" +
		"Content-Type: multipart/mixed; boundary=B\r\n\r\n" +
		"--B\r\nContent-Type: text/html\r\nContent-Transfer-Encoding: base64\r\n\r\nPHA+aGk8L3A+\r\n--B--\r\n"))
	f.Add([]byte(""))
	f.Add([]byte("Content-Type: multipart/mixed; boundary=x\r\n\r\n--x\r\n--x--"))
	f.Fuzz(func(t *testing.T, raw []byte) {
		// Only contract: never panic. A returned error is fine.
		_, _ = ParseMessage(raw)
	})
}
