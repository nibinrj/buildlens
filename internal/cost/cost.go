// Package cost calculates fixed precision equivalent agent cost without floating point rounding.
package cost

import (
	"errors"
	"fmt"
	"math/big"
	"regexp"
)

var ratePattern = regexp.MustCompile(`^[0-9]{1,4}(\.[0-9]{1,6})?$`)

// Calculate rounds once, to six USD decimal places, matching build.cost_usd.
// The rate comes from the price snapshot; missing rate/duration are handled by the caller.
func Calculate(milliseconds int64, hourly string) (string, error) {
	if milliseconds < 0 || !ratePattern.MatchString(hourly) {
		return "", errors.New("cost: invalid duration or hourly rate")
	}
	rate, ok := new(big.Rat).SetString(hourly)
	if !ok {
		return "", errors.New("cost: invalid hourly rate")
	}
	v := new(big.Rat).Mul(rate, new(big.Rat).SetFrac(big.NewInt(milliseconds), big.NewInt(3_600_000)))
	micro := new(big.Rat).Mul(v, big.NewRat(1_000_000, 1))
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(micro.Num(), micro.Denom(), r)
	if new(big.Int).Lsh(r, 1).Cmp(micro.Denom()) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if q.Cmp(big.NewInt(999_999_999_999)) > 0 {
		return "", errors.New("cost: amount exceeds numeric(12,6)")
	}
	whole, fraction := new(big.Int), new(big.Int)
	whole.QuoRem(q, big.NewInt(1_000_000), fraction)
	return fmt.Sprintf("%s.%06d", whole.String(), fraction.Int64()), nil
}
