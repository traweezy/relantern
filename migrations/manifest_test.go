package migrations

import (
	"reflect"
	"testing"
)

func TestRequiredVersionsComeFromEmbeddedSQL(t *testing.T) {
	versions, err := RequiredVersions()
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) < 35 || versions[0] != 1 || versions[len(versions)-1] < 35 {
		t.Fatalf("unexpected reviewed migration range: %v", versions)
	}
}

func TestParseVersionsRejectsDuplicateAndMalformedSQL(t *testing.T) {
	for _, names := range [][]string{
		{"000001_first.sql", "1_second.sql"},
		{"bad_name.sql"},
		{"000000_zero.sql"},
		{"000001_.sql"},
		{"notes.go"},
	} {
		if _, err := parseVersions(names); err == nil {
			t.Fatalf("parseVersions(%v) succeeded", names)
		}
	}
	got, err := parseVersions([]string{"000035_last.sql", "manifest.go", "000001_first.sql"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{1, 35}; !reflect.DeepEqual(got, want) {
		t.Fatalf("versions = %v, want %v", got, want)
	}
}
