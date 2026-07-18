// Package electrum provides LBRY Electrum JSON-RPC protocol types.
//
// This package is I/O-free: it defines request/response types and a
// Client interface that the caller implements for their preferred
// transport (TCP, WebSocket, fetch, etc.).
//
// The caller is responsible for:
//   - Establishing the connection (TCP for native Go, WebSocket for WASM)
//   - JSON-RPC framing (newline-delimited JSON for TCP, message frames for WS)
//   - Concurrency (one request per goroutine, or multiplexing by ID)
//   - Reconnection, backoff, server discovery
//
// liblbry only handles the protocol shape and type-safe request/response
// builders so consumers don't hand-roll JSON marshaling for every call.
package electrum

// Method constants for the LBRY Electrum protocol. These match the method
// names exposed by lbry-hub, hubzilla.lbry.io, and a-hub1.odysee.com.
const (
	// Blockchain methods (subscribed to via blockchain.scripthash.subscribe)
	MethodGetVersion         = "server.version"
	MethodPing               = "server.ping"
	MethodBroadcast          = "blockchain.transaction.broadcast"
	MethodGetTx              = "blockchain.transaction.get"
	MethodGetTxVerbose       = "blockchain.transaction.get" // verbose=true
	MethodGetTxOut           = "blockchain.block.headers"   // not used; placeholder

	// Scripthash subscription methods
	MethodSubscribe     = "blockchain.scripthash.subscribe"
	MethodUnsubscribe   = "blockchain.scripthash.unsubscribe"
	MethodListUnspent   = "blockchain.scripthash.listunspent"
	MethodGetHistory    = "blockchain.scripthash.get_history"
	MethodGetBalance    = "blockchain.scripthash.get_balance"

	// Claimtrie methods (LBRY-specific)
	MethodGetClaimByID = "blockchain.claimtrie.getclaimbyid"
	MethodResolve      = "blockchain.claimtrie.resolve"

	// Estimate fee
	MethodEstimateFee = "blockchain.estimatefee"
)

// Electrum protocol version we target.
const ProtocolVersion = "1.4.1"
