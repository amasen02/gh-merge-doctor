package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/amasen02/gh-merge-doctor/internal/model"
)

const (
	maxResponseBytes = 2 << 20
	maxPages         = 3
	maxRequests      = 20
	defaultTimeout   = 20 * time.Second
)

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// Runner invokes gh with an argument vector and never constructs a shell command.
type Runner struct {
	Command  string
	Timeout  time.Duration
	MaxBytes int
	requests int
}

// Caller is the narrow read-only boundary used by the collector. Tests can
// provide a scripted implementation without network access.
type Caller interface {
	Call(context.Context, ...string) ([]byte, error)
}

func (r *Runner) Call(ctx context.Context, args ...string) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("empty gh argument vector")
	}
	if r.requests >= maxRequests {
		return nil, errors.New("gh request bound exceeded")
	}
	r.requests++
	command := r.Command
	if command == "" {
		command = "gh"
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(callCtx, command, args...)
	// An inherited token may authenticate gh, but GH_HOST is removed so every
	// request is pinned to github.com by the explicit --hostname argument.
	cmd.Env = withoutEnv(os.Environ(), "GH_HOST")
	var out limitedBuffer
	out.max = r.MaxBytes
	if out.max <= 0 {
		out.max = maxResponseBytes
	}
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	err := cmd.Run()
	if callCtx.Err() != nil {
		return nil, fmt.Errorf("gh request timed out: %w", callCtx.Err())
	}
	if err != nil {
		return nil, fmt.Errorf("gh request failed: %w", err)
	}
	return out.Bytes(), nil
}

type limitedBuffer struct {
	bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		return 0, fmt.Errorf("response exceeds %d bytes", b.max)
	}
	return b.Buffer.Write(p)
}
func withoutEnv(env []string, name string) []string {
	prefix := name + "="
	out := make([]string, 0, len(env))
	for _, item := range env {
		if !strings.HasPrefix(item, prefix) {
			out = append(out, item)
		}
	}
	return out
}

func ValidateRepo(repo string) error {
	parts := strings.Split(repo, "/")
	if !repoPattern.MatchString(repo) || len(parts) != 2 || parts[0] == "." || parts[0] == ".." || parts[1] == "." || parts[1] == ".." || strings.HasPrefix(parts[0], "-") || strings.HasPrefix(parts[1], "-") || strings.ContainsAny(repo, "\\\r\n\t") {
		return fmt.Errorf("repo must be owner/name on github.com")
	}
	return nil
}
func ValidatePR(pr int) error {
	if pr < 1 || pr > 1000000000 {
		return errors.New("pr must be a positive bounded number")
	}
	return nil
}

type prResponse struct {
	State  string `json:"state"`
	Merged bool   `json:"merged"`
	Draft  bool   `json:"draft"`
	Head   struct {
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	} `json:"base"`
	MergeCommitSHA string `json:"merge_commit_sha"`
	Mergeable      any    `json:"mergeable"`
	MergeableState string `json:"mergeable_state"`
}
type viewResponse struct {
	MergeStateStatus string `json:"mergeStateStatus"`
	ReviewDecision   string `json:"reviewDecision"`
}
type protectionResponse struct {
	RequiredStatusChecks *struct {
		Strict   bool     `json:"strict"`
		Contexts []string `json:"contexts"`
		Checks   []struct {
			Context string `json:"context"`
			AppID   *int64 `json:"app_id"`
		} `json:"checks"`
	} `json:"required_status_checks"`
}
type checksResponse struct {
	CheckRuns []struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		ID         int64  `json:"id"`
		HeadSHA    string `json:"head_sha"`
		App        struct {
			ID int64 `json:"id"`
		} `json:"app"`
		StartedAt   string `json:"started_at"`
		CompletedAt string `json:"completed_at"`
		HTMLURL     string `json:"html_url"`
	} `json:"check_runs"`
}
type statusesResponse struct {
	Statuses []struct {
		Context   string `json:"context"`
		State     string `json:"state"`
		TargetURL string `json:"target_url"`
		ID        int64  `json:"id"`
		Creator   *struct {
			Login string `json:"login"`
		} `json:"creator"`
		UpdatedAt string `json:"updated_at"`
	} `json:"statuses"`
}
type effectiveRulesResponse struct {
	Rules []struct {
		Type       string `json:"type"`
		Parameters struct {
			RequiredStatusChecks []struct {
				Context       string `json:"context"`
				IntegrationID *int64 `json:"integration_id"`
			} `json:"required_status_checks"`
		} `json:"parameters"`
	} `json:"rules"`
}

// GitHub has returned both an object wrapper and a bare array for the
// effective branch-rules endpoint. Accept either shape while retaining the
// same normalized model.
func (r *effectiveRulesResponse) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var rules []struct {
			Type       string `json:"type"`
			Parameters struct {
				RequiredStatusChecks []struct {
					Context       string `json:"context"`
					IntegrationID *int64 `json:"integration_id"`
				} `json:"required_status_checks"`
			} `json:"parameters"`
		}
		if err := json.Unmarshal(trimmed, &rules); err != nil {
			return err
		}
		r.Rules = rules
		return nil
	}
	type plain effectiveRulesResponse
	var decoded plain
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		return err
	}
	*r = effectiveRulesResponse(decoded)
	return nil
}

func Collect(ctx context.Context, runner Caller, repo string, pr int) (model.Snapshot, error) {
	var s model.Snapshot
	if err := ValidateRepo(repo); err != nil {
		return s, err
	}
	if err := ValidatePR(pr); err != nil {
		return s, err
	}
	s.SchemaVersion, s.Repo, s.PR = 1, repo, pr
	api := func(endpoint string, v any) error {
		body, err := runner.Call(ctx, "api", "--hostname", "github.com", "--method", "GET", endpoint)
		if err != nil {
			s.Limitations = append(s.Limitations, "GitHub read unavailable for "+endpoint)
			return err
		}
		s.SourceURLs = append(s.SourceURLs, "https://api.github.com"+endpoint)
		if err := json.Unmarshal(body, v); err != nil {
			s.Limitations = append(s.Limitations, "GitHub returned invalid data for "+endpoint)
			return err
		}
		return nil
	}
	var p prResponse
	if err := api(fmt.Sprintf("/repos/%s/pulls/%d", repo, pr), &p); err != nil {
		return s, err
	}
	s.State, s.Merged, s.Draft, s.HeadSHA, s.BaseSHA, s.BaseRef, s.TestMergeSHA, s.MergeableState = p.State, p.Merged, p.Draft, p.Head.SHA, p.Base.SHA, p.Base.Ref, p.MergeCommitSHA, p.MergeableState
	s.Mergeable = normalizeMergeable(p.Mergeable)
	var view viewResponse
	if err := runnerCallJSON(ctx, runner, []string{"pr", "view", "--repo", "github.com/" + repo, strconv.Itoa(pr), "--json", "mergeStateStatus,reviewDecision"}, &view); err == nil {
		s.MergeableState = firstNonEmpty(view.MergeStateStatus, s.MergeableState)
		s.ReviewDecision = view.ReviewDecision
		s.SourceURLs = append(s.SourceURLs, "https://github.com/"+repo+"/pull/"+strconv.Itoa(pr))
	} else {
		s.Limitations = append(s.Limitations, "pull request review state could not be collected")
	}
	var protection protectionResponse
	if err := api("/repos/"+repo+"/branches/"+url.PathEscape(s.BaseRef)+"/protection", &protection); err == nil {
		s.Policy.Known = true
		if protection.RequiredStatusChecks != nil {
			s.Policy.Strict = protection.RequiredStatusChecks.Strict
			for _, c := range protection.RequiredStatusChecks.Checks {
				s.Policy.RequiredStatusChecks = append(s.Policy.RequiredStatusChecks, model.RequiredCheck{Context: c.Context, AppID: c.AppID})
			}
			for _, c := range protection.RequiredStatusChecks.Contexts {
				if !containsContext(s.Policy.RequiredStatusChecks, c) {
					s.Policy.RequiredStatusChecks = append(s.Policy.RequiredStatusChecks, model.RequiredCheck{Context: c})
				}
			}
		}
	} else {
		s.Limitations = append(s.Limitations, "classic branch protection could not be established")
	}
	var effective effectiveRulesResponse
	if err := api("/repos/"+repo+"/rules/branches/"+url.PathEscape(s.BaseRef), &effective); err == nil {
		s.Policy.Known = true
		for _, rule := range effective.Rules {
			if rule.Type == "required_status_checks" {
				for _, c := range rule.Parameters.RequiredStatusChecks {
					s.Policy.RequiredStatusChecks = append(s.Policy.RequiredStatusChecks, model.RequiredCheck{Context: c.Context, AppID: c.IntegrationID})
				}
			} else if rule.Type != "" {
				s.Limitations = append(s.Limitations, "effective repository rule "+rule.Type+" is unsupported")
			}
		}
	} else {
		s.Limitations = append(s.Limitations, "effective repository rules could not be established")
	}
	for _, sha := range uniqueStrings(s.HeadSHA, s.TestMergeSHA) {
		collectCommitEvidence(ctx, runner, repo, sha, &s)
	}
	var p2 prResponse
	if err := api(fmt.Sprintf("/repos/%s/pulls/%d", repo, pr), &p2); err == nil {
		if p2.Head.SHA != s.HeadSHA || p2.Base.SHA != s.BaseSHA || p2.Base.Ref != s.BaseRef || p2.MergeCommitSHA != s.TestMergeSHA || p2.State != s.State || p2.Draft != s.Draft || p2.Merged != s.Merged {
			s.HeadChanged = true
			s.Limitations = append(s.Limitations, "pull request changed during capture")
		}
	}
	s.SourceURLs = uniqueSorted(s.SourceURLs)
	s.Limitations = uniqueSorted(s.Limitations)
	return s, nil
}

func runnerCallJSON(ctx context.Context, runner Caller, args []string, v any) error {
	b, err := runner.Call(ctx, args...)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
func collectCommitEvidence(ctx context.Context, runner Caller, repo, sha string, s *model.Snapshot) {
	checksTruncated := false
	for page := 1; page <= maxPages; page++ {
		var v checksResponse
		endpoint := fmt.Sprintf("/repos/%s/commits/%s/check-runs?per_page=100&page=%d", repo, url.PathEscape(sha), page)
		b, err := runner.Call(ctx, "api", "--hostname", "github.com", "--method", "GET", endpoint)
		if err != nil {
			s.Limitations = append(s.Limitations, "check runs unavailable for "+sha)
			break
		}
		s.SourceURLs = append(s.SourceURLs, "https://api.github.com"+endpoint)
		if json.Unmarshal(b, &v) != nil {
			s.Limitations = append(s.Limitations, "check runs response was invalid for "+sha)
			break
		}
		for _, c := range v.CheckRuns {
			app := c.App.ID
			var appID *int64
			if app != 0 {
				appID = &app
			}
			if c.HeadSHA != "" && c.HeadSHA != sha {
				s.Limitations = append(s.Limitations, "check run response SHA differed from queried commit "+sha)
			}
			s.CheckRuns = append(s.CheckRuns, model.CheckRun{Name: c.Name, SHA: sha, Status: c.Status, Conclusion: c.Conclusion, AppID: appID, SourceKnown: appID != nil, ID: c.ID, StartedAt: c.StartedAt, CompletedAt: c.CompletedAt, URL: fmt.Sprintf("https://github.com/%s/commit/%s/checks", repo, sha)})
		}
		if len(v.CheckRuns) < 100 {
			break
		}
		if page == maxPages {
			checksTruncated = true
		}
	}
	if checksTruncated {
		s.Limitations = append(s.Limitations, "check runs exceeded the bounded page limit for "+sha)
	}
	statusesTruncated := false
	for page := 1; page <= maxPages; page++ {
		var v statusesResponse
		endpoint := fmt.Sprintf("/repos/%s/commits/%s/status?per_page=100&page=%d", repo, url.PathEscape(sha), page)
		b, err := runner.Call(ctx, "api", "--hostname", "github.com", "--method", "GET", endpoint)
		if err != nil {
			s.Limitations = append(s.Limitations, "commit statuses unavailable for "+sha)
			break
		}
		s.SourceURLs = append(s.SourceURLs, "https://api.github.com"+endpoint)
		if json.Unmarshal(b, &v) != nil {
			s.Limitations = append(s.Limitations, "commit statuses response was invalid for "+sha)
			break
		}
		for _, st := range v.Statuses {
			creator := ""
			if st.Creator != nil {
				creator = st.Creator.Login
			}
			s.Statuses = append(s.Statuses, model.Status{Context: st.Context, SHA: sha, State: st.State, Creator: creator, ID: st.ID, UpdatedAt: st.UpdatedAt, URL: fmt.Sprintf("https://github.com/%s/commit/%s/checks", repo, sha)})
		}
		if len(v.Statuses) < 100 {
			break
		}
		if page == maxPages {
			statusesTruncated = true
		}
	}
	if statusesTruncated {
		s.Limitations = append(s.Limitations, "commit statuses exceeded the bounded page limit for "+sha)
	}
}
func containsContext(in []model.RequiredCheck, context string) bool {
	for _, c := range in {
		if c.Context == context {
			return true
		}
	}
	return false
}
func uniqueStrings(values ...string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range values {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
func uniqueSorted(in []string) []string {
	return func() []string { out := uniqueStrings(in...); sort.Strings(out); return out }()
}
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func normalizeMergeable(value any) string {
	switch v := value.(type) {
	case bool:
		if v {
			return "MERGEABLE"
		}
		return "NOT_MERGEABLE"
	case string:
		return strings.ToUpper(v)
	default:
		return ""
	}
}
