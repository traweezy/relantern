package worker

import (
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/traweezy/relantern/internal/clock"
	"github.com/traweezy/relantern/internal/jobqueue"
)

func TestExtractionWorkerUsesConfiguredTimeout(t *testing.T) {
	t.Parallel()

	worker := &extractItemWorker{timeout: 7 * time.Minute}
	if got := worker.Timeout(&river.Job[jobqueue.ExtractItemArgs]{}); got != 7*time.Minute {
		t.Fatalf("Timeout() = %s", got)
	}
	if _, err := NewRiverClient(
		nil,
		clock.System{},
		time.Minute,
		time.Second,
		false,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		0,
	); err == nil {
		t.Fatal("NewRiverClient() accepted a zero extraction timeout")
	}
}
