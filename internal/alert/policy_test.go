package alert

import (
	"testing"
	"time"
)

func TestDeliveryAtHonorsQuietHoursAndCriticalBypass(t *testing.T) {
	now := time.Date(2026, time.September, 20, 3, 0, 0, 0, time.UTC) // 23:00 in New York.
	quiet := QuietHours{Timezone: "America/New_York", Start: "22:00", End: "07:00"}
	deferred, err := DeliveryAt(now, quiet)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, time.September, 20, 11, 0, 0, 0, time.UTC)
	if !deferred.Equal(want) {
		t.Fatalf("DeliveryAt() = %s, want %s", deferred, want)
	}
	quiet.Bypass = true
	immediate, err := DeliveryAt(now, quiet)
	if err != nil || !immediate.Equal(now) {
		t.Fatalf("DeliveryAt(bypass) = %s, %v, want %s", immediate, err, now)
	}
}

func TestDeliveryAtResolvesSpringGapAtQuietEnd(t *testing.T) {
	now := time.Date(2027, time.March, 14, 6, 45, 0, 0, time.UTC)
	quiet := QuietHours{Timezone: "America/New_York", Start: "22:00", End: "02:30"}
	deferred, err := DeliveryAt(now, quiet)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2027, time.March, 14, 7, 0, 0, 0, time.UTC)
	if !deferred.Equal(want) {
		t.Fatalf("DeliveryAt() = %s, want first valid minute %s", deferred, want)
	}
}
