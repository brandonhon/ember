package filters

import "testing"

// FuzzParseMatch feeds arbitrary strings to the user-supplied match parser and
// then validates the result. Validate compiles a user regex, so the pair must
// be panic-free for any input.
func FuzzParseMatch(f *testing.F) {
	f.Add(`{"field":"title","op":"contains","value":"foo"}`)
	f.Add(`{"field":"published_at","op":"newer_than","value":"7d"}`)
	f.Add(`{"field":"title","op":"matches","value":"(a|b)+"}`)
	f.Add(`{"field":"has_image","op":"equals","value":"true"}`)
	f.Add(`not json`)
	f.Add(``)
	f.Fuzz(func(t *testing.T, s string) {
		m, err := ParseMatch(s)
		if err == nil {
			_ = m.Validate()
		}
	})
}
