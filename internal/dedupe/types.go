package dedupe

import (
	"crypto/sha256"
	"errors"
	"time"
)

const (
	EvaluatedSimHashDistance     = 17
	EvaluatedEmbeddingSimilarity = 0.86
	DefaultClusterMaxAge         = 30 * 24 * time.Hour
)

type Config struct {
	SimHashDistance     int
	EmbeddingSimilarity float64
	ClusterMaxAge       time.Duration
}

func DefaultConfig() Config {
	return Config{
		SimHashDistance:     EvaluatedSimHashDistance,
		EmbeddingSimilarity: EvaluatedEmbeddingSimilarity,
		ClusterMaxAge:       DefaultClusterMaxAge,
	}
}

func (config Config) Validate() error {
	if config.SimHashDistance < 0 || config.SimHashDistance > 64 {
		return errors.New("SimHash distance must be between 0 and 64")
	}
	if config.EmbeddingSimilarity <= 0 || config.EmbeddingSimilarity > 1 {
		return errors.New("embedding similarity must be greater than 0 and at most 1")
	}
	if config.ClusterMaxAge <= 0 || config.ClusterMaxAge > 365*24*time.Hour {
		return errors.New("cluster maximum age must be between 1ns and 365 days")
	}
	return nil
}

type Document struct {
	RevisionID       string
	SourceID         string
	SourceTier       string
	CanonicalURL     string
	Title            string
	Author           string
	PackageName      string
	Version          string
	RawSHA256        [sha256.Size]byte
	NormalizedSHA256 [sha256.Size]byte
	SimHash          uint64
	PublishedAt      time.Time
	FirstSeenAt      time.Time
}

type Candidate struct {
	ItemID              string
	ClusterID           string
	RevisionID          string
	SourceID            string
	SourceTier          string
	CanonicalURL        string
	NormalizedTitle     string
	NormalizedAuthor    string
	PackageName         string
	Version             string
	RawSHA256           [sha256.Size]byte
	NormalizedSHA256    [sha256.Size]byte
	SimHash             uint64
	PublishedAt         time.Time
	FirstSeenAt         time.Time
	ObservedAt          time.Time
	EmbeddingSimilarity float64
}

type Outcome string

const (
	OutcomeCreate    Outcome = "created"
	OutcomeDuplicate Outcome = "duplicate"
	OutcomeRevision  Outcome = "revision"
	OutcomeCluster   Outcome = "clustered"
)

type Method string

const (
	MethodNew              Method = "new"
	MethodRevision         Method = "revision"
	MethodCanonicalURL     Method = "canonical_url"
	MethodRawSHA256        Method = "raw_sha256"
	MethodNormalizedSHA256 Method = "normalized_sha256"
	MethodMetadata         Method = "metadata"
	MethodSimHash          Method = "simhash"
	MethodEmbedding        Method = "embedding"
	MethodPackageVersion   Method = "package_version"
)

type Decision struct {
	Outcome             Outcome
	Method              Method
	CandidateItemID     string
	CandidateRevisionID string
	ClusterID           string
	SimHashDistance     int
	Similarity          float64
}

type ProcessRequest struct {
	RevisionID           string
	NormalizedText       string
	DeclaredCanonicalURL string
	PackageName          string
	Version              string
	AliasPairs           []HostAlias
	EmbeddingModelID     string
	Embedding            []float32
}

type ProcessResult struct {
	ItemID     string
	ClusterID  string
	Outcome    Outcome
	Method     Method
	Idempotent bool
	Similarity float64
}
