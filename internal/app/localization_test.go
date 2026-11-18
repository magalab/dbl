package app

import "testing"

func TestMyGoLanguageTag(t *testing.T) {
	for language, want := range map[string]string{"zh": "zh-Hans", "en": "en"} {
		if got := mygoLanguageTag(language); got != want {
			t.Errorf("mygoLanguageTag(%q) = %q, want %q", language, got, want)
		}
	}
}
