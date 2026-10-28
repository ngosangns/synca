package registry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// TreeEntry is one path in a repository tree.
type TreeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

// Tree is the recursive Git tree of a repository's default branch.
type Tree struct {
	SHA       string      `json:"sha"`
	Truncated bool        `json:"truncated"`
	Entries   []TreeEntry `json:"tree"`
}

// FolderSHA is the tree id of a folder, or of the root for "".
func (t *Tree) FolderSHA(folder string) (string, bool) {
	if folder == "" {
		return t.SHA, t.SHA != ""
	}
	for _, e := range t.Entries {
		if e.Type == "tree" && e.Path == folder {
			return e.SHA, true
		}
	}
	return "", false
}

// githubBase is GITHUB_API_URL (set on GitHub Actions and GitHub Enterprise),
// else the public API.
func githubBase() string {
	if v := strings.TrimRight(os.Getenv("GITHUB_API_URL"), "/"); v != "" {
		return v
	}
	return "https://api.github.com"
}

func githubToken() string {
	if v := os.Getenv("GITHUB_TOKEN"); v != "" {
		return v
	}
	return os.Getenv("GH_TOKEN")
}

// GitHubTree reads the recursive tree of owner/repo at its default branch.
// The unauthenticated API allows 60 requests an hour; GITHUB_TOKEN or GH_TOKEN
// raises that. A failure is KindUnavailable: callers fall back to git.
func (c *Client) GitHubTree(ctx context.Context, owner, repo string) (*Tree, error) {
	if !segmentRe.MatchString(owner) || !segmentRe.MatchString(repo) {
		return nil, usageErr("invalid repository %s/%s", owner, repo)
	}
	u := githubBase() + "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/git/trees/HEAD?recursive=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, &Error{Kind: KindUnavailable, Msg: err.Error()}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	if tok := githubToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, &Error{Kind: KindUnavailable, Msg: "GitHub is unreachable: " + redactURLErr(err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		return nil, &Error{Kind: KindUnavailable, Msg: "reading the GitHub tree failed"}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &Error{Kind: KindUnavailable, Msg: "GitHub answered " + http.StatusText(resp.StatusCode)}
	}
	var t Tree
	if err := json.Unmarshal(body, &t); err != nil {
		return nil, &Error{Kind: KindUnavailable, Msg: "GitHub sent a tree that is not the expected JSON"}
	}
	return &t, nil
}
