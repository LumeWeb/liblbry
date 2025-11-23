package stream

import (
	lbryTesting "go.lumeweb.com/liblbry/internal/testing"
)

// Helper function to generate multihashes for LBRY test hashes
func GenerateMultihashesForLBRYHashes() ([]string, error) {
	multihashes := make([]string, 0, len(lbryTesting.LBRYTestHashes))
	for _, lbryHash := range lbryTesting.LBRYTestHashes {
		multihashStr, err := ToMultihash(lbryHash)
		if err != nil {
			return nil, err
		}
		multihashes = append(multihashes, multihashStr)
	}
	return multihashes, nil
}
