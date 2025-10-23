package protocol

import "net/http"

// HTTPHandler defines the interface for handling HTTP blob requests
type HTTPHandler interface {
	HandleRequest(w http.ResponseWriter, r *http.Request)
}
