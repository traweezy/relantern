package alert

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
)

var ErrPayloadIntegrity = errors.New("critical alert delivery payload differs from its immutable hash")

type Delivery struct {
	AlertID        string
	Channel        string
	IdempotencyKey string
	Attempt        int
	Title          string
	SourceURL      string
	PackageName    string
	Ecosystem      string
	CurrentVersion string
	VersionRange   string
	PatchedVersion string
}

type Receipt struct {
	ProviderID string
}

type ReconcileResult struct {
	Requeued int
	Overdue  int64
}

// DeliverySHA256 covers the immutable fields while allowing attempt counts to
// advance under the same provider idempotency key.
func DeliverySHA256(delivery Delivery) [sha256.Size]byte {
	delivery.Attempt = 0
	encoded, _ := json.Marshal(delivery)
	return sha256.Sum256(encoded)
}
