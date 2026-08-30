package pgstore

import (
	"testing"

	"github.com/traweezy/relantern/internal/discovery"
)

func TestValidSearchFiltersChecksDatesAndRanges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		filters discovery.SearchFilters
		valid   bool
	}{
		{name: "empty", filters: discovery.SearchFilters{}, valid: true},
		{name: "date only", filters: discovery.SearchFilters{After: "2026-08-01", Before: "2026-08-29"}, valid: true},
		{name: "RFC 3339", filters: discovery.SearchFilters{After: "2026-08-01T12:00:00Z"}, valid: true},
		{name: "invalid calendar date", filters: discovery.SearchFilters{After: "2026-02-30"}, valid: false},
		{name: "reversed range", filters: discovery.SearchFilters{After: "2026-08-30", Before: "2026-08-29"}, valid: false},
		{name: "newline", filters: discovery.SearchFilters{Topic: "database\nsecurity"}, valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if actual := validSearchFilters(test.filters); actual != test.valid {
				t.Fatalf("validSearchFilters(%+v) = %t, want %t", test.filters, actual, test.valid)
			}
		})
	}
}
