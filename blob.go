package liblbry

import (
	"go.lumeweb.com/liblbry/blob"
)

type Blob = blob.Blob

func NewBlob(data, key, iv []byte) (Blob, error) {
	return blob.NewBlob(data, key, iv)
}
