package pgstore

import "testing"

func TestNewRequiresPool(t *testing.T) {
	t.Parallel()
	if _, err := New(nil); err == nil {
		t.Fatal("New(nil) error = nil, want an error")
	}
}

func TestSourceDomain(t *testing.T) {
	t.Parallel()
	if actual := sourceDomain("https://www.example.com/releases/1"); actual != "example.com" {
		t.Fatalf("sourceDomain() = %q, want example.com", actual)
	}
	if actual := sourceDomain(":not-a-url"); actual != "source" {
		t.Fatalf("sourceDomain(invalid) = %q, want source", actual)
	}
}
