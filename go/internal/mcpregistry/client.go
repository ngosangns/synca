package mcpregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// DefaultBase is the official MCP Registry.
	DefaultBase = "https://registry.modelcontextprotocol.io"
	// DefaultLimit is the page size when --limit is not given.
	DefaultLimit = 30
	// MaxLimit is the largest page size the registry accepts.
	MaxLimit = 100

	maxBody = 32 << 20
)

// Kind classifies a failure so the CLI can pick an exit code.
type Kind int

const (
	KindUsage Kind = iota + 1
	KindUnauthorized
	KindNotFound
	KindUnavailable
)

// Error is an MCP registry failure.
type Error struct {
	Kind Kind
	Msg  string
	Hint string
}

func (e *Error) Error() string { return e.Msg }

func usageErr(format string, args ...any) error {
	return &Error{Kind: KindUsage, Msg: fmt.Sprintf(format, args...)}
}

// Client talks to an MCP registry. A zero Token means read-only, unauthenticated
// requests, which is what the official registry serves.
type Client struct {
	BaseURL   string
	Token     string
	HTTP      *http.Client
	UserAgent string
}

// New builds a client from MCP_REGISTRY_URL and MCP_REGISTRY_TOKEN.
func New(version string) *Client {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("MCP_REGISTRY_URL")), "/")
	if base == "" {
		base = DefaultBase
	}
	return &Client{
		BaseURL: base,
		Token:   strings.TrimSpace(os.Getenv("MCP_REGISTRY_TOKEN")),
		HTTP: &http.Client{
			Timeout: 20 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("too many redirects")
				}
				if req.URL.Host != via[0].URL.Host {
					return errors.New("refusing a cross-host redirect")
				}
				return nil
			},
		},
		UserAgent: "synca/" + version,
	}
}

// ListOptions selects one page of servers.
type ListOptions struct {
	Cursor  string
	Limit   int
	Search  string
	Version string
}

var (
	serverNameRe = regexp.MustCompile(`^[A-Za-z0-9.-]+/[A-Za-z0-9._-]+$`)
)

// ValidateServerName checks the reverse-DNS registry name shape.
func ValidateServerName(name string) error {
	if !serverNameRe.MatchString(name) {
		return usageErr("invalid server name %q (use namespace/name)", name)
	}
	return nil
}

// ValidateVersion checks a registry version string.
func ValidateVersion(version string) error {
	if strings.TrimSpace(version) == "" {
		return usageErr("server version must not be empty")
	}
	if utf8.RuneCountInString(version) > 255 {
		return usageErr("server version is longer than 255 characters")
	}
	if strings.ContainsAny(version, "\r\n\x00") {
		return usageErr("server version contains a control character")
	}
	return nil
}

func (c *Client) pageOptions(opts ListOptions) (url.Values, int, error) {
	limit := opts.Limit
	if limit == 0 {
		limit = DefaultLimit
	}
	if limit < 1 || limit > MaxLimit {
		return nil, 0, usageErr("--limit must be between 1 and %d", MaxLimit)
	}
	version := strings.TrimSpace(opts.Version)
	if version == "" {
		version = "latest"
	}
	if err := ValidateVersion(version); err != nil {
		return nil, 0, err
	}
	q := url.Values{
		"limit":   {fmt.Sprint(limit)},
		"version": {version},
	}
	if cursor := strings.TrimSpace(opts.Cursor); cursor != "" {
		q.Set("cursor", cursor)
	}
	return q, limit, nil
}

// List reads one page of servers. Version defaults to latest so search and
// browse do not return superseded versions.
func (c *Client) List(ctx context.Context, opts ListOptions) (*ListResponse, error) {
	q, _, err := c.pageOptions(opts)
	if err != nil {
		return nil, err
	}
	if search := strings.TrimSpace(opts.Search); search != "" {
		q.Set("search", search)
	}
	var out ListResponse
	if err := c.get(ctx, "/v0.1/servers", q, &out); err != nil {
		return nil, err
	}
	if out.Servers == nil {
		out.Servers = []ServerRecord{}
	}
	return &out, nil
}

// Search finds servers by name substring. The registry searches names only.
func (c *Client) Search(ctx context.Context, query string, opts ListOptions) (*ListResponse, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, usageErr("search query must not be empty")
	}
	opts.Search = query
	return c.List(ctx, opts)
}

// Info reads one server version. An empty version means the latest one.
func (c *Client) Info(ctx context.Context, name, version string) (*ServerRecord, error) {
	name = strings.TrimSpace(name)
	if err := ValidateServerName(name); err != nil {
		return nil, err
	}
	version = strings.TrimSpace(version)
	if version == "" {
		version = "latest"
	}
	if err := ValidateVersion(version); err != nil {
		return nil, err
	}
	path := "/v0.1/servers/" + url.PathEscape(name) + "/versions/" + url.PathEscape(version)
	var out ServerRecord
	if err := c.get(ctx, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) baseURL() (string, error) {
	u, err := url.Parse(c.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", usageErr("MCP_REGISTRY_URL must be an http(s) URL without credentials, query or fragment")
	}
	return strings.TrimRight(c.BaseURL, "/"), nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	base, err := c.baseURL()
	if err != nil {
		return err
	}
	u := base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return &Error{Kind: KindUnavailable, Msg: err.Error()}
	}
	req.Header.Set("Accept", "application/json")
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return &Error{Kind: KindUnavailable, Msg: "MCP registry is unreachable: " + redactURLErr(err), Hint: "check the network and retry"}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return &Error{Kind: KindUnavailable, Msg: "reading the MCP registry response failed: " + err.Error()}
	}
	if len(body) > maxBody {
		return &Error{Kind: KindUnavailable, Msg: "MCP registry response is too large"}
	}
	if resp.StatusCode != http.StatusOK {
		return statusError(resp, body)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return &Error{Kind: KindUnavailable, Msg: "MCP registry sent a response that is not the expected JSON", Hint: "retry later"}
	}
	return nil
}

func redactURLErr(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	return err.Error()
}

func statusError(resp *http.Response, body []byte) error {
	var doc struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &doc)
	msg := doc.Message
	if msg == "" {
		msg = doc.Error
	}
	if msg == "" {
		msg = http.StatusText(resp.StatusCode)
	}
	switch resp.StatusCode {
	case http.StatusBadRequest:
		return &Error{Kind: KindUsage, Msg: "MCP registry rejected the request: " + msg}
	case http.StatusUnauthorized, http.StatusForbidden:
		return &Error{
			Kind: KindUnauthorized,
			Msg:  "MCP registry denied the request: " + msg,
			Hint: "set MCP_REGISTRY_TOKEN for a registry that needs authentication",
		}
	case http.StatusNotFound:
		return &Error{Kind: KindNotFound, Msg: "not found in the MCP registry: " + msg, Hint: "check the server name and version"}
	case http.StatusTooManyRequests:
		hint := "wait and retry"
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			hint = "retry after " + ra + " seconds"
		}
		return &Error{Kind: KindUnavailable, Msg: "MCP registry rate limit reached", Hint: hint}
	default:
		return &Error{Kind: KindUnavailable, Msg: fmt.Sprintf("MCP registry answered %d: %s", resp.StatusCode, msg), Hint: "retry later"}
	}
}
