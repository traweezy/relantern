package soak

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	MaximumLedgerBytes      = 1 << 20
	MaximumArtifactBytes    = 64 << 20
	MinimumDuration         = 14 * 24 * time.Hour
	MaximumObservationGap   = 26 * time.Hour
	minimumReviewedTopItems = 10
)

var (
	gitSHA        = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	sha256Digest  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	railwayUUID   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	requiredKinds = []ExerciseKind{
		ExerciseRedeployDuringBacklog,
		ExerciseRedeployDigestWindow,
		ExerciseMissedMorningCatchup,
		ExerciseDatabaseRestore,
	}
)

type Outcome string

const (
	OutcomePass       Outcome = "pass"
	OutcomeExtend     Outcome = "extend"
	OutcomeFail       Outcome = "fail"
	OutcomeInProgress Outcome = "in_progress"
)

type ExerciseKind string

const (
	ExerciseProviderOutage        ExerciseKind = "provider_outage"
	ExerciseRedeployDuringBacklog ExerciseKind = "redeploy_during_backlog"
	ExerciseRedeployDigestWindow  ExerciseKind = "redeploy_digest_window"
	ExerciseMissedMorningCatchup  ExerciseKind = "missed_morning_catchup"
	ExerciseDatabaseRestore       ExerciseKind = "database_restore"
)

type FatalKind string

const (
	FatalSecurityBoundaryFailure FatalKind = "security_boundary_failure"
	FatalLostEvidence            FatalKind = "lost_evidence"
	FatalUnboundedProviderCost   FatalKind = "unbounded_provider_cost"
	FatalRepeatedCriticalMiss    FatalKind = "repeated_critical_miss"
)

type Artifact struct {
	Reference string `json:"reference"`
	SHA256    string `json:"sha256"`
}

type Profile struct {
	Priority0SourcesActive bool `json:"priority0SourcesActive"`
	RealReadOnlyAPIsActive bool `json:"realReadOnlyApisActive"`
	LowCapOpenAIProject    bool `json:"lowCapOpenAiProject"`
	StagingOnlyDelivery    bool `json:"stagingOnlyDelivery"`
}

type DemoChecks struct {
	Uptime        bool `json:"uptime"`
	Performance   bool `json:"performance"`
	Accessibility bool `json:"accessibility"`
	Isolation     bool `json:"isolation"`
}

type Observation struct {
	ObservedAt                string     `json:"observedAt"`
	ReleaseSHA                string     `json:"releaseSha"`
	FreshnessSLOMet           bool       `json:"freshnessSloMet"`
	DuplicateAlerts           int        `json:"duplicateAlerts"`
	UnsupportedMaterialClaims int        `json:"unsupportedMaterialClaims"`
	QueueHealthy              bool       `json:"queueHealthy"`
	Demo                      DemoChecks `json:"demo"`
	Evidence                  []Artifact `json:"evidence"`
}

type Exercise struct {
	Kind       ExerciseKind `json:"kind"`
	OccurredAt string       `json:"occurredAt"`
	ReleaseSHA string       `json:"releaseSha"`
	Provider   string       `json:"provider,omitempty"`
	Passed     bool         `json:"passed"`
	Evidence   []Artifact   `json:"evidence"`
}

type OwnerReview struct {
	ReviewedTopItems     int `json:"reviewedTopItems"`
	UsefulOrAlreadyKnown int `json:"usefulOrAlreadyKnown"`
}

type Cost struct {
	ActualCents  int64 `json:"actualCents"`
	BudgetCents  int64 `json:"budgetCents"`
	HardCapCents int64 `json:"hardCapCents"`
}

type FatalFinding struct {
	Kind       FatalKind  `json:"kind"`
	OccurredAt string     `json:"occurredAt"`
	Evidence   []Artifact `json:"evidence"`
}

type Ledger struct {
	Version              int            `json:"version"`
	Environment          string         `json:"environment"`
	ProjectID            string         `json:"projectId"`
	ReleaseSHA           string         `json:"releaseSha"`
	StartedAt            string         `json:"startedAt"`
	EndedAt              string         `json:"endedAt,omitempty"`
	Profile              Profile        `json:"profile"`
	Observations         []Observation  `json:"observations"`
	Exercises            []Exercise     `json:"exercises"`
	OwnerReview          OwnerReview    `json:"ownerReview"`
	Cost                 Cost           `json:"cost"`
	NonCriticalSLOMisses []string       `json:"nonCriticalSloMisses"`
	FatalFindings        []FatalFinding `json:"fatalFindings"`
}

type Report struct {
	Outcome          Outcome  `json:"outcome"`
	DurationHours    float64  `json:"durationHours"`
	ObservationCount int      `json:"observationCount"`
	ExerciseCount    int      `json:"exerciseCount"`
	ReviewedTopItems int      `json:"reviewedTopItems"`
	UsefulnessRate   float64  `json:"usefulnessRate"`
	ActualCostCents  int64    `json:"actualCostCents"`
	BudgetCents      int64    `json:"budgetCents"`
	Reasons          []string `json:"reasons"`
}

func Decode(reader io.Reader) (Ledger, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, MaximumLedgerBytes+1))
	if err != nil {
		return Ledger{}, fmt.Errorf("read soak ledger: %w", err)
	}
	if len(raw) > MaximumLedgerBytes {
		return Ledger{}, fmt.Errorf("soak ledger exceeds %d bytes", MaximumLedgerBytes)
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var ledger Ledger
	if err := decoder.Decode(&ledger); err != nil {
		return Ledger{}, fmt.Errorf("decode soak ledger: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Ledger{}, errors.New("decode soak ledger: trailing JSON value")
	}
	return ledger, nil
}

func VerifyLocalArtifacts(ledger Ledger, repositoryRoot string) error {
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return fmt.Errorf("resolve evidence root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve evidence root symlinks: %w", err)
	}

	for _, namedArtifact := range ledgerArtifacts(ledger) {
		artifact := namedArtifact.artifact
		if !validReference(artifact.Reference) {
			return fmt.Errorf("%s has an invalid evidence reference", namedArtifact.name)
		}
		parsed, err := url.Parse(artifact.Reference)
		if err != nil {
			return fmt.Errorf("parse %s reference: %w", namedArtifact.name, err)
		}
		if parsed.IsAbs() {
			continue
		}

		candidate := filepath.Join(root, filepath.FromSlash(artifact.Reference))
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", namedArtifact.name, err)
		}
		relative, err := filepath.Rel(root, resolved)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%s resolves outside the evidence root", namedArtifact.name)
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return fmt.Errorf("stat %s: %w", namedArtifact.name, err)
		}
		if !info.Mode().IsRegular() || info.Size() > MaximumArtifactBytes {
			return fmt.Errorf("%s must be a regular file no larger than %d bytes", namedArtifact.name, MaximumArtifactBytes)
		}
		file, err := os.Open(resolved)
		if err != nil {
			return fmt.Errorf("open %s: %w", namedArtifact.name, err)
		}
		hasher := sha256.New()
		copied, copyErr := io.Copy(hasher, io.LimitReader(file, MaximumArtifactBytes+1))
		closeErr := file.Close()
		if copyErr != nil {
			return fmt.Errorf("hash %s: %w", namedArtifact.name, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close %s: %w", namedArtifact.name, closeErr)
		}
		if copied > MaximumArtifactBytes {
			return fmt.Errorf("%s exceeds %d bytes", namedArtifact.name, MaximumArtifactBytes)
		}
		actual := fmt.Sprintf("%x", hasher.Sum(nil))
		if actual != artifact.SHA256 {
			return fmt.Errorf("%s SHA-256 mismatch", namedArtifact.name)
		}
	}
	return nil
}

func Evaluate(ledger Ledger, now time.Time) (Report, error) {
	startedAt, endedAt, observationTimes, exerciseTimes, findingTimes, err := validate(ledger)
	if err != nil {
		return Report{}, err
	}

	evaluationEnd := now
	if endedAt != nil {
		evaluationEnd = *endedAt
	}
	duration := time.Duration(0)
	if startedAt != nil && evaluationEnd.After(*startedAt) {
		duration = evaluationEnd.Sub(*startedAt)
	}

	reasons := evaluateReasons(
		ledger,
		startedAt,
		endedAt,
		observationTimes,
		exerciseTimes,
		findingTimes,
		now,
	)
	outcome := OutcomeInProgress
	if len(ledger.FatalFindings) > 0 || ledger.Cost.HardCapCents > 0 && ledger.Cost.ActualCents > ledger.Cost.HardCapCents {
		outcome = OutcomeFail
	} else if endedAt != nil {
		outcome = OutcomePass
		if len(reasons) > 0 {
			outcome = OutcomeExtend
		}
	}

	usefulnessRate := float64(0)
	if ledger.OwnerReview.ReviewedTopItems > 0 {
		usefulnessRate = float64(ledger.OwnerReview.UsefulOrAlreadyKnown) /
			float64(ledger.OwnerReview.ReviewedTopItems)
	}

	return Report{
		Outcome:          outcome,
		DurationHours:    duration.Hours(),
		ObservationCount: len(ledger.Observations),
		ExerciseCount:    len(ledger.Exercises),
		ReviewedTopItems: ledger.OwnerReview.ReviewedTopItems,
		UsefulnessRate:   usefulnessRate,
		ActualCostCents:  ledger.Cost.ActualCents,
		BudgetCents:      ledger.Cost.BudgetCents,
		Reasons:          reasons,
	}, nil
}

func validate(ledger Ledger) (
	*time.Time,
	*time.Time,
	[]time.Time,
	[]time.Time,
	[]time.Time,
	error,
) {
	var problems []string
	if ledger.Version != 1 {
		problems = append(problems, "version must be 1")
	}
	if ledger.Environment != "staging" {
		problems = append(problems, "environment must be staging")
	}
	if ledger.ProjectID != "" && !railwayUUID.MatchString(ledger.ProjectID) {
		problems = append(problems, "projectId must be a lowercase Railway UUID")
	}
	if ledger.ReleaseSHA != "" && !gitSHA.MatchString(ledger.ReleaseSHA) {
		problems = append(problems, "releaseSha must be a full lowercase Git SHA")
	}

	startedAt := parseOptionalTime("startedAt", ledger.StartedAt, &problems)
	endedAt := parseOptionalTime("endedAt", ledger.EndedAt, &problems)
	if startedAt == nil && endedAt != nil {
		problems = append(problems, "endedAt requires startedAt")
	}
	if startedAt != nil && endedAt != nil && !endedAt.After(*startedAt) {
		problems = append(problems, "endedAt must be after startedAt")
	}

	observationTimes := make([]time.Time, 0, len(ledger.Observations))
	for index, observation := range ledger.Observations {
		prefix := fmt.Sprintf("observations[%d]", index)
		observationTimes = append(observationTimes, parseRequiredTime(prefix+".observedAt", observation.ObservedAt, &problems))
		validateEventSHA(prefix, ledger.ReleaseSHA, observation.ReleaseSHA, &problems)
		if observation.DuplicateAlerts < 0 || observation.UnsupportedMaterialClaims < 0 {
			problems = append(problems, prefix+" counts must be non-negative")
		}
		validateArtifacts(prefix+".evidence", observation.Evidence, &problems)
	}
	validateChronology("observations", observationTimes, &problems)

	exerciseTimes := make([]time.Time, 0, len(ledger.Exercises))
	seenExercises := make(map[ExerciseKind]struct{}, len(ledger.Exercises))
	for index, exercise := range ledger.Exercises {
		prefix := fmt.Sprintf("exercises[%d]", index)
		exerciseTimes = append(exerciseTimes, parseRequiredTime(prefix+".occurredAt", exercise.OccurredAt, &problems))
		validateEventSHA(prefix, ledger.ReleaseSHA, exercise.ReleaseSHA, &problems)
		if !validExerciseKind(exercise.Kind) {
			problems = append(problems, prefix+".kind is unsupported")
		}
		if exercise.Kind == ExerciseProviderOutage && strings.TrimSpace(exercise.Provider) == "" {
			problems = append(problems, prefix+".provider is required for provider_outage")
		}
		if exercise.Kind != ExerciseProviderOutage && exercise.Provider != "" {
			problems = append(problems, prefix+".provider is only valid for provider_outage")
		}
		if _, exists := seenExercises[exercise.Kind]; exists && exercise.Kind != ExerciseProviderOutage {
			problems = append(problems, prefix+".kind is duplicated")
		}
		seenExercises[exercise.Kind] = struct{}{}
		validateArtifacts(prefix+".evidence", exercise.Evidence, &problems)
	}
	validateChronology("exercises", exerciseTimes, &problems)

	findingTimes := make([]time.Time, 0, len(ledger.FatalFindings))
	for index, finding := range ledger.FatalFindings {
		prefix := fmt.Sprintf("fatalFindings[%d]", index)
		findingTimes = append(findingTimes, parseRequiredTime(prefix+".occurredAt", finding.OccurredAt, &problems))
		if !validFatalKind(finding.Kind) {
			problems = append(problems, prefix+".kind is unsupported")
		}
		validateArtifacts(prefix+".evidence", finding.Evidence, &problems)
	}
	validateChronology("fatalFindings", findingTimes, &problems)

	if ledger.OwnerReview.ReviewedTopItems < 0 || ledger.OwnerReview.UsefulOrAlreadyKnown < 0 {
		problems = append(problems, "ownerReview counts must be non-negative")
	}
	if ledger.OwnerReview.UsefulOrAlreadyKnown > ledger.OwnerReview.ReviewedTopItems {
		problems = append(problems, "ownerReview useful count cannot exceed reviewed count")
	}
	if ledger.Cost.ActualCents < 0 || ledger.Cost.BudgetCents < 0 || ledger.Cost.HardCapCents < 0 {
		problems = append(problems, "cost values must be non-negative integer cents")
	}
	if ledger.Cost.HardCapCents > 0 && ledger.Cost.BudgetCents > ledger.Cost.HardCapCents {
		problems = append(problems, "cost budget cannot exceed the hard cap")
	}
	for index, miss := range ledger.NonCriticalSLOMisses {
		if strings.TrimSpace(miss) == "" {
			problems = append(problems, fmt.Sprintf("nonCriticalSloMisses[%d] must not be empty", index))
		}
	}
	if len(problems) > 0 {
		return nil, nil, nil, nil, nil, errors.New(strings.Join(problems, "; "))
	}
	return startedAt, endedAt, observationTimes, exerciseTimes, findingTimes, nil
}

func evaluateReasons(
	ledger Ledger,
	startedAt *time.Time,
	endedAt *time.Time,
	observationTimes []time.Time,
	exerciseTimes []time.Time,
	findingTimes []time.Time,
	now time.Time,
) []string {
	var reasons []string
	if ledger.ProjectID == "" {
		reasons = append(reasons, "Railway staging project ID is missing")
	}
	if ledger.ReleaseSHA == "" {
		reasons = append(reasons, "frozen release SHA is missing")
	}
	if startedAt == nil {
		reasons = append(reasons, "soak start time is missing")
	}
	if endedAt == nil {
		reasons = append(reasons, "soak has not been concluded")
	}
	if startedAt != nil {
		if startedAt.After(now) {
			reasons = append(reasons, "soak start time is in the future")
		}
		end := now
		if endedAt != nil {
			end = *endedAt
			if endedAt.After(now) {
				reasons = append(reasons, "soak end time is in the future")
			}
		}
		if end.Sub(*startedAt) < MinimumDuration {
			reasons = append(reasons, "minimum 336-hour soak duration has not elapsed")
		}
		validateEvidenceWindow(*startedAt, end, observationTimes, exerciseTimes, findingTimes, &reasons)
	}
	if !ledger.Profile.Priority0SourcesActive {
		reasons = append(reasons, "Priority 0 sources are not confirmed active")
	}
	if !ledger.Profile.RealReadOnlyAPIsActive {
		reasons = append(reasons, "real read-only APIs are not confirmed active")
	}
	if !ledger.Profile.LowCapOpenAIProject {
		reasons = append(reasons, "low-cap staging OpenAI project is not confirmed")
	}
	if !ledger.Profile.StagingOnlyDelivery {
		reasons = append(reasons, "staging-only delivery isolation is not confirmed")
	}

	if len(ledger.Observations) < 14 {
		reasons = append(reasons, "at least 14 daily observations are required")
	}
	for index, observation := range ledger.Observations {
		prefix := fmt.Sprintf("observation %d", index+1)
		if !observation.FreshnessSLOMet {
			reasons = append(reasons, prefix+" missed the freshness SLO")
		}
		if observation.DuplicateAlerts > 0 {
			reasons = append(reasons, prefix+" recorded duplicate alerts")
		}
		if observation.UnsupportedMaterialClaims > 0 {
			reasons = append(reasons, prefix+" recorded unsupported material claims")
		}
		if !observation.QueueHealthy {
			reasons = append(reasons, prefix+" did not have a healthy recovered queue")
		}
		if !observation.Demo.Uptime || !observation.Demo.Performance ||
			!observation.Demo.Accessibility || !observation.Demo.Isolation {
			reasons = append(reasons, prefix+" did not pass all anonymous demo checks")
		}
		if len(observation.Evidence) == 0 {
			reasons = append(reasons, prefix+" has no durable evidence")
		}
	}

	outageCount := 0
	for index, exercise := range ledger.Exercises {
		if exercise.Kind == ExerciseProviderOutage {
			outageCount++
		}
		if !exercise.Passed {
			reasons = append(reasons, fmt.Sprintf("exercise %d did not pass", index+1))
		}
		if len(exercise.Evidence) == 0 {
			reasons = append(reasons, fmt.Sprintf("exercise %d has no durable evidence", index+1))
		}
	}
	if outageCount < 2 {
		reasons = append(reasons, "at least two provider outage exercises are required")
	}
	for _, kind := range requiredKinds {
		if !slices.ContainsFunc(ledger.Exercises, func(exercise Exercise) bool { return exercise.Kind == kind }) {
			reasons = append(reasons, fmt.Sprintf("required %s exercise is missing", kind))
		}
	}

	if ledger.OwnerReview.ReviewedTopItems < minimumReviewedTopItems {
		reasons = append(reasons, "owner review has fewer than 10 top items")
	} else if ledger.OwnerReview.UsefulOrAlreadyKnown*5 < ledger.OwnerReview.ReviewedTopItems*4 {
		reasons = append(reasons, "owner usefulness rate is below 80 percent")
	}
	if ledger.Cost.BudgetCents <= 0 {
		reasons = append(reasons, "cost budget is not configured")
	} else if ledger.Cost.ActualCents > ledger.Cost.BudgetCents {
		reasons = append(reasons, "provider cost exceeded the reviewed budget")
	}
	if ledger.Cost.HardCapCents <= 0 {
		reasons = append(reasons, "provider hard cap is not configured")
	} else if ledger.Cost.ActualCents > ledger.Cost.HardCapCents {
		reasons = append(reasons, "provider cost exceeded the hard cap")
	}
	for _, miss := range ledger.NonCriticalSLOMisses {
		reasons = append(reasons, "non-critical SLO miss: "+miss)
	}
	for _, finding := range ledger.FatalFindings {
		reasons = append(reasons, "fatal staging finding: "+string(finding.Kind))
	}
	for index, finding := range ledger.FatalFindings {
		if len(finding.Evidence) == 0 {
			reasons = append(reasons, fmt.Sprintf("fatal finding %d has no durable evidence", index+1))
		}
	}
	return reasons
}

func parseOptionalTime(name string, value string, problems *[]string) *time.Time {
	if value == "" {
		return nil
	}
	parsed := parseRequiredTime(name, value, problems)
	if parsed.IsZero() {
		return nil
	}
	return &parsed
}

func parseRequiredTime(name string, value string, problems *[]string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		*problems = append(*problems, name+" must be RFC3339")
		return time.Time{}
	}
	return parsed
}

func validateChronology(name string, times []time.Time, problems *[]string) {
	for index := 1; index < len(times); index++ {
		if !times[index].After(times[index-1]) {
			*problems = append(*problems, fmt.Sprintf("%s must be strictly chronological", name))
			return
		}
	}
}

func validateEventSHA(prefix string, ledgerSHA string, eventSHA string, problems *[]string) {
	if !gitSHA.MatchString(eventSHA) {
		*problems = append(*problems, prefix+".releaseSha must be a full lowercase Git SHA")
	} else if ledgerSHA != "" && eventSHA != ledgerSHA {
		*problems = append(*problems, prefix+".releaseSha does not match the frozen release SHA")
	}
}

func validateArtifacts(prefix string, artifacts []Artifact, problems *[]string) {
	for index, artifact := range artifacts {
		name := fmt.Sprintf("%s[%d]", prefix, index)
		if !validReference(artifact.Reference) {
			*problems = append(*problems, name+".reference must be a safe repository-relative path or HTTPS URL")
		}
		if !sha256Digest.MatchString(artifact.SHA256) {
			*problems = append(*problems, name+".sha256 must be a lowercase SHA-256 digest")
		}
	}
}

func validReference(reference string) bool {
	if reference == "" || strings.ContainsAny(reference, "\r\n\t\\") {
		return false
	}
	parsed, err := url.Parse(reference)
	if err != nil {
		return false
	}
	if parsed.IsAbs() {
		return parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil &&
			parsed.RawQuery == "" && parsed.Fragment == ""
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	return !strings.HasPrefix(reference, "/") && path.Clean(reference) == reference &&
		reference != "." && !strings.HasPrefix(reference, "../")
}

type namedArtifact struct {
	name     string
	artifact Artifact
}

func ledgerArtifacts(ledger Ledger) []namedArtifact {
	artifacts := make([]namedArtifact, 0)
	for index, observation := range ledger.Observations {
		for evidenceIndex, artifact := range observation.Evidence {
			artifacts = append(artifacts, namedArtifact{
				name:     fmt.Sprintf("observations[%d].evidence[%d]", index, evidenceIndex),
				artifact: artifact,
			})
		}
	}
	for index, exercise := range ledger.Exercises {
		for evidenceIndex, artifact := range exercise.Evidence {
			artifacts = append(artifacts, namedArtifact{
				name:     fmt.Sprintf("exercises[%d].evidence[%d]", index, evidenceIndex),
				artifact: artifact,
			})
		}
	}
	for index, finding := range ledger.FatalFindings {
		for evidenceIndex, artifact := range finding.Evidence {
			artifacts = append(artifacts, namedArtifact{
				name:     fmt.Sprintf("fatalFindings[%d].evidence[%d]", index, evidenceIndex),
				artifact: artifact,
			})
		}
	}
	return artifacts
}

func validExerciseKind(kind ExerciseKind) bool {
	return kind == ExerciseProviderOutage || slices.Contains(requiredKinds, kind)
}

func validFatalKind(kind FatalKind) bool {
	return kind == FatalSecurityBoundaryFailure || kind == FatalLostEvidence ||
		kind == FatalUnboundedProviderCost || kind == FatalRepeatedCriticalMiss
}

func validateEvidenceWindow(
	startedAt time.Time,
	endedAt time.Time,
	observationTimes []time.Time,
	exerciseTimes []time.Time,
	findingTimes []time.Time,
	reasons *[]string,
) {
	for _, namedTimes := range []struct {
		name  string
		times []time.Time
	}{
		{name: "observation", times: observationTimes},
		{name: "exercise", times: exerciseTimes},
		{name: "fatal finding", times: findingTimes},
	} {
		for _, eventTime := range namedTimes.times {
			if eventTime.Before(startedAt) || eventTime.After(endedAt) {
				*reasons = append(*reasons, namedTimes.name+" falls outside the soak window")
			}
		}
	}
	if len(observationTimes) == 0 {
		return
	}
	if observationTimes[0].Sub(startedAt) > MaximumObservationGap {
		*reasons = append(*reasons, "first observation is more than 26 hours after soak start")
	}
	for index := 1; index < len(observationTimes); index++ {
		if observationTimes[index].Sub(observationTimes[index-1]) > MaximumObservationGap {
			*reasons = append(*reasons, "observation continuity has a gap greater than 26 hours")
			break
		}
	}
	if endedAt.Sub(observationTimes[len(observationTimes)-1]) > MaximumObservationGap {
		*reasons = append(*reasons, "last observation is more than 26 hours before soak end")
	}
}
