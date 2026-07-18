// Package coinselect implements coin selection algorithms for LBRY
// (and Bitcoin-style) transactions. Pure algorithms — no I/O.
//
// The primary algorithm is Branch-and-Bound (BnB), which finds an
// exact-match subset of UTXOs that meets the target with minimal waste.
// Falls back to Closest-Match (single UTXO ≥ target) and Accumulator
// (greedy largest-first) when BnB can't find an exact match.
//
// Algorithm ported from LBRYFoundation/lbry-daemon's
// wallet/coin_selection.go, which is itself a Go port of lbry-sdk's
// lbry/wallet/coinselection.py.
//
// All coin amounts are in satoshis.
package coinselect

import (
	"math/rand"
	"sort"
)

const (
	// MaximumTries caps the BnB iterations to prevent runaway search.
	// Default 100000 matches lbry-daemon.
	MaximumTries = 100_000

	// DefaultInputSize is the serialized size of a typical P2PKH input
	// (outpoint + script length + 107-byte signature + sequence).
	DefaultInputSize = 148

	// DefaultOutputSize is the serialized size of a typical P2PKH output
	// (value + script length + 25-byte P2PKH script).
	DefaultOutputSize = 34
)

// UTXO represents an unspent transaction output available for spending.
type UTXO struct {
	TxID            string // hex, internal byte order
	Vout            uint32
	Amount          int64 // satoshis (raw output value)
	EffectiveAmount int64 // satoshis (Amount - Fee, if precomputed)
	Fee             int64 // satoshis (fee to spend this input)
	Height          int   // 0 = unconfirmed, >0 = confirmed block height
}

// IsConfirmed reports whether the UTXO is confirmed (Height > 0).
func (u UTXO) IsConfirmed() bool {
	return u.Height > 0
}

// IsEffectivelyZero reports whether the UTXO has zero or negative effective
// value. Such UTXOs are filtered out by BnB because they cannot help reach
// the target.
func (u UTXO) IsEffectivelyZero() bool {
	return u.EffectiveAmount <= 0
}

// SelectionResult is the outcome of a coin selection strategy.
type SelectionResult struct {
	UTXOs      []UTXO
	Total      int64 // sum of raw Amount for selected UTXOs
	Effective  int64 // sum of EffectiveAmount for selected UTXOs
	Waste      int64 // Effective - target (overshoot)
	ExactMatch bool
	Tries      int
}

// Selector holds the parameters for coin selection.
type Selector struct {
	Target       int64
	CostOfChange int64
	MaxTries     int
}

// NewSelector creates a Selector with sensible defaults.
func NewSelector(target, costOfChange int64) *Selector {
	return &Selector{
		Target:       target,
		CostOfChange: costOfChange,
		MaxTries:     MaximumTries,
	}
}

// Select runs the Standard strategy and returns the selected UTXOs.
// It is the simplest entry point for callers that don't need the full
// SelectionResult metadata.
func (s *Selector) Select(utxos []UTXO) []UTXO {
	return s.Standard(utxos).UTXOs
}

// BranchAndBound runs the BnB algorithm to find an exact-match subset
// with minimal waste. Returns an empty SelectionResult if no exact match
// is found within MaxTries iterations.
//
// Algorithm (per lbry-sdk coinselection.py and lbry-daemon coin_selection.go):
//   1. Sort UTXOs by effective amount descending (stable sort)
//   2. DFS through combinations, pruning when:
//      a. current + remaining < target (cannot reach target)
//      b. current > target + cost_of_change (waste too high)
//   3. When current >= target with waste <= best_waste, record selection
//   4. Skip duplicate effective_amount+fee pairs (avoids redundant branches)
func (s *Selector) BranchAndBound(utxos []UTXO) SelectionResult {
	result := SelectionResult{}

	// Filter out dust/negative-effective UTXOs
	filtered := make([]UTXO, 0, len(utxos))
	for _, u := range utxos {
		if !u.IsEffectivelyZero() {
			filtered = append(filtered, u)
		}
	}
	if len(filtered) == 0 {
		return result
	}

	// Sort by effective amount descending, stable
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].EffectiveAmount > filtered[j].EffectiveAmount
	})

	// Compute total available effective amount
	available := int64(0)
	for _, u := range filtered {
		available += u.EffectiveAmount
	}
	if s.Target > available {
		return result // not enough funds
	}

	maxTries := s.MaxTries
	if maxTries <= 0 {
		maxTries = MaximumTries
	}

	currentValue := int64(0)
	currentAvailable := available
	currentSelection := make([]bool, 0, len(filtered))
	bestWaste := s.CostOfChange
	bestSelection := make([]bool, 0, len(filtered))

	tries := 0
	for tries < maxTries {
		tries++

		backtrack := currentValue+currentAvailable < s.Target ||
			currentValue > s.Target+s.CostOfChange
		if !backtrack && currentValue >= s.Target {
			newWaste := currentValue - s.Target
			if newWaste <= bestWaste {
				bestWaste = newWaste
				bestSelection = append(bestSelection[:0], currentSelection...)
			}
			backtrack = true
		}

		if backtrack {
			for len(currentSelection) > 0 && !currentSelection[len(currentSelection)-1] {
				currentSelection = currentSelection[:len(currentSelection)-1]
				currentAvailable += filtered[len(currentSelection)].EffectiveAmount
			}
			if len(currentSelection) == 0 {
				break
			}
			idx := len(currentSelection) - 1
			currentSelection[idx] = false
			currentValue -= filtered[idx].EffectiveAmount
			continue
		}

		idx := len(currentSelection)
		u := filtered[idx]
		currentAvailable -= u.EffectiveAmount

		// Skip duplicate (same effective_amount + fee as previous unselected)
		if idx > 0 && !currentSelection[idx-1] {
			prev := filtered[idx-1]
			if u.EffectiveAmount == prev.EffectiveAmount && u.Fee == prev.Fee {
				currentSelection = append(currentSelection, false)
				continue
			}
		}

		currentSelection = append(currentSelection, true)
		currentValue += u.EffectiveAmount
	}

	if len(bestSelection) == 0 {
		return result
	}

	result.ExactMatch = true
	result.Tries = tries
	result.Waste = bestWaste
	for i, include := range bestSelection {
		if include {
			result.UTXOs = append(result.UTXOs, filtered[i])
			result.Total += filtered[i].Amount
			result.Effective += filtered[i].EffectiveAmount
		}
	}
	return result
}

// ClosestMatch finds a single UTXO that is >= target + cost_of_change
// with the smallest overage. Returns an empty SelectionResult if no such
// UTXO exists.
func (s *Selector) ClosestMatch(utxos []UTXO) SelectionResult {
	result := SelectionResult{}
	target := s.Target + s.CostOfChange

	var bestMatch UTXO
	found := false
	var smallestChange int64

	for _, u := range utxos {
		if u.EffectiveAmount < target {
			continue
		}
		change := u.EffectiveAmount - target
		if !found || change < smallestChange {
			smallestChange = change
			bestMatch = u
			found = true
		}
	}

	if !found {
		return result
	}

	result.UTXOs = []UTXO{bestMatch}
	result.Total = bestMatch.Amount
	result.Effective = bestMatch.EffectiveAmount
	result.Waste = bestMatch.EffectiveAmount - s.Target // Effective overshoot vs raw target
	return result
}

// Accumulator is a greedy algorithm: sort UTXOs by EffectiveAmount
// descending and pick them one at a time until target is reached.
//
// Unlike BranchAndBound, this does NOT minimize waste — it picks large
// UTXOs first which often produces significant change. Use it as a
// last-resort fallback or when UTXO count is very small.
func (s *Selector) Accumulator(utxos []UTXO) SelectionResult {
	result := SelectionResult{}

	sorted := make([]UTXO, len(utxos))
	copy(sorted, utxos)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].EffectiveAmount > sorted[j].EffectiveAmount
	})

	var total int64
	for _, u := range sorted {
		if u.IsEffectivelyZero() {
			continue
		}
		result.UTXOs = append(result.UTXOs, u)
		total += u.EffectiveAmount
		if total >= s.Target {
			break
		}
	}

	if total < s.Target {
		result.UTXOs = nil
		return result
	}

	result.Effective = total
	result.Waste = total - s.Target
	for _, u := range result.UTXOs {
		result.Total += u.Amount
	}
	return result
}

// PreferConfirmed first tries to select from confirmed UTXOs only,
// falling back to the full pool if insufficient confirmed funds.
//
// "Confirmed" means Height > 0.
func (s *Selector) PreferConfirmed(utxos []UTXO) SelectionResult {
	confirmed := make([]UTXO, 0, len(utxos))
	for _, u := range utxos {
		if u.IsConfirmed() {
			confirmed = append(confirmed, u)
		}
	}
	if len(confirmed) == 0 {
		return s.Standard(utxos)
	}
	result := s.Standard(confirmed)
	if len(result.UTXOs) == 0 {
		return s.Standard(utxos)
	}
	return result
}

// Standard runs BranchAndBound → ClosestMatch → Accumulator as a
// tiered fallback. This is the recommended default.
func (s *Selector) Standard(utxos []UTXO) SelectionResult {
	if r := s.BranchAndBound(utxos); len(r.UTXOs) > 0 {
		return r
	}
	if r := s.ClosestMatch(utxos); len(r.UTXOs) > 0 {
		return r
	}
	return s.Accumulator(utxos)
}

// EffectiveAmount pairs a UTXO with the fee required to spend it.
type EffectiveAmount struct {
	UTXO UTXO
	Fee  int64
}

// EffectiveAmount returns the net value of the UTXO after subtracting the fee.
func (e EffectiveAmount) EffectiveAmount() int64 {
	return e.UTXO.Amount - e.Fee
}

// EstimateFee computes the fee for spending a UTXO given the fee-per-byte
// rate and the input's serialized size.
func EstimateFee(inputSize int, feePerByte int64) int64 {
	return int64(inputSize) * feePerByte
}

// EstimateFeeP2PKH computes the fee to spend a P2PKH UTXO at feePerByte.
func EstimateFeeP2PKH(feePerByte int64) int64 {
	return EstimateFee(DefaultInputSize, feePerByte)
}

// Estimate populates each UTXO's Fee and EffectiveAmount fields and
// returns a new slice with those fields set. The original UTXOs are not
// modified.
//
// For claim-script UTXOs, callers should override Fee based on the
// actual script size before calling Estimate or after receiving the
// returned slice.
func Estimate(utxos []UTXO, feePerByte int64) []UTXO {
	out := make([]UTXO, len(utxos))
	for i, u := range utxos {
		fee := EstimateFeeP2PKH(feePerByte)
		u.Fee = fee
		u.EffectiveAmount = u.Amount - fee
		out[i] = u
	}
	return out
}

// Available returns the sum of all effective amounts.
func Available(utxos []UTXO) int64 {
	var sum int64
	for _, u := range utxos {
		sum += u.EffectiveAmount
	}
	return sum
}

// randomDraw shuffles UTXOs and accumulates until target + cost_of_change
// is reached. It is not exported; it can be exposed later if needed.
func randomDraw(utxos []UTXO, target, costOfChange int64) SelectionResult {
	result := SelectionResult{}
	if len(utxos) == 0 {
		return result
	}

	shuffled := make([]UTXO, len(utxos))
	copy(shuffled, utxos)
	for i := len(shuffled) - 1; i > 0; i-- {
		j := rand.Intn(i + 1)
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}

	t := target + costOfChange
	var total int64
	for _, u := range shuffled {
		result.UTXOs = append(result.UTXOs, u)
		result.Total += u.Amount
		result.Effective += u.EffectiveAmount
		total += u.EffectiveAmount
		if total >= t {
			result.Waste = total - target
			return result
		}
	}

	return SelectionResult{}
}
