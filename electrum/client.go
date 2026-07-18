package electrum

import (
	"context"
	"encoding/json"
	"fmt"
)

// Client is the interface a caller implements to provide transport for
// Electrum JSON-RPC requests.
//
// Implementations might use:
//   - net.Conn + json.NewDecoder for raw TCP
//   - gorilla/websocket for WebSocket transport
//   - fetch() + TextDecoder for browser/WASM
//
// The contract:
//   - Call MUST send the request bytes over the wire and return the
//     raw response bytes (one JSON-RPC response, terminated however
//     the transport prefers).
//   - Call MUST respect ctx cancellation/deadline.
//   - Call MUST return *ErrResponse when the server returns a JSON-RPC error.
//   - Call MUST return a non-nil error if the response cannot be parsed.
//
// liblbry ships a NewDecoder helper that takes the raw bytes from Call
// and returns a parsed Response.
type Client interface {
	Call(ctx context.Context, method string, params ...interface{}) (json.RawMessage, error)
}

// ClientFunc adapts a plain function to the Client interface.
type ClientFunc func(ctx context.Context, method string, params ...interface{}) (json.RawMessage, error)

// Call implements Client.
func (f ClientFunc) Call(ctx context.Context, method string, params ...interface{}) (json.RawMessage, error) {
	return f(ctx, method, params...)
}

// CallTyped sends a request via the client and unmarshals the result into out.
// This is the primary typed accessor — wrap raw Client calls with this for
// type-safe responses.
func CallTyped(ctx context.Context, c Client, method string, out interface{}, params ...interface{}) error {
	raw, err := c.Call(ctx, method, params...)
	if err != nil {
		return err
	}
	resp := &Response{}
	if err := json.Unmarshal(raw, resp); err != nil {
		return fmt.Errorf("decode response: %w (raw: %s)", err, string(raw))
	}
	if resp.Error != nil {
		return &ErrResponse{Err: resp.Error}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(resp.Result, out)
}

// Subscribe returns the server's scripthash status. Used after calling
// blockchain.scripthash.subscribe.
func Subscribe(ctx context.Context, c Client, scripthash string) (string, error) {
	var status string
	err := CallTyped(ctx, c, MethodSubscribe, &status, scripthash)
	return status, err
}

// ListUnspent returns UTXOs for a scripthash.
func ListUnspent(ctx context.Context, c Client, scripthash string) ([]UTXO, error) {
	var utxos []UTXO
	err := CallTyped(ctx, c, MethodListUnspent, &utxos, scripthash)
	return utxos, err
}

// GetHistory returns confirmed and unconfirmed history for a scripthash.
func GetHistory(ctx context.Context, c Client, scripthash string) ([]HistoryEntry, error) {
	var history []HistoryEntry
	err := CallTyped(ctx, c, MethodGetHistory, &history, scripthash)
	return history, err
}

// GetBalance returns confirmed and unconfirmed balance for a scripthash.
func GetBalance(ctx context.Context, c Client, scripthash string) (Balance, error) {
	var bal Balance
	err := CallTyped(ctx, c, MethodGetBalance, &bal, scripthash)
	return bal, err
}

// Broadcast submits a raw transaction to the network. Returns the txid.
func Broadcast(ctx context.Context, c Client, txHex string) (string, error) {
	var txid string
	err := CallTyped(ctx, c, MethodBroadcast, &txid, txHex)
	return txid, err
}

// GetTxHex fetches a raw transaction in non-verbose mode.
func GetTxHex(ctx context.Context, c Client, txid string) (string, error) {
	var hexStr string
	err := CallTyped(ctx, c, MethodGetTx, &hexStr, txid, false)
	return hexStr, err
}

// GetTxVerbose fetches a transaction in verbose mode (full JSON).
func GetTxVerbose(ctx context.Context, c Client, txid string) (json.RawMessage, error) {
	var raw json.RawMessage
	err := CallTyped(ctx, c, MethodGetTxVerbose, &raw, txid, true)
	return raw, err
}

// Ping checks server availability.
func Ping(ctx context.Context, c Client) error {
	var result interface{}
	return CallTyped(ctx, c, MethodPing, &result)
}

// Version returns [client_version, protocol_version].
func Version(ctx context.Context, c Client) ([2]string, error) {
	var v [2]string
	err := CallTyped(ctx, c, MethodGetVersion, &v, "liblbry", ProtocolVersion)
	return v, err
}

// ResolveClaim calls blockchain.claimtrie.resolve.
// Returns the base64-encoded protobuf Outputs message.
func ResolveClaim(ctx context.Context, c Client, name string) (string, error) {
	var b64 string
	err := CallTyped(ctx, c, MethodResolve, &b64, name)
	return b64, err
}

// GetClaimByID calls blockchain.claimtrie.getclaimbyid.
// Returns the base64-encoded protobuf Outputs message.
func GetClaimByID(ctx context.Context, c Client, claimIDHex string) (string, error) {
	var b64 string
	err := CallTyped(ctx, c, MethodGetClaimByID, &b64, claimIDHex)
	return b64, err
}
