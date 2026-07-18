package coinselect

import (
	"testing"
)

// Helper: make a UTXO with EffectiveAmount pre-computed (skipping the
// estimator for cleaner test cases).
func utxo(amount, effective int64, confirmed bool) UTXO {
	var h int
	if confirmed {
		h = 1
	}
	return UTXO{
		TxID:            "txid",
		Vout:            0,
		Amount:          amount,
		EffectiveAmount: effective,
		Fee:             amount - effective,
		Height:          h,
	}
}

func TestBranchAndBound_ExactMatch(t *testing.T) {
	// UTXOs that exactly sum to target
	utxos := []UTXO{
		utxo(100, 100, true),
		utxo(200, 200, true),
		utxo(300, 300, true),
	}
	s := NewSelector(300, 50) // target 300, cost of change 50
	r := s.BranchAndBound(utxos)

	if !r.ExactMatch {
		t.Errorf("expected ExactMatch=true, got false")
	}
	if r.Effective != 300 {
		t.Errorf("expected effective total 300, got %d", r.Effective)
	}
	if r.Waste != 0 {
		t.Errorf("expected waste 0, got %d", r.Waste)
	}
}

func TestBranchAndBound_TwoSmallSumToTarget(t *testing.T) {
	// No single UTXO matches, but 100+200 = 300 exactly
	utxos := []UTXO{
		utxo(100, 100, true),
		utxo(200, 200, true),
		utxo(500, 500, true),
	}
	s := NewSelector(300, 100)
	r := s.BranchAndBound(utxos)

	if !r.ExactMatch {
		t.Error("expected exact match (100+200=300)")
	}
	if r.Effective != 300 {
		t.Errorf("expected effective 300, got %d", r.Effective)
	}
	if r.Waste != 0 {
		t.Errorf("expected waste 0, got %d", r.Waste)
	}
}

func TestBranchAndBound_OverShoot(t *testing.T) {
	// Only 500 is available, target is 250, overshoot is 250.
	// With cost of change 100, the BnB algorithm backtracks because
	// 250 waste > 100 cost. It should return an empty result; the Standard
	// strategy will then fall through to ClosestMatch which picks 500.
	utxos := []UTXO{utxo(500, 500, true)}
	s := NewSelector(250, 100)
	r := s.BranchAndBound(utxos)

	if len(r.UTXOs) != 0 {
		t.Errorf("expected BnB to return empty (waste > cost of change), got %d UTXOs", len(r.UTXOs))
	}

	// Verify Standard falls through to ClosestMatch and still funds it
	std := s.Standard(utxos)
	if len(std.UTXOs) != 1 {
		t.Errorf("expected Standard to find 1 UTXO via ClosestMatch, got %d", len(std.UTXOs))
	}
	if len(std.UTXOs) > 0 && std.UTXOs[0].Amount != 500 {
		t.Errorf("expected UTXO of 500, got %d", std.UTXOs[0].Amount)
	}
}

func TestBranchAndBound_InsufficientFunds(t *testing.T) {
	utxos := []UTXO{utxo(100, 100, true)}
	s := NewSelector(500, 50)
	r := s.BranchAndBound(utxos)

	if len(r.UTXOs) != 0 {
		t.Errorf("expected empty result, got %d UTXOs", len(r.UTXOs))
	}
}

func TestBranchAndBound_FiltersDust(t *testing.T) {
	// One UTXO with EffectiveAmount = 0 (dust)
	utxos := []UTXO{
		utxo(100, 0, true), // dust, should be filtered
		utxo(500, 500, true),
	}
	s := NewSelector(300, 50)
	// Direct BnB returns empty because the only viable UTXO (500) overshoots
	// target+cost (350). Standard should fall through to ClosestMatch.
	r := s.Standard(utxos)

	if len(r.UTXOs) != 1 {
		t.Errorf("expected 1 UTXO (dust filtered), got %d", len(r.UTXOs))
	}
	if len(r.UTXOs) > 0 && r.UTXOs[0].Amount != 500 {
		t.Errorf("expected UTXO of 500, got %d", r.UTXOs[0].Amount)
	}
}

func TestBranchAndBound_SkipsDuplicates(t *testing.T) {
	// Two identical UTXOs (same EffectiveAmount and Fee)
	// BnB's skip optimization should still find the right selection
	utxos := []UTXO{
		utxo(100, 100, true),
		utxo(100, 100, true),
		utxo(300, 300, true),
	}
	s := NewSelector(200, 50)
	r := s.BranchAndBound(utxos)

	if len(r.UTXOs) != 2 {
		t.Errorf("expected 2 UTXOs (100+100=200), got %d", len(r.UTXOs))
	}
}

func TestBranchAndBound_MinimizesWaste(t *testing.T) {
	// Target 200, cost 100. Options:
	//   - 200 alone: waste 0, but is this preferred over 100+100?
	//   - 100+100: waste 0
	//   - 500: waste 300 > cost_of_change, would backtrack
	utxos := []UTXO{
		utxo(100, 100, true),
		utxo(100, 100, true),
		utxo(200, 200, true),
		utxo(500, 500, true),
	}
	s := NewSelector(200, 100)
	r := s.BranchAndBound(utxos)

	if !r.ExactMatch {
		t.Error("expected exact match")
	}
	if r.Waste != 0 {
		t.Errorf("expected waste 0, got %d", r.Waste)
	}
}

func TestBranchAndBound_ManyUTXOs(t *testing.T) {
	// Stress test: 20 UTXOs of 100 each, target 350
	utxos := make([]UTXO, 20)
	for i := range utxos {
		utxos[i] = utxo(100, 100, true)
	}
	s := NewSelector(350, 50)
	r := s.BranchAndBound(utxos)

	// Should find 4 UTXOs (400, waste 50)
	if len(r.UTXOs) != 4 {
		t.Errorf("expected 4 UTXOs, got %d", len(r.UTXOs))
	}
	if r.Waste != 50 {
		t.Errorf("expected waste 50, got %d", r.Waste)
	}
}

func TestClosestMatch_SingleBest(t *testing.T) {
	utxos := []UTXO{
		utxo(100, 100, true),
		utxo(250, 250, true), // closest to target 200 + cost 50 = 250
		utxo(500, 500, true),
	}
	s := NewSelector(200, 50)
	r := s.ClosestMatch(utxos)

	if len(r.UTXOs) != 1 {
		t.Fatalf("expected 1 UTXO, got %d", len(r.UTXOs))
	}
	if r.UTXOs[0].Amount != 250 {
		t.Errorf("expected 250, got %d", r.UTXOs[0].Amount)
	}
}

func TestClosestMatch_NoneAboveTarget(t *testing.T) {
	utxos := []UTXO{utxo(100, 100, true)}
	s := NewSelector(500, 50)
	r := s.ClosestMatch(utxos)

	if len(r.UTXOs) != 0 {
		t.Errorf("expected empty, got %d UTXOs", len(r.UTXOs))
	}
}

func TestAccumulator_LargestFirst(t *testing.T) {
	utxos := []UTXO{
		utxo(100, 100, true),
		utxo(200, 200, true),
		utxo(500, 500, true),
	}
	s := NewSelector(250, 50)
	r := s.Accumulator(utxos)

	// Should pick 500 first (largest), overshooting to 500
	if len(r.UTXOs) != 1 {
		t.Errorf("expected 1 UTXO, got %d", len(r.UTXOs))
	}
	if len(r.UTXOs) > 0 && r.UTXOs[0].Amount != 500 {
		t.Errorf("expected 500 first, got %d", r.UTXOs[0].Amount)
	}
}

func TestAccumulator_MultipleToReach(t *testing.T) {
	utxos := []UTXO{
		utxo(100, 100, true),
		utxo(150, 150, true),
		utxo(200, 200, true),
	}
	// Target 300. Sorted desc: 200, 150, 100. 200+150=350. Waste 50.
	s := NewSelector(300, 50)
	r := s.Accumulator(utxos)

	if len(r.UTXOs) != 2 {
		t.Errorf("expected 2 UTXOs, got %d", len(r.UTXOs))
	}
	if r.Effective != 350 {
		t.Errorf("expected effective 350, got %d", r.Effective)
	}
}

func TestPreferConfirmed_UsesConfirmedFirst(t *testing.T) {
	utxos := []UTXO{
		utxo(500, 500, false),  // unconfirmed, 500
		utxo(100, 100, true),   // confirmed, 100
		utxo(200, 200, true),   // confirmed, 200
	}
	s := NewSelector(300, 50)
	r := s.PreferConfirmed(utxos)

	// Should use only confirmed (100+200=300), ignoring unconfirmed 500
	if len(r.UTXOs) != 2 {
		t.Errorf("expected 2 confirmed UTXOs, got %d", len(r.UTXOs))
	}
	for _, u := range r.UTXOs {
		if !u.IsConfirmed() {
			t.Error("PreferConfirmed returned an unconfirmed UTXO")
		}
	}
}

func TestPreferConfirmed_FallbackToAll(t *testing.T) {
	// Confirmed total < target, should fall back to full pool
	utxos := []UTXO{
		utxo(50, 50, true),   // confirmed, too small
		utxo(500, 500, false), // unconfirmed
	}
	s := NewSelector(300, 50)
	r := s.PreferConfirmed(utxos)

	// Confirmed alone can't reach target, so use the unconfirmed one
	if len(r.UTXOs) != 1 {
		t.Errorf("expected 1 UTXO (fallback), got %d", len(r.UTXOs))
	}
}

func TestStandard_FallsThroughStrategies(t *testing.T) {
	// BnB won't find a match (overshoot too big), closest match picks 500,
	// accumulator also picks 500
	utxos := []UTXO{
		utxo(100, 100, true),
		utxo(500, 500, true),
	}
	s := NewSelector(400, 100)
	r := s.Standard(utxos)

	if len(r.UTXOs) == 0 {
		t.Error("Standard should have found something via fallback")
	}
}

func TestUTXO_IsConfirmed(t *testing.T) {
	if !(UTXO{Height: 1}).IsConfirmed() {
		t.Error("Height=1 should be confirmed")
	}
	if (UTXO{Height: 0}).IsConfirmed() {
		t.Error("Height=0 should NOT be confirmed")
	}
	if (UTXO{Height: -1}).IsConfirmed() {
		t.Error("Height=-1 should NOT be confirmed")
	}
}

func TestUTXO_IsEffectivelyZero(t *testing.T) {
	if !(UTXO{EffectiveAmount: 0}).IsEffectivelyZero() {
		t.Error("EffectiveAmount=0 should be effectively zero")
	}
	if !(UTXO{EffectiveAmount: -100}).IsEffectivelyZero() {
		t.Error("negative EffectiveAmount should be effectively zero")
	}
	if (UTXO{EffectiveAmount: 1}).IsEffectivelyZero() {
		t.Error("EffectiveAmount=1 should NOT be effectively zero")
	}
}

func TestEstimate_PopulatesFields(t *testing.T) {
	utxos := []UTXO{
		{TxID: "a", Vout: 0, Amount: 1000},
		{TxID: "b", Vout: 1, Amount: 2000},
	}
	feePerByte := int64(10)
	expectedFee := EstimateFeeP2PKH(feePerByte)

	estimated := Estimate(utxos, feePerByte)

	if len(estimated) != len(utxos) {
		t.Fatalf("expected %d estimated UTXOs, got %d", len(utxos), len(estimated))
	}

	for i, u := range estimated {
		if u.Fee != expectedFee {
			t.Errorf("expected Fee=%d, got %d", expectedFee, u.Fee)
		}
		if u.EffectiveAmount != utxos[i].Amount-expectedFee {
			t.Errorf("expected EffectiveAmount=%d, got %d", utxos[i].Amount-expectedFee, u.EffectiveAmount)
		}
	}

	// Original UTXOs should not be mutated (value-copy semantics).
	for i, u := range utxos {
		if u.Fee != 0 || u.EffectiveAmount != 0 {
			t.Errorf("original UTXO %d was mutated: Fee=%d EffectiveAmount=%d", i, u.Fee, u.EffectiveAmount)
		}
	}
}

func TestEstimate_StandardSelectUsesEffectiveAmount(t *testing.T) {
	// raw amounts small enough that without fee adjustment they would be filtered
	feePerByte := int64(1)
	fee := EstimateFeeP2PKH(feePerByte)

	utxos := []UTXO{
		{TxID: "a", Vout: 0, Amount: 500},
		{TxID: "b", Vout: 1, Amount: 1000},
	}

	estimated := Estimate(utxos, feePerByte)
	target := estimated[1].EffectiveAmount // net value of the 1000-satoshi UTXO

	s := NewSelector(target, fee)
	r := s.Standard(estimated)

	if len(r.UTXOs) != 1 {
		t.Fatalf("expected 1 selected UTXO, got %d", len(r.UTXOs))
	}
	if r.UTXOs[0].Amount != 1000 {
		t.Errorf("expected UTXO amount 1000, got %d", r.UTXOs[0].Amount)
	}
	if r.Effective != target {
		t.Errorf("expected Effective=%d, got %d", target, r.Effective)
	}
}

func TestAvailable(t *testing.T) {
	feePerByte := int64(1)
	fee := EstimateFeeP2PKH(feePerByte)
	utxos := []UTXO{
		{TxID: "a", Vout: 0, Amount: 1000},
		{TxID: "b", Vout: 1, Amount: 2000},
	}

	estimated := Estimate(utxos, feePerByte)
	got := Available(estimated)
	want := int64(3000) - 2*fee
	if got != want {
		t.Errorf("expected Available=%d, got %d", want, got)
	}
}

