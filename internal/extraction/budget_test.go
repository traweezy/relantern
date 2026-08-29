package extraction

import "testing"

func TestUSDBudgetsRemainFixedPoint(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"0", "0.01", "25.00", "99999999.99999999"} {
		if _, err := ParseUSD(value); err != nil {
			t.Errorf("ParseUSD(%q) error = %v", value, err)
		}
	}
	for _, value := range []string{"", "-1", "+1", "01", "1.", ".1", "1.000000001", "NaN", "1e2"} {
		if _, err := ParseUSD(value); err == nil {
			t.Errorf("ParseUSD(%q) succeeded", value)
		}
	}
	if err := ValidateBudgetRange("25.00", "50.00"); err != nil {
		t.Fatalf("ValidateBudgetRange() error = %v", err)
	}
	if err := ValidateBudgetRange("0", "50.00"); err == nil {
		t.Fatal("ValidateBudgetRange() accepted a zero soft budget")
	}
	if err := ValidateBudgetRange("50.00", "25.00"); err == nil {
		t.Fatal("ValidateBudgetRange() accepted hard < soft")
	}
}
