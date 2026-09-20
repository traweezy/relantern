package migrations

import (
	"embed"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"
)

// Embedding the reviewed SQL files keeps the runtime's required versions tied
// to the same migration inputs used by the one-shot migrate service.
//
//go:embed *.sql
var sqlFiles embed.FS

// RequiredVersions returns every reviewed Goose version in this build.
func RequiredVersions() ([]int64, error) {
	entries, err := fs.ReadDir(sqlFiles, ".")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return parseVersions(names)
}

func parseVersions(names []string) ([]int64, error) {
	versions := make([]int64, 0, len(names))
	seen := make(map[int64]bool, len(names))
	for _, name := range names {
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		prefix, suffix, ok := strings.Cut(name, "_")
		if !ok || suffix == ".sql" || !allDigits(prefix) {
			return nil, fmt.Errorf("invalid migration filename %q", name)
		}
		version, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("invalid migration version in %q", name)
		}
		if seen[version] {
			return nil, fmt.Errorf("duplicate migration version %d", version)
		}
		seen[version] = true
		versions = append(versions, version)
	}
	if len(versions) == 0 {
		return nil, fmt.Errorf("no SQL migrations embedded")
	}
	// fs.ReadDir returns sorted names. Sorting here also makes the parser safe
	// for callers that supply unordered test or generated inputs.
	slices.Sort(versions)
	return versions, nil
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
