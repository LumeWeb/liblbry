package protocol

// Protocol message types for LBRY reflector protocol

// HandshakeRequestResponse represents the handshake message
type HandshakeRequestResponse struct {
	Version *int `json:"version"`
}

// SendBlobRequest represents a blob upload request
type SendBlobRequest struct {
	BlobHash   string `json:"blob_hash,omitempty"`
	BlobSize   int    `json:"blob_size,omitempty"`
	SdBlobHash string `json:"sd_blob_hash,omitempty"`
	SdBlobSize int    `json:"sd_blob_size,omitempty"`
}

// SendBlobResponse represents a response to a regular blob request
type SendBlobResponse struct {
	SendBlob bool `json:"send_blob"`
}

// SendSDBlobResponse represents a response to an SD blob request
type SendSDBlobResponse struct {
	SendSdBlob  bool     `json:"send_sd_blob"`
	NeededBlobs []string `json:"needed_blobs,omitempty"`
}

// BlobTransferResponse represents a response after blob transfer
type BlobTransferResponse struct {
	ReceivedBlob bool `json:"received_blob"`
}

// SDBlobTransferResponse represents a response after SD blob transfer
type SDBlobTransferResponse struct {
	ReceivedSdBlob bool `json:"received_sd_blob"`
}
