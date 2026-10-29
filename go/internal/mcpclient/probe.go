// Package mcpclient speaks enough of the Model Context Protocol to measure a
// server: initialize, then tools/list, resources/list and prompts/list. It
// covers the three transports synca stores — streamable HTTP, legacy SSE and
// stdio commands — and estimates context cost from the serialized definitions.
// The URLs, commands and credentials all come from synca's own config files,
// which is the same reach the user's coding agents already have.
package mcpclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// DefaultTimeout bounds one probe when Spec.Timeout is zero.
const DefaultTimeout = 15 * time.Second

// maxDefinitions caps the serialized definitions one probe accumulates, so a
// server that answers tools/list forever cannot exhaust memory.
const maxDefinitions = 32 << 20

// maxPages caps tools/list pagination.
const maxPages = 64

// protocolNew is requested first; protocolLegacy is one retry for servers
// that predate it.
const (
	protocolNew    = "2025-06-18"
	protocolLegacy = "2024-11-05"
)

// Spec selects the server to probe. Transport is "http", "sse" or "stdio";
// headers and env carry the configured credentials and are never echoed back.
type Spec struct {
	Transport string
	Command   []string
	URL       string
	Headers   map[string]string
	Env       map[string]string
	Timeout   time.Duration
	UserAgent string
	Version   string
}

// Result reports what one probe learned. Tokens is the estimated context
// cost: every definition serialized as JSON, bytes / 4.
type Result struct {
	OK            bool
	Err           string
	Protocol      string
	ServerName    string
	ServerVersion string
	Tools         int
	Resources     int
	Prompts       int
	Tokens        int
}

// Probe connects to the server, lists its tools, resources and prompts, and
// estimates the tokens a client spends on the definitions. A JSON-RPC error
// on resources or prompts is tolerated; only initialize and tools/list
// decide OK.
func Probe(ctx context.Context, spec Spec) Result {
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	res := probeWith(ctx, spec, protocolNew)
	if res.OK {
		return res
	}
	// Old servers answer a modern initialize with an error; retry once with
	// the 2024-11-05 protocol before reporting the failure.
	if res2 := probeWith(ctx, spec, protocolLegacy); res2.OK {
		return res2
	}
	return res
}

func probeWith(ctx context.Context, spec Spec, protocol string) Result {
	sess, err := dial(ctx, spec, protocol)
	if err != nil {
		return Result{Err: err.Error()}
	}
	defer sess.close()
	return runProbe(ctx, sess)
}

// runProbe runs initialize → lists over one open session.
func runProbe(ctx context.Context, sess *session) Result {
	var out Result
	init, err := sess.call(ctx, "initialize", map[string]any{
		"protocolVersion": sess.protocol,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]string{
			"name":    "synca",
			"version": sess.version,
		},
	})
	if err != nil {
		out.Err = err.Error()
		return out
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(init, &raw); err != nil {
		out.Err = "initialize result is not an object"
		return out
	}
	var doc struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(init, &doc); err != nil {
		out.Err = "initialize result is unreadable: " + err.Error()
		return out
	}
	out.Protocol = doc.ProtocolVersion
	out.ServerName = doc.ServerInfo.Name
	out.ServerVersion = doc.ServerInfo.Version
	sess.negotiated = doc.ProtocolVersion

	if err := sess.notify(ctx, "notifications/initialized", nil); err != nil {
		out.Err = "initialized notification failed: " + err.Error()
		return out
	}

	var caps struct {
		Tools     *json.RawMessage `json:"tools"`
		Resources *json.RawMessage `json:"resources"`
		Prompts   *json.RawMessage `json:"prompts"`
	}
	if capsRaw, ok := raw["capabilities"]; ok {
		_ = json.Unmarshal(capsRaw, &caps)
	}

	toolBytes, err := sess.listAll(ctx, "tools/list", "tools", &out.Tools, caps.Tools != nil)
	if err != nil {
		out.Err = err.Error()
		return out
	}
	resBytes, resErr := sess.listAll(ctx, "resources/list", "resources", &out.Resources, caps.Resources != nil)
	promBytes, promErr := sess.listAll(ctx, "prompts/list", "prompts", &out.Prompts, caps.Prompts != nil)
	out.Tokens = (toolBytes + resBytes + promBytes) / 4
	out.OK = true
	// A server that declared the capability but failed the call still counts;
	// the failure is noted so the estimate is visibly incomplete.
	var notes []string
	if resErr != nil && !errors.Is(resErr, errMethodNotFound) {
		notes = append(notes, "resources: "+resErr.Error())
	}
	if promErr != nil && !errors.Is(promErr, errMethodNotFound) {
		notes = append(notes, "prompts: "+promErr.Error())
	}
	out.Err = strings.Join(notes, "; ")
	return out
}

var errMethodNotFound = errors.New("method not found")

// session wraps a transport with the initialize bookkeeping: the first call
// uses the configured protocol version and every later call the negotiated
// one, for transports that carry it as a header.
type session struct {
	sess       transport
	protocol   string
	version    string
	nextID     uint64
	negotiated string
}

func (s *session) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	s.nextID++
	id := s.nextID
	payload, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	return s.sess.call(ctx, id, s.protocolVersion(), payload)
}

func (s *session) notify(ctx context.Context, method string, params any) error {
	payload, err := json.Marshal(rpcNotification{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return err
	}
	return s.sess.post(ctx, payload)
}

func (s *session) protocolVersion() string {
	if s.negotiated != "" {
		return s.negotiated
	}
	return s.protocol
}

// listAll pages one list method and returns the serialized bytes of every
// definition. A missing capability or a -32601 answer counts as empty.
func (s *session) listAll(ctx context.Context, method, field string, count *int, declared bool) (int, error) {
	if !declared {
		return 0, nil
	}
	total := 0
	cursor := ""
	for page := 0; ; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := s.call(ctx, method, params)
		if err != nil {
			var rpc *rpcError
			if errors.As(err, &rpc) && rpc.Code == -32601 {
				return total, errMethodNotFound
			}
			return total, fmt.Errorf("%s failed: %w", method, err)
		}
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(raw, &doc); err != nil {
			return total, fmt.Errorf("%s result is not an object", method)
		}
		if items, ok := doc[field]; ok && !bytes.Equal(bytes.TrimSpace(items), []byte("null")) {
			var list []json.RawMessage
			if err := json.Unmarshal(items, &list); err != nil {
				return total, fmt.Errorf("%s %s is not an array", method, field)
			}
			for _, item := range list {
				total += len(item)
				*count++
				if total > maxDefinitions {
					return total, fmt.Errorf("%s definitions exceed %d bytes", method, maxDefinitions)
				}
			}
		}
		if page >= maxPages {
			return total, fmt.Errorf("%s did not finish within %d pages", method, maxPages)
		}
		var next string
		if len(doc["nextCursor"]) > 0 && json.Unmarshal(doc["nextCursor"], &next) == nil && next != "" {
			cursor = next
			continue
		}
		return total, nil
	}
}

// ---------- JSON-RPC shapes ----------

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *uint64         `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("JSON-RPC %d: %s", e.Code, e.Message)
}

// transport is one wired-up connection. call sends a request payload and
// returns the result bytes of the response that answers id; post sends a
// notification.
type transport interface {
	call(ctx context.Context, id uint64, protocolVersion string, payload []byte) (json.RawMessage, error)
	post(ctx context.Context, payload []byte) error
	close()
}

func dial(ctx context.Context, spec Spec, protocol string) (*session, error) {
	s := &session{protocol: protocol, version: spec.Version}
	switch spec.Transport {
	case "http":
		t, err := newHTTPTransport(spec)
		if err != nil {
			return nil, err
		}
		s.sess = t
		return s, nil
	case "sse":
		t, err := newSSETransport(ctx, spec)
		if err != nil {
			// Servers registered as sse that migrated to streamable HTTP still
			// answer a POST; fall back before giving up.
			t2, httpErr := newHTTPTransport(spec)
			if httpErr != nil {
				return nil, err
			}
			s.sess = t2
			return s, nil
		}
		s.sess = t
		return s, nil
	case "stdio":
		t, err := newStdioTransport(ctx, spec)
		if err != nil {
			return nil, err
		}
		s.sess = t
		return s, nil
	default:
		return nil, fmt.Errorf("unsupported transport %q", spec.Transport)
	}
}

func (s *session) close() {
	if s.sess != nil {
		s.sess.close()
	}
}

// ---------- streamable HTTP ----------

type httpTransport struct {
	url       string
	spec      Spec
	client    *http.Client
	sessionID string
}

func newHTTPTransport(spec Spec) (*httpTransport, error) {
	u, err := url.Parse(spec.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("%q is not an http(s) URL", redactURL(spec.URL))
	}
	return &httpTransport{url: spec.URL, spec: spec, client: probeHTTPClient()}, nil
}

func probeHTTPClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Host != via[0].URL.Host {
				return errors.New("refusing a cross-host redirect")
			}
			return nil
		},
	}
}

func (t *httpTransport) applyHeaders(req *http.Request, protocolVersion, accept string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", accept)
	if protocolVersion != "" {
		req.Header.Set("MCP-Protocol-Version", protocolVersion)
	}
	if t.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", t.sessionID)
	}
	if t.spec.UserAgent != "" {
		req.Header.Set("User-Agent", t.spec.UserAgent)
	}
	for name, value := range t.spec.Headers {
		req.Header.Set(name, value)
	}
}

func (t *httpTransport) call(ctx context.Context, id uint64, protocolVersion string, payload []byte) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	t.applyHeaders(req, protocolVersion, "application/json, text/event-stream")
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s is unreachable: %s", redactURL(t.url), redactURLErr(err))
	}
	defer resp.Body.Close()
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.sessionID = sid
	}
	if resp.StatusCode == http.StatusAccepted {
		return nil, fmt.Errorf("%s accepted the request without a response", redactURL(t.url))
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("%s answered %d: %s", redactURL(t.url), resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return readRPCResponse(resp.Body, resp.Header.Get("Content-Type"), id)
}

func (t *httpTransport) post(ctx context.Context, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	t.applyHeaders(req, "", "application/json, text/event-stream")
	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s is unreachable: %s", redactURL(t.url), redactURLErr(err))
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s rejected the notification with %d", redactURL(t.url), resp.StatusCode)
	}
	return nil
}

func (t *httpTransport) close() {
	if t.sessionID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, t.url, nil)
	if err != nil {
		return
	}
	t.applyHeaders(req, "", "application/json")
	if resp, err := t.client.Do(req); err == nil {
		resp.Body.Close()
	}
}

// ---------- shared response reading ----------

// readRPCResponse reads the JSON-RPC response for id from a JSON or SSE body.
func readRPCResponse(r io.Reader, contentType string, id uint64) (json.RawMessage, error) {
	if strings.Contains(contentType, "text/event-stream") {
		return responseFromSSE(r, id)
	}
	payload, err := io.ReadAll(io.LimitReader(r, maxDefinitions+1))
	if err != nil {
		return nil, fmt.Errorf("reading the response failed: %w", err)
	}
	if len(payload) > maxDefinitions {
		return nil, errors.New("response is too large")
	}
	return decodeResponse(payload, id)
}

// responseFromSSE scans an SSE stream for the JSON-RPC response with id.
func responseFromSSE(r io.Reader, id uint64) (json.RawMessage, error) {
	sse := newSSEReader(r)
	for {
		ev, err := sse.next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("the stream ended before the response to request %d", id)
			}
			return nil, err
		}
		if ev.data == "" {
			continue
		}
		raw, ok, err := matchResponse([]byte(ev.data), id)
		if err != nil {
			return nil, err
		}
		if ok {
			return raw, nil
		}
	}
}

func decodeResponse(payload []byte, id uint64) (json.RawMessage, error) {
	raw, ok, err := matchResponse(payload, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("the response does not answer request %d", id)
	}
	return raw, nil
}

// matchResponse decodes one JSON-RPC payload and reports whether it answers
// id. When it does and carries an error, that error is returned.
func matchResponse(payload []byte, id uint64) (json.RawMessage, bool, error) {
	var resp rpcResponse
	if err := json.Unmarshal(bytes.TrimSpace(payload), &resp); err != nil {
		return nil, false, fmt.Errorf("the answer is not JSON-RPC: %s", firstLine(payload))
	}
	if resp.ID == nil || *resp.ID != id {
		return nil, false, nil
	}
	if resp.Error != nil {
		return nil, false, resp.Error
	}
	return resp.Result, true, nil
}

func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// ---------- SSE reader ----------

type sseEvent struct {
	name string
	data string
}

// sseReader is a minimal Server-Sent-Events reader: event name plus data for
// each blank-line-delimited event, ignoring comments and heartbeats.
type sseReader struct {
	scanner *bufio.Scanner
}

func newSSEReader(r io.Reader) *sseReader {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 8<<20)
	return &sseReader{scanner: sc}
}

func (s *sseReader) next() (sseEvent, error) {
	ev := sseEvent{}
	var data strings.Builder
	sawAny := false
	for s.scanner.Scan() {
		line := strings.TrimRight(s.scanner.Text(), "\r")
		sawAny = true
		switch {
		case line == "":
			if data.Len() > 0 || ev.name != "" {
				ev.data = strings.TrimSuffix(data.String(), "\n")
				return ev, nil
			}
			ev.name = ""
		case strings.HasPrefix(line, ":"):
			// comment or heartbeat
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			data.WriteString("\n")
		case strings.HasPrefix(line, "event:"):
			ev.name = strings.TrimPrefix(strings.TrimPrefix(line, "event:"), " ")
		default:
			// id:, retry:, or a malformed line — ignored for probing.
		}
	}
	if !sawAny {
		return ev, io.EOF
	}
	if data.Len() > 0 || ev.name != "" {
		ev.data = strings.TrimSuffix(data.String(), "\n")
		return ev, nil
	}
	return ev, io.EOF
}

// ---------- legacy SSE transport ----------

type sseTransport struct {
	spec      Spec
	endpoint  string
	client    *http.Client
	replies   chan *rpcResponse
	sseCancel context.CancelFunc
	done      chan struct{}
	errMu     sync.Mutex
	err       error // set by the reader before it exits
}

func newSSETransport(ctx context.Context, spec Spec) (*sseTransport, error) {
	u, err := url.Parse(spec.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("%q is not an http(s) URL", redactURL(spec.URL))
	}
	streamCtx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, spec.URL, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	if spec.UserAgent != "" {
		req.Header.Set("User-Agent", spec.UserAgent)
	}
	for name, value := range spec.Headers {
		req.Header.Set(name, value)
	}
	resp, err := probeHTTPClient().Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("%s is unreachable: %s", redactURL(spec.URL), redactURLErr(err))
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("%s answered %d opening the SSE stream", redactURL(spec.URL), resp.StatusCode)
	}

	t := &sseTransport{
		spec:      spec,
		client:    probeHTTPClient(),
		replies:   make(chan *rpcResponse, 16),
		sseCancel: cancel,
		done:      make(chan struct{}),
	}
	endpoint := make(chan string, 1)
	go t.read(resp.Body, endpoint)
	select {
	case ep := <-endpoint:
		t.endpoint = ep
	case <-time.After(10 * time.Second):
		t.close()
		return nil, fmt.Errorf("%s never sent an SSE endpoint event", redactURL(spec.URL))
	case <-ctx.Done():
		t.close()
		return nil, ctx.Err()
	}
	return t, nil
}

func (t *sseTransport) read(body io.Reader, endpoint chan string) {
	defer close(t.done)
	defer t.sseCancel()
	sse := newSSEReader(body)
	for {
		ev, err := sse.next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				t.errMu.Lock()
				t.err = err
				t.errMu.Unlock()
			}
			return
		}
		if ev.data == "" {
			continue
		}
		if ev.name == "endpoint" {
			select {
			case endpoint <- t.resolve(ev.data):
			default:
			}
			continue
		}
		var resp rpcResponse
		if err := json.Unmarshal([]byte(ev.data), &resp); err != nil {
			continue
		}
		select {
		case t.replies <- &resp:
		default:
		}
	}
}

func (t *sseTransport) resolve(ref string) string {
	base, err := url.Parse(t.spec.URL)
	if err != nil {
		return ref
	}
	got, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return base.ResolveReference(got).String()
}

func (t *sseTransport) post(ctx context.Context, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if t.spec.UserAgent != "" {
		req.Header.Set("User-Agent", t.spec.UserAgent)
	}
	for name, value := range t.spec.Headers {
		req.Header.Set(name, value)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s is unreachable: %s", redactURL(t.endpoint), redactURLErr(err))
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s answered %d", redactURL(t.endpoint), resp.StatusCode)
	}
	return nil
}

func (t *sseTransport) call(ctx context.Context, id uint64, _ string, payload []byte) (json.RawMessage, error) {
	if err := t.post(ctx, payload); err != nil {
		return nil, err
	}
	for {
		select {
		case resp := <-t.replies:
			if resp.ID == nil || *resp.ID != id {
				continue
			}
			if resp.Error != nil {
				return nil, resp.Error
			}
			return resp.Result, nil
		case <-t.done:
			t.errMu.Lock()
			defer t.errMu.Unlock()
			if t.err != nil {
				return nil, t.err
			}
			return nil, errors.New("the SSE stream closed before the response arrived")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (t *sseTransport) close() {
	t.sseCancel()
}

// ---------- stdio ----------

type stdioTransport struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	replies chan *rpcResponse
	closed  sync.Once
	wait    sync.Once
}

func newStdioTransport(ctx context.Context, spec Spec) (*stdioTransport, error) {
	if len(spec.Command) == 0 {
		return nil, errors.New("the server has no command to run")
	}
	cmd := exec.CommandContext(ctx, spec.Command[0], spec.Command[1:]...)
	cmd.Env = os.Environ()
	for key, value := range spec.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s failed: %w", spec.Command[0], err)
	}
	t := &stdioTransport{cmd: cmd, stdin: stdin, replies: make(chan *rpcResponse, 16)}
	go t.read(stdout)
	return t, nil
}

func (t *stdioTransport) read(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), maxDefinitions)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var resp rpcResponse
		if err := json.Unmarshal(line, &resp); err != nil || resp.ID == nil {
			continue
		}
		select {
		case t.replies <- &resp:
		default:
		}
	}
}

func (t *stdioTransport) call(ctx context.Context, id uint64, _ string, payload []byte) (json.RawMessage, error) {
	if _, err := t.stdin.Write(append(payload, '\n')); err != nil {
		return nil, fmt.Errorf("the server stopped reading input: %w", err)
	}
	for {
		select {
		case resp := <-t.replies:
			if resp.ID == nil || *resp.ID != id {
				continue
			}
			if resp.Error != nil {
				return nil, resp.Error
			}
			return resp.Result, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (t *stdioTransport) post(_ context.Context, payload []byte) error {
	_, err := t.stdin.Write(append(payload, '\n'))
	return err
}

func (t *stdioTransport) close() {
	t.closed.Do(func() {
		_ = t.stdin.Close()
		done := make(chan struct{})
		go func() {
			t.wait.Do(func() { _ = t.cmd.Wait() })
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			if t.cmd.Process != nil {
				_ = t.cmd.Process.Kill()
			}
			<-done
		}
	})
}

// ---------- helpers ----------

func redactURLErr(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	return err.Error()
}

// redactURL hides userinfo before a URL lands in an error message.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = url.User("••••••")
	return u.String()
}
