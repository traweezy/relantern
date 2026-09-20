package jobqueue

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestInserterUsesArgumentDefaultsWithoutQueueOverride(t *testing.T) {
	inserter, err := NewInserter()
	if err != nil {
		t.Fatalf("NewInserter() error = %v", err)
	}
	if options := inserter.insertOptions(DeliverDigestArgs{}); options != nil {
		t.Fatalf("insertOptions() = %+v, want nil to preserve River defaults", options)
	}
}

func TestAdvisoryObservationInserterRejectsInvalidIdentities(t *testing.T) {
	inserter, err := NewInserter()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := inserter.EnqueueSplitAdvisoryObservation(
		context.Background(), nil, SplitAdvisoryObservationArgs{},
	); err == nil || !strings.Contains(err.Error(), "must be positive") {
		t.Fatalf("invalid split observation error = %v", err)
	}
	if _, _, err := inserter.EnqueueAssessAdvisoryObservation(
		context.Background(), nil, AssessAdvisoryObservationArgs{},
	); err == nil || !strings.Contains(err.Error(), "must be positive") {
		t.Fatalf("invalid assess event error = %v", err)
	}
	if _, _, err := inserter.EnqueueAssessAdvisoryObservation(
		context.Background(), nil, AssessAdvisoryObservationArgs{EventID: 1, RevisionID: "invalid"},
	); err == nil || !strings.Contains(err.Error(), "revision ID is invalid") {
		t.Fatalf("invalid assess revision error = %v", err)
	}
}

func TestIsolatedInserterOverridesOnlyQueue(t *testing.T) {
	const queue = "test_digest_workflow"
	inserter, err := NewIsolatedTestInserter(queue)
	if err != nil {
		t.Fatalf("NewIsolatedTestInserter() error = %v", err)
	}
	arguments := DeliverDigestArgs{DigestID: "digest-id"}
	want := arguments.InsertOpts()
	want.Queue = queue
	if got := inserter.insertOptions(arguments); got == nil || !reflect.DeepEqual(*got, want) {
		t.Fatalf("insertOptions() = %+v, want %+v", got, want)
	}
}

func TestIsolatedInserterRejectsUnsafeQueues(t *testing.T) {
	tests := map[string]string{
		"empty":            "",
		"production queue": QueueDelivery,
		"missing prefix":   "integration_digest",
		"invalid name":     "test_digest.queue",
		"too long":         "test_" + strings.Repeat("a", 60),
	}
	for name, queue := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := NewIsolatedTestInserter(queue); err == nil {
				t.Fatalf("NewIsolatedTestInserter(%q) succeeded", queue)
			}
		})
	}
}
