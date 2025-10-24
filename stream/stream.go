package stream

import "go.lumeweb.com/liblbry/blob"

// Adapted from https://github.com/lbryio/lbry.go

// Stream represents a sequence of blobs forming a complete file
type Stream []blob.Blob

// -1 to leave room for padding, since there must be at least one byte of pkcs7 padding
const maxBlobDataSize = blob.MaxBlobSize - 1
