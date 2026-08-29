package extraction

import (
	"errors"
	"math/big"
	"regexp"
)

var usdPattern = regexp.MustCompile(`^(?:0|[1-9][0-9]{0,7})(?:\.[0-9]{1,8})?$`)

// USD is a fixed-point decimal amount passed to PostgreSQL numeric columns.
// It deliberately does not expose floating-point conversions.
type USD string

func ParseUSD(value string) (USD, error) {
	if !usdPattern.MatchString(value) {
		return "", errors.New("USD amount must be a non-negative decimal with at most eight fractional digits")
	}
	amount, valid := new(big.Rat).SetString(value)
	if !valid || amount.Sign() < 0 {
		return "", errors.New("USD amount must be a valid non-negative decimal")
	}
	return USD(value), nil
}

func ValidateBudgetRange(soft USD, hard USD) error {
	softAmount, valid := new(big.Rat).SetString(string(soft))
	if !valid || softAmount.Sign() <= 0 {
		return errors.New("monthly soft USD budget must be positive")
	}
	hardAmount, valid := new(big.Rat).SetString(string(hard))
	if !valid || hardAmount.Cmp(softAmount) < 0 {
		return errors.New("monthly hard USD budget must be greater than or equal to the soft budget")
	}
	return nil
}
