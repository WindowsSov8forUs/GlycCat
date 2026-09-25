package processor

import (
	"context"
	"testing"
)

func TestNormalizeLegacyQuotesPreservesOtherContent(t *testing.T) {
	input := `<quote data-x='1'><message id="old"/></quote><text>keep &amp; this</text>`
	want := `<quote data-x='1' id="old"><message id="old"/></quote><text>keep &amp; this</text>`
	got, err := normalizeLegacyQuotes(context.Background(), input)
	if err != nil || got != want {
		t.Fatalf("normalizeLegacyQuotes() = %q, %v", got, err)
	}
	explicit := `<quote id="new"><message id="old"/></quote>`
	got, err = normalizeLegacyQuotes(context.Background(), explicit)
	if err != nil || got != explicit {
		t.Fatalf("explicit quote changed to %q: %v", got, err)
	}
}
