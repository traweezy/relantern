package radar

import "time"

type State string

const (
	StateAdopt  State = "adopt"
	StateTrial  State = "trial"
	StateAssess State = "assess"
	StateHold   State = "hold"
	StateReject State = "reject"
)

type EvidenceLink struct {
	Label      string `json:"label"`
	URL        string `json:"url"`
	SourceTier string `json:"sourceTier"`
}

type CandidateEvidence struct {
	ID                        string         `json:"id,omitempty"`
	Ecosystem                 string         `json:"ecosystem"`
	PackageName               string         `json:"packageName"`
	RepositoryURL             string         `json:"repositoryUrl"`
	DiscoverySource           string         `json:"discoverySource"`
	IncumbentPackage          string         `json:"incumbentPackage"`
	StableRelease             string         `json:"stableRelease"`
	License                   string         `json:"license"`
	ContributorCount          int            `json:"contributorCount"`
	ReleaseCadenceDays        *int           `json:"releaseCadenceDays,omitempty"`
	IssueResponseDays         *int           `json:"issueResponseDays,omitempty"`
	SecurityResponseDays      *int           `json:"securityResponseDays,omitempty"`
	SecurityAdvisoryCount     int            `json:"securityAdvisoryCount"`
	CriticalAdvisoryCount     int            `json:"criticalAdvisoryCount"`
	ScorecardScore            *float64       `json:"scorecardScore,omitempty"`
	SignedReleases            bool           `json:"signedReleases"`
	ProvenanceVerified        bool           `json:"provenanceVerified"`
	TypesSupported            bool           `json:"typesSupported"`
	BundleSizeBytes           *int64         `json:"bundleSizeBytes,omitempty"`
	RuntimeCompatibility      []string       `json:"runtimeCompatibility"`
	ProjectTypes              []string       `json:"projectTypes"`
	CompatibilityRequirements []string       `json:"compatibilityRequirements"`
	ExitConditions            []string       `json:"exitConditions"`
	MaintenanceSignals        map[string]any `json:"maintenanceSignals"`
	SecuritySignals           map[string]any `json:"securitySignals"`
	PopularitySignals         map[string]any `json:"popularitySignals"`
	Links                     []EvidenceLink `json:"links"`
	ObservedAt                time.Time      `json:"observedAt"`
}

type ComparisonDimension struct {
	Name      string         `json:"name"`
	Candidate string         `json:"candidate"`
	Current   string         `json:"current"`
	Verdict   string         `json:"verdict"`
	Evidence  []EvidenceLink `json:"evidence"`
}

type Assessment struct {
	SuggestedState State                 `json:"suggestedState"`
	Confidence     float64               `json:"confidence"`
	Misleading     bool                  `json:"misleading"`
	Dimensions     []ComparisonDimension `json:"dimensions"`
	Reasons        []string              `json:"reasons"`
}

type MetricSnapshot struct {
	ID                   string         `json:"id"`
	ObservedAt           time.Time      `json:"observedAt"`
	ReleaseVersion       string         `json:"releaseVersion"`
	License              string         `json:"license"`
	ContributorCount     int            `json:"contributorCount"`
	RuntimeCompatibility []string       `json:"runtimeCompatibility"`
	TypesSupported       bool           `json:"typesSupported"`
	BundleSizeBytes      *int64         `json:"bundleSizeBytes,omitempty"`
	Maintenance          map[string]any `json:"maintenance"`
	Security             map[string]any `json:"security"`
	Popularity           map[string]any `json:"popularity"`
	Provenance           map[string]any `json:"provenance"`
}

type Comparison struct {
	ID             string                `json:"id"`
	SuggestedState State                 `json:"suggestedState"`
	Confidence     float64               `json:"confidence"`
	Misleading     bool                  `json:"misleading"`
	Dimensions     []ComparisonDimension `json:"dimensions"`
	Evidence       []EvidenceLink        `json:"evidence"`
	AssessedAt     time.Time             `json:"assessedAt"`
}

type Decision struct {
	ID                        string         `json:"id"`
	State                     State          `json:"state"`
	DecisionSource            string         `json:"decisionSource"`
	Rationale                 string         `json:"rationale"`
	Evidence                  map[string]any `json:"evidence"`
	DecidedAt                 time.Time      `json:"decidedAt"`
	ReviewAt                  time.Time      `json:"reviewAt"`
	ApplicableProjectTypes    []string       `json:"applicableProjectTypes"`
	CompatibilityRequirements []string       `json:"compatibilityRequirements"`
	ExitConditions            []string       `json:"exitConditions"`
}

type Candidate struct {
	ID               string          `json:"id"`
	Ecosystem        string          `json:"ecosystem"`
	PackageName      string          `json:"packageName"`
	RepositoryURL    string          `json:"repositoryUrl"`
	DiscoveredAt     time.Time       `json:"discoveredAt"`
	DiscoverySource  string          `json:"discoverySource"`
	CurrentState     State           `json:"currentState"`
	IncumbentPackage string          `json:"incumbentPackage"`
	ReviewAt         time.Time       `json:"reviewAt"`
	Version          int64           `json:"version"`
	LatestMetric     *MetricSnapshot `json:"latestMetric,omitempty"`
	LatestComparison *Comparison     `json:"latestComparison,omitempty"`
	Decisions        []Decision      `json:"decisions"`
}

type DiscoveryRun struct {
	ID              string     `json:"id"`
	TriggerType     string     `json:"triggerType"`
	State           string     `json:"state"`
	EvidenceCount   int        `json:"evidenceCount"`
	CandidateCount  int        `json:"candidateCount"`
	MisleadingCount int        `json:"misleadingCount"`
	ErrorCode       string     `json:"errorCode,omitempty"`
	RequestedAt     time.Time  `json:"requestedAt"`
	StartedAt       *time.Time `json:"startedAt,omitempty"`
	CompletedAt     *time.Time `json:"completedAt,omitempty"`
}

type Snapshot struct {
	GeneratedAt time.Time      `json:"generatedAt"`
	States      []State        `json:"states"`
	Candidates  []Candidate    `json:"candidates"`
	Runs        []DiscoveryRun `json:"runs"`
	ReviewDue   int            `json:"reviewDue"`
}

type QueueDiscoveryRequest struct {
	UserID         string
	TriggerType    string
	IdempotencyKey string
}

type DecisionRequest struct {
	UserID                    string
	CandidateID               string
	ExpectedVersion           int64
	State                     State
	Rationale                 string
	Evidence                  map[string]any
	ReviewAt                  time.Time
	ApplicableProjectTypes    []string
	CompatibilityRequirements []string
	ExitConditions            []string
}

type DiscoveryResult struct {
	EvidenceCount   int
	CandidateCount  int
	MisleadingCount int
}
