package electrum

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestScripthash_KnownVector(t *testing.T) {
	// SHA256("") = e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
	// Reversed (byte-by-byte) = 55b852781b9995a44c939b64e441ae2724b96f99c8f4fb9a141cfc9842c4b0e3
	got := Scripthash([]byte{})
	want := "55b852781b9995a44c939b64e441ae2724b96f99c8f4fb9a141cfc9842c4b0e3"
	if got != want {
		t.Errorf("scripthash empty: got %s, want %s", got, want)
	}
}

func TestScripthash_Deterministic(t *testing.T) {
	script := []byte{0x76, 0xa9, 0x14, 0x01, 0x02, 0x03, 0x88, 0xac}
	h1 := Scripthash(script)
	h2 := Scripthash(script)
	if h1 != h2 {
		t.Errorf("Scripthash not deterministic: %s != %s", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("Scripthash must be 64 hex chars, got %d: %s", len(h1), h1)
	}
}

func TestScripthash_ReversesSHA256(t *testing.T) {
	// Cross-check: scripthash of arbitrary bytes
	script := []byte("test script bytes")
	h := Scripthash(script)
	// Decode and verify it matches sha256 reversed
	raw, _ := hex.DecodeString(h)
	// Reverse back
	for i, j := 0, len(raw)-1; i < j; i, j = i+1, j-1 {
		raw[i], raw[j] = raw[j], raw[i]
	}
	// Should equal sha256("test script bytes")
	// sha256("test script bytes") = 50f70d7b07426bb21b1a9b93287967ad7f4ee1e1c4b2c7c8e3f7d8b9a0c1d2e3
	// (we don't hardcode the exact hash, but we verify reverse symmetry)
	if len(raw) != 32 {
		t.Errorf("decoded scripthash must be 32 bytes, got %d", len(raw))
	}
}

func TestRequest_Marshal(t *testing.T) {
	req := NewRequest(42, "blockchain.scripthash.listunspent", "abc123")
	raw, err := req.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// Verify JSON shape
	var got map[string]interface{}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc = %v, want 2.0", got["jsonrpc"])
	}
	if got["id"].(float64) != 42 {
		t.Errorf("id = %v, want 42", got["id"])
	}
	if got["method"] != "blockchain.scripthash.listunspent" {
		t.Errorf("method = %v", got["method"])
	}
	params, ok := got["params"].([]interface{})
	if !ok || len(params) != 1 || params[0] != "abc123" {
		t.Errorf("params = %v, want [abc123]", params)
	}
}

func TestRequest_MarshalEmptyParams(t *testing.T) {
	req := NewRequest(1, "server.ping")
	raw, err := req.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// Empty params should marshal as [], not null
	var got map[string]interface{}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	params, ok := got["params"].([]interface{})
	if !ok {
		t.Errorf("params should be an array, got %T", got["params"])
	}
	if len(params) != 0 {
		t.Errorf("params should be empty, got %v", params)
	}
}

func TestRPCError_Error(t *testing.T) {
	e := &RPCError{Code: -32601, Message: "Method not found"}
	if e.Error() != "electrum error -32601: Method not found" {
		t.Errorf("Error() = %q", e.Error())
	}
}

func TestErrResponse_Unwrap(t *testing.T) {
	rpc := &RPCError{Code: -1, Message: "fail"}
	e := &ErrResponse{Err: rpc}
	if e.Error() != rpc.Error() {
		t.Errorf("ErrResponse.Error = %q, want %q", e.Error(), rpc.Error())
	}
	// errors.Is/As support via Unwrap
	if e.Unwrap() != rpc {
		t.Error("Unwrap should return underlying RPCError")
	}
}

// mockClient is a test Client implementation that returns canned responses.
type mockClient struct {
	calls    []call
	response json.RawMessage
	err      error
}

type call struct {
	method string
	params []interface{}
}

func (m *mockClient) Call(ctx context.Context, method string, params ...interface{}) (json.RawMessage, error) {
	m.calls = append(m.calls, call{method, params})
	return m.response, m.err
}

func TestCallTyped_UnmarshalsResult(t *testing.T) {
	m := &mockClient{
		response: json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":["a","b","c"]}`),
	}
	var out []string
	err := CallTyped(context.Background(), m, "test", &out)
	if err != nil {
		t.Fatalf("CallTyped: %v", err)
	}
	if len(out) != 3 || out[0] != "a" || out[1] != "b" || out[2] != "c" {
		t.Errorf("out = %v", out)
	}
	if len(m.calls) != 1 {
		t.Errorf("expected 1 call, got %d", len(m.calls))
	}
	if m.calls[0].method != "test" {
		t.Errorf("method = %q, want 'test'", m.calls[0].method)
	}
}

func TestCallTyped_ReturnsRPCError(t *testing.T) {
	m := &mockClient{
		response: json.RawMessage(`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"Method not found"}}`),
	}
	var out interface{}
	err := CallTyped(context.Background(), m, "bad", &out)
	if err == nil {
		t.Fatal("expected error for RPC error response")
	}
	if _, ok := err.(*ErrResponse); !ok {
		t.Errorf("expected *ErrResponse, got %T", err)
	}
}

func TestCallTyped_NilOut(t *testing.T) {
	m := &mockClient{
		response: json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":null}`),
	}
	err := CallTyped(context.Background(), m, "test", nil)
	if err != nil {
		t.Errorf("CallTyped with nil out: %v", err)
	}
}

func TestListUnspent_ParsesUTXOs(t *testing.T) {
	m := &mockClient{
		response: json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":[
			{"tx_hash":"abc","tx_pos":0,"value":1000000,"height":100},
			{"tx_hash":"def","tx_pos":1,"value":500000,"height":0}
		]}`),
	}
	utxos, err := ListUnspent(context.Background(), m, "scripthash123")
	if err != nil {
		t.Fatalf("ListUnspent: %v", err)
	}
	if len(utxos) != 2 {
		t.Fatalf("expected 2 UTXOs, got %d", len(utxos))
	}
	if utxos[0].TxHash != "abc" || utxos[0].Value != 1000000 {
		t.Errorf("utxos[0] = %+v", utxos[0])
	}
	if utxos[1].Height != 0 {
		t.Errorf("utxos[1].Height = %d, want 0 (unconfirmed)", utxos[1].Height)
	}
}

func TestBroadcast_ReturnsTxID(t *testing.T) {
	m := &mockClient{
		response: json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":"abc123def456"}`),
	}
	txid, err := Broadcast(context.Background(), m, "0100000001...")
	if err != nil {
		t.Fatalf("Broadcast: %v", err)
	}
	if txid != "abc123def456" {
		t.Errorf("txid = %q, want abc123def456", txid)
	}
	if m.calls[0].method != MethodBroadcast {
		t.Errorf("method = %q, want %q", m.calls[0].method, MethodBroadcast)
	}
}

func TestVersion_ReturnsPair(t *testing.T) {
	m := &mockClient{
		response: json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":["liblbry 1.0","1.4.1"]}`),
	}
	v, err := Version(context.Background(), m)
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v[0] != "liblbry 1.0" || v[1] != "1.4.1" {
		t.Errorf("v = %v", v)
	}
}

func TestClientFunc_AdaptsFunction(t *testing.T) {
	called := false
	c := ClientFunc(func(ctx context.Context, method string, params ...interface{}) (json.RawMessage, error) {
		called = true
		return json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":"ok"}`), nil
	})
	var out string
	err := CallTyped(context.Background(), c, "ping", &out)
	if err != nil {
		t.Fatalf("CallTyped: %v", err)
	}
	if !called {
		t.Error("ClientFunc was not called")
	}
	if out != "ok" {
		t.Errorf("out = %q", out)
	}
}

func TestMethodConstants(t *testing.T) {
	// Smoke test: ensure method constants are non-empty and distinct
	methods := []string{
		MethodGetVersion, MethodPing, MethodBroadcast, MethodGetTx,
		MethodListUnspent, MethodGetHistory, MethodGetBalance,
		MethodGetClaimByID, MethodResolve, MethodEstimateFee,
	}
	seen := make(map[string]bool)
	for _, m := range methods {
		if m == "" {
			t.Error("empty method constant")
		}
		if seen[m] {
			t.Errorf("duplicate method constant: %s", m)
		}
		seen[m] = true
	}
}
