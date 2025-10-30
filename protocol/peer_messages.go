package protocol

// CompositeRequest represents a request to a peer in the LBRY peer protocol.
// It can request either a single blob (RequestedBlob) or check availability of
// multiple blobs (RequestedBlobs). The BlobDataPaymentRate field is optional
// and can be used to negotiate payment terms.
type CompositeRequest struct {
	// List of blob hashes to check availability for
	RequestedBlobs []string `json:"requested_blobs,omitempty"`

	// Optional payment rate for blob data (in LBC/byte)
	BlobDataPaymentRate *float64 `json:"blob_data_payment_rate,omitempty"`

	// Single blob hash being requested for download
	RequestedBlob string `json:"requested_blob,omitempty"`
}

// CompositeResponse represents a response from a peer in the LBRY peer protocol.
// It contains either a list of available blobs or an incoming blob transfer.
type CompositeResponse struct {
	// List of blob hashes that are available from this peer
	AvailableBlobs []string `json:"available_blobs"`

	// Optional payment rate for blob data (in LBC/byte)
	BlobDataPaymentRate string `json:"blob_data_payment_rate,omitempty"`

	// Details about an incoming blob transfer, if any
	IncomingBlob *IncomingBlob `json:"incoming_blob,omitempty"`
}

// IncomingBlob contains information about a blob being transferred from a peer.
// If there was an error, it will be in the Error field.
type IncomingBlob struct {
	// Error message if something went wrong with the transfer
	Error string `json:"error,omitempty"`

	// Hash of the blob being transferred
	BlobHash string `json:"blob_hash"`

	// Size of the blob in bytes
	Length int `json:"length"`
}
