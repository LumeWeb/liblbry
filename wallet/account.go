package wallet

import "fmt"

// DefaultGapLimit is the standard BIP44 gap limit: scan 20 addresses ahead
// of the last used address before considering the chain exhausted.
const DefaultGapLimit = 20

// ChainType identifies an HD derivation chain.
type ChainType uint32

const (
	ChainReceiving ChainType = 0
	ChainChange    ChainType = 1
	ChainChannel   ChainType = 2
)

// AddressInfo holds a derived address and its derivation path.
type AddressInfo struct {
	Address string
	Chain   ChainType
	Index   uint32
	Used    bool
}

// AddressManager manages address derivation with gap-limit scanning.
// It tracks which addresses have been used (received transactions) and
// generates new addresses up to gap_limit ahead of the last used address.
//
// The caller is responsible for checking on-chain usage — AddressManager
// is pure state and does no I/O.
type AddressManager struct {
	wallet   *Wallet
	chain    ChainType
	gapLimit int

	// addresses holds all derived addresses, indexed by position.
	addresses []*AddressInfo

	// lastUsedIndex is the highest index known to have received funds.
	// -1 means no addresses have been used.
	lastUsedIndex int
}

// NewAddressManager creates an AddressManager for the given chain.
func NewAddressManager(w *Wallet, chain ChainType, gapLimit int) *AddressManager {
	if gapLimit <= 0 {
		gapLimit = DefaultGapLimit
	}
	return &AddressManager{
		wallet:        w,
		chain:         chain,
		gapLimit:      gapLimit,
		lastUsedIndex: -1,
	}
}

// EnsureGap ensures that at least gapLimit unused addresses exist beyond
// the last used address. Returns the list of newly derived addresses.
func (am *AddressManager) EnsureGap() ([]*AddressInfo, error) {
	var newAddrs []*AddressInfo

	// After lastUsedIndex N, we need addresses 0..N+gapLimit inclusive,
	// which is N + gapLimit + 1 total. If nothing used yet (lastUsedIndex=-1),
	// we need 0..gapLimit-1 = gapLimit total.
	needed := am.gapLimit
	if am.lastUsedIndex >= 0 {
		needed = am.lastUsedIndex + 1 + am.gapLimit
	}

	for i := len(am.addresses); i < needed; i++ {
		addr, err := am.wallet.AddressStringAt(uint32(am.chain), uint32(i))
		if err != nil {
			return nil, fmt.Errorf("derive address at chain=%d index=%d: %w", am.chain, i, err)
		}
		info := &AddressInfo{
			Address: addr,
			Chain:   am.chain,
			Index:   uint32(i),
			Used:    false,
		}
		am.addresses = append(am.addresses, info)
		newAddrs = append(newAddrs, info)
	}

	return newAddrs, nil
}

// MarkUsed marks the address at the given index as used and updates
// the lastUsedIndex. Returns newly generated addresses if the gap
// needs to be extended.
func (am *AddressManager) MarkUsed(index int) ([]*AddressInfo, error) {
	if index < 0 {
		return nil, fmt.Errorf("index must be non-negative")
	}

	// Ensure we have enough addresses to cover this index
	needed := index + 1
	for len(am.addresses) < needed {
		i := len(am.addresses)
		addr, err := am.wallet.AddressStringAt(uint32(am.chain), uint32(i))
		if err != nil {
			return nil, fmt.Errorf("derive address at index %d: %w", i, err)
		}
		am.addresses = append(am.addresses, &AddressInfo{
			Address: addr,
			Chain:   am.chain,
			Index:   uint32(i),
			Used:    false,
		})
	}

	am.addresses[index].Used = true
	if index > am.lastUsedIndex {
		am.lastUsedIndex = index
	}

	// Ensure gap limit is maintained after marking
	return am.EnsureGap()
}

// CurrentAddresses returns all known addresses.
func (am *AddressManager) CurrentAddresses() []*AddressInfo {
	return am.addresses
}

// UnusedAddresses returns addresses that have not been marked as used.
func (am *AddressManager) UnusedAddresses() []*AddressInfo {
	var unused []*AddressInfo
	for _, a := range am.addresses {
		if !a.Used {
			unused = append(unused, a)
		}
	}
	return unused
}

// LastUsedIndex returns the highest index known to have received funds.
// Returns -1 if no addresses have been used.
func (am *AddressManager) LastUsedIndex() int { return am.lastUsedIndex }

// NextUnusedAddress returns the first unused address, or an error if
// the gap is exhausted (shouldn't happen after EnsureGap).
func (am *AddressManager) NextUnusedAddress() (*AddressInfo, error) {
	for _, a := range am.addresses {
		if !a.Used {
			return a, nil
		}
	}
	return nil, fmt.Errorf("no unused addresses available (gap exhausted)")
}
