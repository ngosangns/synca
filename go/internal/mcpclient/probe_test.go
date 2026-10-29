package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

const initResult = `{"protocolVersion":"2025-06-18","capabilities":{"tools":{"listChanged":false},"resources":{},"prompts":{}},"serverInfo":{"name":"fixture","version":"1.2.3"}}`

func toolsResult(tools ...string) string {
	return fmt.Sprintf(`{"tools":[%s]}`, strings.Join(tools, ","))
}

const toolA = `{"name":"alpha","description":"First tool","inputSchema":{"type":"object","properties":{"x":{"type":"number"}}}}`
const toolB = `{"name":"beta","description":"Second tool","inputSchema":{"type":"object"}}`
const resourceR = `{"uri":"file:///tmp/a.txt","name":"a.txt","description":"A file"}`

// jsonRPCHandler answers initialize and list calls for a streamable HTTP
// server. sseBody wraps replies in an SSE stream when set.
type jsonRPCHandler struct {
	t          *testing.T
	answers    map[string]string // method → result JSON
	sse        bool
	sessionID  string
	sawHeaders []string
}

func (h *jsonRPCHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID     *uint64  `json:"id"`
		Method string   `json:"method"`
		Params tcParams `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	if req.ID == nil {
		// A notification expects no reply.
		w.WriteHeader(http.StatusAccepted)
		return
	}
	h.sawHeaders = append(h.sawHeaders, r.Header.Get("Mcp-Session-Id"))
	if h.sessionID != "" {
		w.Header().Set("Mcp-Session-Id", h.sessionID)
	}
	result, ok := h.answers[req.Method]
	if !ok {
		writeRPC(w, h.sse, req.ID, ``, &rpcError{Code: -32601, Message: "no such method"})
		return
	}
	writeRPC(w, h.sse, req.ID, result, nil)
}

type tcParams struct{}

func writeRPC(w http.ResponseWriter, sse bool, id *uint64, result string, rpcErr *rpcError) {
	var payload string
	if rpcErr != nil {
		payload = fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"error":{"code":%d,"message":%q}}`, *id, rpcErr.Code, rpcErr.Message)
	} else {
		payload = fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":%s}`, *id, result)
	}
	if !sse {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(payload))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprintf(w, "event: message\ndata: %s\n\n", payload)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func resultOk(t *testing.T, res Result) {
	t.Helper()
	if !res.OK {
		t.Fatalf("probe failed: %s", res.Err)
	}
}

func TestProbeStreamableHTTPJSON(t *testing.T) {
	handler := &jsonRPCHandler{
		t:       t,
		answers: map[string]string{"initialize": initResult, "tools/list": toolsResult(toolA, toolB), "resources/list": `{"resources":[` + resourceR + `]}`},
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	res := Probe(context.Background(), Spec{Transport: "http", URL: server.URL, Version: "0.4.0"})
	resultOk(t, res)
	if res.Tools != 2 || res.Resources != 1 || res.Prompts != 0 {
		t.Fatalf("counts %#v", res)
	}
	if res.ServerName != "fixture" || res.ServerVersion != "1.2.3" || res.Protocol != "2025-06-18" {
		t.Fatalf("server info %#v", res)
	}
	want := (len(toolA) + len(toolB) + len(resourceR)) / 4
	if res.Tokens != want {
		t.Fatalf("tokens %d, want %d", res.Tokens, want)
	}
}

func TestProbeStreamableHTTPSSE(t *testing.T) {
	handler := &jsonRPCHandler{
		t:         t,
		answers:   map[string]string{"initialize": initResult, "tools/list": toolsResult(toolA)},
		sse:       true,
		sessionID: "sid-1",
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	res := Probe(context.Background(), Spec{Transport: "http", URL: server.URL})
	resultOk(t, res)
	if res.Tools != 1 || res.Tokens != len(toolA)/4 {
		t.Fatalf("result %#v", res)
	}
	for i, got := range handler.sawHeaders[1:] {
		if got != "sid-1" {
			t.Fatalf("call %d session header %q", i+1, got)
		}
	}
}

func TestProbeMethodNotFoundLists(t *testing.T) {
	handler := &jsonRPCHandler{
		t:       t,
		answers: map[string]string{"initialize": initResult, "tools/list": toolsResult(toolB)},
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	res := Probe(context.Background(), Spec{Transport: "http", URL: server.URL})
	resultOk(t, res)
	if res.Resources != 0 || res.Prompts != 0 {
		t.Fatalf("counts %#v", res)
	}
}

func TestProbeLegacySSE(t *testing.T) {
	requests := make(chan uint64, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "text/event-stream")
			rc := http.NewResponseController(w)
			fmt.Fprint(w, "event: endpoint\ndata: /messages\n\n")
			rc.Flush()
			for {
				select {
				case id := <-requests:
					// Only tools/list responses after initialize.
					result := initResult
					if id != 1 {
						result = toolsResult(toolA, toolB)
					}
					fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":%s}\n\n", id, result)
					rc.Flush()
				case <-r.Context().Done():
					return
				}
			}
		case http.MethodPost:
			var req struct {
				ID     uint64 `json:"id"`
				Method string `json:"method"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad body", http.StatusBadRequest)
				return
			}
			requests <- req.ID
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer server.Close()

	res := Probe(context.Background(), Spec{Transport: "sse", URL: server.URL})
	resultOk(t, res)
	if res.Tools != 2 {
		t.Fatalf("tools %d", res.Tools)
	}
}

func TestProbeLegacySSEFallsBackToHTTP(t *testing.T) {
	// The server speaks streamable HTTP only: GET fails, POST answers.
	handler := &jsonRPCHandler{
		t:       t,
		answers: map[string]string{"initialize": initResult, "tools/list": toolsResult(toolA)},
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	res := Probe(context.Background(), Spec{Transport: "sse", URL: server.URL})
	resultOk(t, res)
	if res.Tools != 1 {
		t.Fatalf("tools %d", res.Tools)
	}
}

func TestProbeStdio(t *testing.T) {
	res := Probe(context.Background(), Spec{
		Transport: "stdio",
		Command:   []string{os.Args[0], "-test.run=TestStdioHelperProcess", "--"},
		Env:       map[string]string{"SYNCA_STDIO_HELPER": "1"},
		Version:   "0.4.0",
	})
	resultOk(t, res)
	if res.Tools != 2 || res.ServerName != "fixture" {
		t.Fatalf("result %#v", res)
	}
	if res.Tokens != (len(toolA)+len(toolB))/4 {
		t.Fatalf("tokens %d", res.Tokens)
	}
}

// TestStdioHelperProcess is the child process for TestProbeStdio: a tiny MCP
// server over stdin/stdout that answers every request with the fixture.
func TestStdioHelperProcess(t *testing.T) {
	if os.Getenv("SYNCA_STDIO_HELPER") != "1" {
		return
	}
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	first := true
	for {
		var line map[string]any
		if err := dec.Decode(&line); err != nil {
			os.Exit(0)
		}
		id, ok := line["id"].(float64)
		if !ok {
			continue
		}
		result := initResult
		if !first {
			result = toolsResult(toolA, toolB)
		}
		first = false
		_ = enc.Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"result":  json.RawMessage(result),
		})
	}
}

func TestProbeFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	cases := []struct {
		name string
		spec Spec
	}{
		{"http 500", Spec{Transport: "http", URL: server.URL}},
		{"http refused", Spec{Transport: "http", URL: "http://127.0.0.1:1/mcp"}},
		{"sse no endpoint", Spec{Transport: "sse", URL: "http://127.0.0.1:1/mcp"}},
		{"stdio missing", Spec{Transport: "stdio", Command: []string{"synca-no-such-binary-xyz"}}},
		{"unknown transport", Spec{Transport: "carrier", URL: "https://example.com"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := Probe(context.Background(), tc.spec)
			if res.OK {
				t.Fatalf("expected failure, got %#v", res)
			}
			if res.Err == "" {
				t.Fatal("empty error")
			}
		})
	}
}

func TestProbeTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The probe times out at 150ms; a disconnected client that never
		// receives a write is only noticed here, so self-release shortly after.
		select {
		case <-r.Context().Done():
		case <-time.After(500 * time.Millisecond):
		}
	}))
	defer server.Close()

	res := Probe(context.Background(), Spec{Transport: "http", URL: server.URL, Timeout: 150 * time.Millisecond})
	if res.OK || res.Err == "" {
		t.Fatalf("expected timeout failure, got %#v", res)
	}
}

func TestSSEReader(t *testing.T) {
	stream := ": ping\nevent: endpoint\ndata: /messages?sid=1\r\n\r\n" +
		"event: message\ndata: {\"a\":1}\ndata: {\"b\":2}\n\n" +
		"data: tail\n"
	sse := newSSEReader(strings.NewReader(stream))
	first, err := sse.next()
	if err != nil || first.name != "endpoint" || first.data != "/messages?sid=1" {
		t.Fatalf("first %#v %v", first, err)
	}
	second, err := sse.next()
	if err != nil || second.data != "{\"a\":1}\n{\"b\":2}" {
		t.Fatalf("second %#v %v", second, err)
	}
	third, err := sse.next()
	if err != nil || third.data != "tail" {
		t.Fatalf("third %#v %v", third, err)
	}
	if _, err := sse.next(); err == nil {
		t.Fatal("expected EOF")
	}
}
