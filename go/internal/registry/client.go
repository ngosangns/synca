// Package registry is the skills.sh client: search, leaderboard, official
// list, skill snapshot, and audits. It does no file IO outside its own HTTP.
package registry

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
	defaultBase = "https://skills.sh"
	maxBody     = 32 << 20
	// MaxSearchLimit is the largest limit the search endpoint accepts.
	MaxSearchLimit = 200
)

// Views are the leaderboard views the public listing serves.
var Views = []string{"all-time", "trending", "hot"}

// Kind classifies a failure so the CLI can pick an exit code.
type Kind int

const (
	KindUsage Kind = iota + 1
	KindUnauthorized
	KindNotFound
	KindUnavailable
)

// Error is a registry failure.
type Error struct {
	Kind Kind
	Msg  string
	Hint string
}

func (e *Error) Error() string { return e.Msg }

func usageErr(format string, args ...any) error {
	return &Error{Kind: KindUsage, Msg: fmt.Sprintf(format, args...)}
}

// Client talks to skills.sh. A zero Token means only the public endpoints work.
type Client struct {
	BaseURL     string
	DownloadURL string
	Token       string
	HTTP        *http.Client
	UserAgent   string
}

// New builds a client from the environment: SKILLS_API_URL and
// SKILLS_DOWNLOAD_URL (the same variables the skills CLI reads) and
// SKILLS_API_TOKEN, falling back to VERCEL_OIDC_TOKEN.
func New(version string) *Client {
	base := strings.TrimRight(os.Getenv("SKILLS_API_URL"), "/")
	if base == "" {
		base = defaultBase
	}
	dl := strings.TrimRight(os.Getenv("SKILLS_DOWNLOAD_URL"), "/")
	if dl == "" {
		dl = base
	}
	token := strings.TrimSpace(os.Getenv("SKILLS_API_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("VERCEL_OIDC_TOKEN"))
	}
	return &Client{
		BaseURL:     base,
		DownloadURL: dl,
		Token:       token,
		HTTP:        &http.Client{Timeout: 20 * time.Second},
		UserAgent:   "synca/" + version,
	}
}

// Skill is the shape every listing returns.
type Skill struct {
	ID                string  `json:"id"`
	Source            string  `json:"source"`
	Slug              string  `json:"slug"`
	Name              string  `json:"name"`
	Installs          int     `json:"installs"`
	SourceType        string  `json:"sourceType"`
	InstallURL        *string `json:"installUrl"`
	URL               string  `json:"url"`
	Official          bool    `json:"official"`
	Duplicate         bool    `json:"duplicate"`
	WeeklyInstalls    []int   `json:"weeklyInstalls,omitempty"`
	InstallsYesterday *int    `json:"installsYesterday,omitempty"`
	Change            *int    `json:"change,omitempty"`
}

// SourceTypeOf says "github" for owner/repo and "well-known" for a domain.
func SourceTypeOf(source string) string {
	if strings.Contains(source, "/") {
		return "github"
	}
	return "well-known"
}

func newSkill(source, slug, name string, installs int) Skill {
	s := Skill{
		ID:         source + "/" + slug,
		Source:     source,
		Slug:       slug,
		Name:       name,
		Installs:   installs,
		SourceType: SourceTypeOf(source),
	}
	if s.Name == "" {
		s.Name = slug
	}
	if s.SourceType == "github" {
		u := "https://github.com/" + source
		s.InstallURL = &u
	} else {
		u := "https://" + source
		s.InstallURL = &u
	}
	s.URL = defaultBase + "/" + s.ID
	return s
}

var segmentRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// SplitID validates "owner/repo/slug" or "domain/slug" and returns its segments.
func SplitID(id string) ([]string, error) {
	parts := strings.Split(strings.Trim(strings.TrimSpace(id), "/"), "/")
	if len(parts) != 2 && len(parts) != 3 {
		return nil, usageErr("invalid skill id %q (use owner/repo/slug)", id)
	}
	for _, p := range parts {
		if p == "." || p == ".." || !segmentRe.MatchString(p) {
			return nil, usageErr("invalid skill id %q (use owner/repo/slug)", id)
		}
	}
	return parts, nil
}

func (c *Client) hasToken() bool { return c.Token != "" }

func needsToken(what string) error {
	return &Error{
		Kind: KindUnauthorized,
		Msg:  what + " needs a skills.sh API token",
		Hint: "set SKILLS_API_TOKEN to a Vercel OIDC token (see https://skills.sh/docs/api#authentication)",
	}
}

func (c *Client) get(ctx context.Context, base, path string, query url.Values, auth bool, out any) error {
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
	if auth && c.hasToken() {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return &Error{Kind: KindUnavailable, Msg: "skills.sh is unreachable: " + redactURLErr(err), Hint: "check the network and retry"}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return &Error{Kind: KindUnavailable, Msg: "reading skills.sh response failed: " + err.Error()}
	}
	if len(body) > maxBody {
		return &Error{Kind: KindUnavailable, Msg: "skills.sh response is too large"}
	}
	if resp.StatusCode != http.StatusOK {
		return statusError(resp, body)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return &Error{Kind: KindUnavailable, Msg: "skills.sh sent a response that is not the expected JSON"}
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
		return &Error{Kind: KindUsage, Msg: "skills.sh rejected the request: " + msg}
	case http.StatusUnauthorized, http.StatusForbidden:
		return needsToken("this request")
	case http.StatusNotFound:
		return &Error{Kind: KindNotFound, Msg: "not found on skills.sh: " + msg}
	case http.StatusTooManyRequests:
		hint := "wait and retry"
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			hint = "retry after " + ra + " seconds"
		}
		return &Error{Kind: KindUnavailable, Msg: "skills.sh rate limit reached", Hint: hint}
	default:
		return &Error{Kind: KindUnavailable, Msg: fmt.Sprintf("skills.sh answered %d: %s", resp.StatusCode, msg), Hint: "retry later"}
	}
}

// SearchResult is the search document.
type SearchResult struct {
	Query      string  `json:"query"`
	SearchType string  `json:"searchType"`
	Count      int     `json:"count"`
	Skills     []Skill `json:"skills"`
}

// Search finds skills by name, source or description. The query needs at least
// two characters. A zero limit means the server default.
func (c *Client) Search(ctx context.Context, query, owner string, limit int) (*SearchResult, error) {
	query = strings.TrimSpace(query)
	if utf8.RuneCountInString(query) < 2 {
		return nil, usageErr("search query needs at least 2 characters")
	}
	if limit < 0 || limit > MaxSearchLimit {
		return nil, usageErr("--limit must be between 1 and %d", MaxSearchLimit)
	}
	q := url.Values{"q": {query}}
	if limit > 0 {
		q.Set("limit", fmt.Sprint(limit))
	}
	if owner = strings.ToLower(strings.TrimSpace(owner)); owner != "" {
		if !ownerRe.MatchString(owner) {
			return nil, usageErr("--owner must be a valid GitHub owner")
		}
		q.Set("owner", owner)
	}
	var raw struct {
		SearchType string `json:"searchType"`
		Skills     []struct {
			ID       string `json:"id"`
			Source   string `json:"source"`
			SkillID  string `json:"skillId"`
			Name     string `json:"name"`
			Installs int    `json:"installs"`
		} `json:"skills"`
	}
	if err := c.get(ctx, c.BaseURL, "/api/search", q, false, &raw); err != nil {
		return nil, err
	}
	out := &SearchResult{Query: query, SearchType: raw.SearchType, Skills: []Skill{}}
	for _, r := range raw.Skills {
		if r.Source == "" || r.SkillID == "" {
			continue
		}
		out.Skills = append(out.Skills, newSkill(r.Source, r.SkillID, r.Name, r.Installs))
	}
	out.Count = len(out.Skills)
	return out, nil
}

var ownerRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,38})$`)

// BrowseResult is one page of the leaderboard.
type BrowseResult struct {
	View    string  `json:"view"`
	Page    int     `json:"page"`
	PerPage int     `json:"perPage"`
	Total   int     `json:"total"`
	HasMore bool    `json:"hasMore"`
	Skills  []Skill `json:"skills"`
}

// Browse reads one leaderboard page. Pages are 0-indexed. A non-empty source
// lists one repository and needs a token.
func (c *Client) Browse(ctx context.Context, view string, page int, source string) (*BrowseResult, error) {
	if view == "" {
		view = "all-time"
	}
	known := false
	for _, v := range Views {
		known = known || v == view
	}
	if !known {
		return nil, usageErr("invalid --view %q (use all-time|trending|hot)", view)
	}
	if page < 0 {
		return nil, usageErr("--page must be 0 or more")
	}
	if source != "" {
		return c.browseSource(ctx, view, page, source)
	}
	var raw struct {
		Skills []struct {
			Source            string `json:"source"`
			SkillID           string `json:"skillId"`
			Name              string `json:"name"`
			Installs          int    `json:"installs"`
			WeeklyInstalls    []int  `json:"weeklyInstalls"`
			IsOfficial        bool   `json:"isOfficial"`
			InstallsYesterday *int   `json:"installsYesterday"`
			Change            *int   `json:"change"`
		} `json:"skills"`
		Total   int  `json:"total"`
		HasMore bool `json:"hasMore"`
		Page    int  `json:"page"`
	}
	path := fmt.Sprintf("/api/skills/%s/%d", view, page)
	if err := c.get(ctx, c.BaseURL, path, nil, false, &raw); err != nil {
		return nil, err
	}
	out := &BrowseResult{View: view, Page: page, Total: raw.Total, HasMore: raw.HasMore, Skills: []Skill{}}
	for _, r := range raw.Skills {
		if r.Source == "" || r.SkillID == "" {
			continue
		}
		s := newSkill(r.Source, r.SkillID, r.Name, r.Installs)
		s.Official = r.IsOfficial
		s.WeeklyInstalls = r.WeeklyInstalls
		s.InstallsYesterday = r.InstallsYesterday
		s.Change = r.Change
		out.Skills = append(out.Skills, s)
	}
	out.PerPage = len(out.Skills)
	return out, nil
}

var repoRe = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

func (c *Client) browseSource(ctx context.Context, view string, page int, source string) (*BrowseResult, error) {
	if view != "all-time" {
		return nil, usageErr("--source works with --view all-time only")
	}
	if !repoRe.MatchString(source) {
		return nil, usageErr("--source must be owner/repo")
	}
	if !c.hasToken() {
		return nil, needsToken("listing one repository")
	}
	var raw struct {
		Data []v1Skill `json:"data"`
		Page struct {
			Page    int  `json:"page"`
			PerPage int  `json:"perPage"`
			Total   int  `json:"total"`
			HasMore bool `json:"hasMore"`
		} `json:"pagination"`
	}
	q := url.Values{"view": {view}, "page": {fmt.Sprint(page)}, "per_page": {"100"}, "source": {source}}
	if err := c.get(ctx, c.BaseURL, "/api/v1/skills", q, true, &raw); err != nil {
		return nil, err
	}
	out := &BrowseResult{View: view, Page: raw.Page.Page, PerPage: raw.Page.PerPage, Total: raw.Page.Total, HasMore: raw.Page.HasMore, Skills: []Skill{}}
	for _, r := range raw.Data {
		out.Skills = append(out.Skills, r.skill(false))
	}
	return out, nil
}

type v1Skill struct {
	ID          string  `json:"id"`
	Slug        string  `json:"slug"`
	Name        string  `json:"name"`
	Source      string  `json:"source"`
	Installs    int     `json:"installs"`
	SourceType  string  `json:"sourceType"`
	InstallURL  *string `json:"installUrl"`
	URL         string  `json:"url"`
	IsDuplicate bool    `json:"isDuplicate"`
}

func (v v1Skill) skill(official bool) Skill {
	s := newSkill(v.Source, v.Slug, v.Name, v.Installs)
	if v.ID != "" {
		s.ID = v.ID
	}
	if v.SourceType != "" {
		s.SourceType = v.SourceType
	}
	if v.InstallURL != nil {
		s.InstallURL = v.InstallURL
	}
	if v.URL != "" {
		s.URL = v.URL
	}
	s.Official = official
	s.Duplicate = v.IsDuplicate
	return s
}

// Owner is one publisher in the official list.
type Owner struct {
	Owner         string  `json:"owner"`
	TotalInstalls int     `json:"totalInstalls"`
	FeaturedRepo  string  `json:"featuredRepo"`
	FeaturedSkill string  `json:"featuredSkill"`
	Skills        []Skill `json:"skills"`
}

// OfficialResult is the curated first-party list.
type OfficialResult struct {
	Owners      []Owner `json:"owners"`
	TotalOwners int     `json:"totalOwners"`
	TotalSkills int     `json:"totalSkills"`
	GeneratedAt string  `json:"generatedAt"`
}

// Official reads the curated first-party skills. It needs a token.
func (c *Client) Official(ctx context.Context) (*OfficialResult, error) {
	if !c.hasToken() {
		return nil, needsToken("the official list")
	}
	var raw struct {
		Data []struct {
			Owner         string    `json:"owner"`
			TotalInstalls int       `json:"totalInstalls"`
			FeaturedRepo  string    `json:"featuredRepo"`
			FeaturedSkill string    `json:"featuredSkill"`
			Skills        []v1Skill `json:"skills"`
		} `json:"data"`
		TotalOwners int    `json:"totalOwners"`
		TotalSkills int    `json:"totalSkills"`
		GeneratedAt string `json:"generatedAt"`
	}
	if err := c.get(ctx, c.BaseURL, "/api/v1/skills/curated", nil, true, &raw); err != nil {
		return nil, err
	}
	out := &OfficialResult{Owners: []Owner{}, TotalOwners: raw.TotalOwners, TotalSkills: raw.TotalSkills, GeneratedAt: raw.GeneratedAt}
	for _, o := range raw.Data {
		owner := Owner{Owner: o.Owner, TotalInstalls: o.TotalInstalls, FeaturedRepo: o.FeaturedRepo, FeaturedSkill: o.FeaturedSkill, Skills: []Skill{}}
		for _, s := range o.Skills {
			owner.Skills = append(owner.Skills, s.skill(true))
		}
		out.Owners = append(out.Owners, owner)
	}
	return out, nil
}

// File is one file of a skill snapshot.
type File struct {
	Path     string  `json:"path"`
	Size     int     `json:"size"`
	Contents *string `json:"contents,omitempty"`
}

// Info is a skill snapshot.
type Info struct {
	ID       string  `json:"id"`
	Source   string  `json:"source"`
	Slug     string  `json:"slug"`
	Installs *int    `json:"installs"`
	Hash     *string `json:"hash"`
	Files    []File  `json:"files"`
}

// Info reads a skill's file snapshot. With a token it uses the v1 detail
// endpoint, which also has the install count. Without one it reads the public
// download endpoint, which has no count and covers GitHub skills only.
func (c *Client) Info(ctx context.Context, id string, contents bool) (*Info, error) {
	parts, err := SplitID(id)
	if err != nil {
		return nil, err
	}
	var files []struct {
		Path     string `json:"path"`
		Contents string `json:"contents"`
	}
	info := &Info{ID: strings.Join(parts, "/"), Files: []File{}}
	if c.hasToken() {
		var raw struct {
			Source   string  `json:"source"`
			Slug     string  `json:"slug"`
			Installs *int    `json:"installs"`
			Hash     *string `json:"hash"`
			Files    []struct {
				Path     string `json:"path"`
				Contents string `json:"contents"`
			} `json:"files"`
		}
		if err := c.get(ctx, c.BaseURL, "/api/v1/skills/"+strings.Join(parts, "/"), nil, true, &raw); err != nil {
			return nil, err
		}
		info.Source, info.Slug, info.Installs, info.Hash = raw.Source, raw.Slug, raw.Installs, raw.Hash
		files = raw.Files
	} else {
		if len(parts) != 3 {
			return nil, needsToken("a well-known skill")
		}
		var raw struct {
			Hash  *string `json:"hash"`
			Files []struct {
				Path     string `json:"path"`
				Contents string `json:"contents"`
			} `json:"files"`
		}
		if err := c.get(ctx, c.DownloadURL, "/api/download/"+strings.Join(parts, "/"), nil, false, &raw); err != nil {
			return nil, err
		}
		info.Source, info.Slug, info.Hash = parts[0]+"/"+parts[1], parts[2], raw.Hash
		files = raw.Files
	}
	for _, f := range files {
		file := File{Path: f.Path, Size: len(f.Contents)}
		if contents {
			text := f.Contents
			file.Contents = &text
		}
		info.Files = append(info.Files, file)
	}
	return info, nil
}

// Audit is one partner verdict.
type Audit struct {
	Provider   string   `json:"provider"`
	Slug       string   `json:"slug"`
	Status     string   `json:"status"`
	Summary    string   `json:"summary"`
	AuditedAt  string   `json:"auditedAt"`
	RiskLevel  string   `json:"riskLevel,omitempty"`
	Categories []string `json:"categories,omitempty"`
}

// AuditResult lists the verdicts for one skill.
type AuditResult struct {
	ID     string  `json:"id"`
	Source string  `json:"source"`
	Slug   string  `json:"slug"`
	Audits []Audit `json:"audits"`
}

// Audits reads the security audits. A skill nobody audited yet gives an empty
// list, not an error.
func (c *Client) Audits(ctx context.Context, id string) (*AuditResult, error) {
	parts, err := SplitID(id)
	if err != nil {
		return nil, err
	}
	joined := strings.Join(parts, "/")
	var out AuditResult
	err = c.get(ctx, c.BaseURL, "/api/v1/skills/audit/"+joined, nil, true, &out)
	var re *Error
	if errors.As(err, &re) && re.Kind == KindNotFound {
		source := parts[0]
		if len(parts) == 3 {
			source += "/" + parts[1]
		}
		return &AuditResult{ID: joined, Source: source, Slug: parts[len(parts)-1], Audits: []Audit{}}, nil
	}
	if err != nil {
		return nil, err
	}
	if out.Audits == nil {
		out.Audits = []Audit{}
	}
	return &out, nil
}

// Snapshot is the public file snapshot of a GitHub skill.
type Snapshot struct {
	Hash  string
	Files []DownloadFile
}

// DownloadFile is one file of a snapshot.
type DownloadFile struct {
	Path     string `json:"path"`
	Contents string `json:"contents"`
}

// Download reads the public snapshot of a GitHub skill with file contents.
func (c *Client) Download(ctx context.Context, owner, repo, slug string) (*Snapshot, error) {
	for _, p := range []string{owner, repo, slug} {
		if p == "." || p == ".." || !segmentRe.MatchString(p) {
			return nil, usageErr("invalid skill %s/%s/%s", owner, repo, slug)
		}
	}
	var raw struct {
		Hash  string         `json:"hash"`
		Files []DownloadFile `json:"files"`
	}
	if err := c.get(ctx, c.DownloadURL, "/api/download/"+owner+"/"+repo+"/"+slug, nil, false, &raw); err != nil {
		return nil, err
	}
	return &Snapshot{Hash: raw.Hash, Files: raw.Files}, nil
}
