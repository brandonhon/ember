package emailinbox

import "testing"

func TestGenerateHandle(t *testing.T) {
	h, err := GenerateHandle()
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != handleLen {
		t.Errorf("len(h) = %d, want %d", len(h), handleLen)
	}
	if !ValidHandle(h) {
		t.Errorf("generated handle %q rejected by ValidHandle", h)
	}
	// Determinism check: two consecutive generations should differ.
	h2, _ := GenerateHandle()
	if h == h2 {
		t.Errorf("two generations returned same handle %q", h)
	}
}

func TestValidHandle(t *testing.T) {
	cases := map[string]bool{
		"":               false,
		"ABCDEF":         false, // too short
		"ABCDEFGHIJKLMN": false, // too long
		"01234ABCDEFG":   true,  // 12 valid chars
		"01234abcdefg":   false, // lowercase not in alphabet
		"ZZZZZZZZZZZ!":   false, // bad char at end
		"IOULZZZZZZZZ":   false, // I/L/O/U excluded
	}
	for in, want := range cases {
		if got := ValidHandle(in); got != want {
			t.Errorf("ValidHandle(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestExtractHandle(t *testing.T) {
	cases := []struct {
		addr, domain string
		wantHandle   string
		wantOK       bool
	}{
		{"01234ABCDEFG@mail.example.com", "mail.example.com", "01234ABCDEFG", true},
		{"01234ABCDEFG@MAIL.example.com", "mail.example.com", "01234ABCDEFG", true},
		{"01234ABCDEFG@other.example.com", "mail.example.com", "", false},
		{"badhandle@mail.example.com", "mail.example.com", "", false},
		{"no-at-sign", "mail.example.com", "", false},
		{"@mail.example.com", "mail.example.com", "", false},
	}
	for _, tc := range cases {
		gotH, gotOK := extractHandle(tc.addr, tc.domain)
		if gotOK != tc.wantOK {
			t.Errorf("extractHandle(%q, %q) ok = %v, want %v", tc.addr, tc.domain, gotOK, tc.wantOK)
		}
		if gotH != tc.wantHandle {
			t.Errorf("extractHandle(%q, %q) handle = %q, want %q", tc.addr, tc.domain, gotH, tc.wantHandle)
		}
	}
}
