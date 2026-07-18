package electrum

import (
	"encoding/json"
	"fmt"
)

// Request is a JSON-RPC 2.0 request for the Electrum protocol.
//
// JSON-RPC 2.0 spec: https://www.jsonrpc.org/specification
//
// Example:
//
//	{"jsonrpc":"2.0","id":1,"method":"blockchain.scripthash.listunspent","params":["abc123"]}
type Request struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int64         `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

// NewRequest creates a JSON-RPC 2.0 request with the given ID and parameters.
func NewRequest(id int64, method string, params ...interface{}) *Request {
	return &Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
}

// Marshal serializes the request as JSON.
func (r *Request) Marshal() ([]byte, error) {
	// Ensure params is always [] (not null) for empty params, since
	// some Electrum servers reject null params.
	if r.Params == nil {
		r.Params = []interface{}{}
	}
	return json.Marshal(r)
}

// Response is a JSON-RPC 2.0 response from the Electrum server.
// Exactly one of Result or Error is non-nil.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC 2.0 error response.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error implements the error interface.
func (e *RPCError) Error() string {
	return fmt.Sprintf("electrum error %d: %s", e.Code, e.Message)
}

// Notification is a JSON-RPC 2.0 server-initiated message (no ID).
// Used for blockchain.scripthash.subscribe status updates.
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  []json.RawMessage `json:"params"`
}

// ScripthashStatus represents the status field of a scripthash notification.
// "abc..." (hex hash) means the address has changed since last poll; the
// client should re-fetch UTXOs. nil means no change.
type ScripthashStatus struct {
	Scripthash string `json:"-"`
	Status     *string `json:"status,omitempty"`
}

// ErrResponse is returned by Client implementations when the server
// returns a JSON-RPC error or the response is malformed.
type ErrResponse struct {
	Err *RPCError
}

func (e *ErrResponse) Error() string {
	if e.Err == nil {
		return "electrum: response error"
	}
	return e.Err.Error()
}

// Unwrap returns the underlying RPCError for errors.Is/As compatibility.
func (e *ErrResponse) Unwrap() error { return e.Err }

// UTXO represents an unspent transaction output as returned by
// blockchain.scripthash.listunspent.
type UTXO struct {
	TxHash string `json:"tx_hash"`
	TxPos  uint32 `json:"tx_pos"`
	Value  int64  `json:"value"`   // satoshis
	Height int    `json:"height"`  // 0 = unconfirmed, negative = unconfirmed
}

// HistoryEntry is a single entry from blockchain.scripthash.get_history.
// Confirmed entries have height >= 0; unconfirmed have height == 0.
type HistoryEntry struct {
	TxHash string `json:"tx_hash"`
	Height int    `json:"height"`
}

// Balance represents the result of blockchain.scripthash.get_balance.
// Confirmed and Unconfirmed are in satoshis.
type Balance struct {
	Confirmed   int64 `json:"confirmed"`
	Unconfirmed int64 `json:"unconfirmed"`
}

// FeeEstimate represents the result of blockchain.estimatefee.
type FeeEstimate struct {
	// FeePerKB is in satoshis per 1000 bytes (Electrum convention).
	FeePerKB int64 `json:"feerate,omitempty"`
}
