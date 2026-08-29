package dedupe

import "testing"

func TestSimHashIsDeterministicAndNearDuplicateSensitive(t *testing.T) {
	t.Parallel()

	base := "Go 1.27 adds deterministic scheduler improvements and fixes a runtime race in production."
	near := "Go 1.27 adds deterministic scheduler improvements and fixes one runtime race in production."
	unrelated := "React introduces a compiler-driven rendering model for interactive browser interfaces."
	baseHash := SimHash(base)
	if baseHash == 0 || baseHash != SimHash(base) {
		t.Fatalf("SimHash() is empty or unstable: %d", baseHash)
	}
	if nearDistance := SimHashDistance(baseHash, SimHash(near)); nearDistance >= SimHashDistance(baseHash, SimHash(unrelated)) {
		t.Fatalf("near distance %d is not below unrelated distance", nearDistance)
	}
}

func TestMetadataNormalizationAndReleaseExtraction(t *testing.T) {
	t.Parallel()

	if normalized := NormalizeMetadata("  React—Compiler: V1.0  "); normalized != "react compiler v1 0" {
		t.Fatalf("NormalizeMetadata() = %q", normalized)
	}
	packageName, version := ExtractReleaseMetadata("Go 1.27 is released")
	if packageName != "go" || version != "1.27" {
		t.Fatalf("ExtractReleaseMetadata() = %q, %q", packageName, version)
	}
	packageName, version = ExtractReleaseMetadata("An announcement without a version")
	if packageName != "" || version != "" {
		t.Fatalf("ExtractReleaseMetadata() without version = %q, %q", packageName, version)
	}
}

func TestConfigValidation(t *testing.T) {
	t.Parallel()

	if err := DefaultConfig().Validate(); err != nil {
		t.Fatalf("DefaultConfig().Validate() error = %v", err)
	}
	for _, config := range []Config{
		{SimHashDistance: -1, EmbeddingSimilarity: EvaluatedEmbeddingSimilarity, ClusterMaxAge: DefaultClusterMaxAge},
		{SimHashDistance: 65, EmbeddingSimilarity: EvaluatedEmbeddingSimilarity, ClusterMaxAge: DefaultClusterMaxAge},
		{SimHashDistance: 3, EmbeddingSimilarity: EvaluatedEmbeddingSimilarity},
		{SimHashDistance: 3, EmbeddingSimilarity: EvaluatedEmbeddingSimilarity, ClusterMaxAge: 366 * 24 * 60 * 60 * 1e9},
		{SimHashDistance: 3, EmbeddingSimilarity: 0, ClusterMaxAge: DefaultClusterMaxAge},
		{SimHashDistance: 3, EmbeddingSimilarity: 1.1, ClusterMaxAge: DefaultClusterMaxAge},
	} {
		if err := config.Validate(); err == nil {
			t.Errorf("Config.Validate() accepted %+v", config)
		}
	}
}
