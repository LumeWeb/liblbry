package executor

import (
	"context"

	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/blob"
)

// CompletionHandler defines the interface for handling task completion callbacks
type CompletionHandler interface {
	// CompleteWithData completes a request with successful data
	CompleteWithData(req *blob.BlobRequest, data []byte, raceCancel context.CancelFunc, hash string)

	// CompleteWithError completes a request with an error
	CompleteWithError(req *blob.BlobRequest, err error) error

	// CompleteWithErrorAndCancel completes a request with an error and cancels race attempts
	CompleteWithErrorAndCancel(req *blob.BlobRequest, err error, raceCancel context.CancelFunc, hash string)

	// TrackFailedPeer tracks a failed peer attempt
	TrackFailedPeer(req *blob.BlobRequest, err error) (int32, bool)
}
