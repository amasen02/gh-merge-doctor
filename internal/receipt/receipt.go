package receipt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/amasen02/gh-merge-doctor/internal/diagnose"
	"github.com/amasen02/gh-merge-doctor/internal/model"
)

const SchemaVersion = 1

type Envelope struct {
	SchemaVersion  int                `json:"schema_version"`
	ObservedAt     string             `json:"observed_at"`
	Repo           string             `json:"repo"`
	PR             int                `json:"pr"`
	Status         string             `json:"status"`
	Summary        string             `json:"summary"`
	HeadSHA        string             `json:"head_sha"`
	SelectedSHA    string             `json:"selected_sha,omitempty"`
	Findings       []diagnose.Finding `json:"findings"`
	Blockers       []diagnose.Finding `json:"blockers"`
	NextSteps      []string           `json:"next_steps"`
	Limitations    []string           `json:"limitations"`
	SourceURLs     []string           `json:"source_urls"`
	ResponseSHA256 string             `json:"response_sha256"`
}

type canonical struct {
	SchemaVersion  int              `json:"schema_version"`
	Repo           string           `json:"repo"`
	PR             int              `json:"pr"`
	State          string           `json:"state"`
	Merged         bool             `json:"merged"`
	Draft          bool             `json:"draft"`
	HeadSHA        string           `json:"head_sha"`
	BaseSHA        string           `json:"base_sha"`
	BaseRef        string           `json:"base_ref"`
	TestMergeSHA   string           `json:"test_merge_sha,omitempty"`
	Mergeable      string           `json:"mergeable,omitempty"`
	MergeableState string           `json:"mergeable_state,omitempty"`
	ReviewDecision string           `json:"review_decision,omitempty"`
	HeadChanged    bool             `json:"head_changed,omitempty"`
	SelectedSHA    string           `json:"selected_sha,omitempty"`
	Policy         model.Policy     `json:"policy"`
	CheckRuns      []model.CheckRun `json:"check_runs,omitempty"`
	Statuses       []model.Status   `json:"statuses,omitempty"`
	Limitations    []string         `json:"limitations,omitempty"`
}

func Build(s model.Snapshot, r diagnose.Report, observed time.Time) Envelope {
	findings := append([]diagnose.Finding(nil), r.Findings...)
	blockers := append([]diagnose.Finding(nil), r.Blockers...)
	limits := append([]string(nil), r.Limitations...)
	urls := append([]string(nil), s.SourceURLs...)
	if findings == nil {
		findings = []diagnose.Finding{}
	}
	if blockers == nil {
		blockers = []diagnose.Finding{}
	}
	if r.NextSteps == nil {
		r.NextSteps = []string{}
	}
	if limits == nil {
		limits = []string{}
	}
	if urls == nil {
		urls = []string{}
	}
	sort.Strings(urls)
	sort.Strings(limits)
	selected := r.SelectedSHA
	checks := make([]model.CheckRun, 0, len(s.CheckRuns))
	for _, check := range s.CheckRuns {
		if check.SHA == selected {
			checks = append(checks, check)
		}
	}
	statuses := make([]model.Status, 0, len(s.Statuses))
	for _, status := range s.Statuses {
		if status.SHA == selected {
			statuses = append(statuses, status)
		}
	}
	sort.Slice(checks, func(i, j int) bool {
		if checks[i].Name != checks[j].Name {
			return checks[i].Name < checks[j].Name
		}
		if checks[i].StartedAt != checks[j].StartedAt {
			return checks[i].StartedAt < checks[j].StartedAt
		}
		return checks[i].ID < checks[j].ID
	})
	sort.Slice(statuses, func(i, j int) bool {
		if statuses[i].Context != statuses[j].Context {
			return statuses[i].Context < statuses[j].Context
		}
		return statuses[i].ID < statuses[j].ID
	})
	policy := s.Policy
	policy.RequiredStatusChecks = append([]model.RequiredCheck(nil), s.Policy.RequiredStatusChecks...)
	sort.Slice(policy.RequiredStatusChecks, func(i, j int) bool {
		if policy.RequiredStatusChecks[i].Context != policy.RequiredStatusChecks[j].Context {
			return policy.RequiredStatusChecks[i].Context < policy.RequiredStatusChecks[j].Context
		}
		return appIDValue(policy.RequiredStatusChecks[i].AppID) < appIDValue(policy.RequiredStatusChecks[j].AppID)
	})
	c := canonical{SchemaVersion: SchemaVersion, Repo: s.Repo, PR: s.PR, State: s.State, Merged: s.Merged, Draft: s.Draft, HeadSHA: s.HeadSHA, BaseSHA: s.BaseSHA, BaseRef: s.BaseRef, TestMergeSHA: s.TestMergeSHA, Mergeable: s.Mergeable, MergeableState: s.MergeableState, ReviewDecision: s.ReviewDecision, HeadChanged: s.HeadChanged, SelectedSHA: selected, Policy: policy, CheckRuns: checks, Statuses: statuses, Limitations: limits}
	b, _ := json.Marshal(c)
	h := sha256.Sum256(b)
	return Envelope{SchemaVersion: SchemaVersion, ObservedAt: observed.UTC().Format(time.RFC3339Nano), Repo: s.Repo, PR: s.PR, Status: r.Status, Summary: r.Summary, HeadSHA: r.HeadSHA, SelectedSHA: r.SelectedSHA, Findings: findings, Blockers: blockers, NextSteps: append([]string{}, r.NextSteps...), Limitations: limits, SourceURLs: urls, ResponseSHA256: hex.EncodeToString(h[:])}
}

func appIDValue(id *int64) int64 {
	if id == nil {
		return -1
	}
	return *id
}

func Write(filename string, e Envelope) error {
	if filename == "" {
		return fmt.Errorf("receipt path is empty")
	}
	if dir := filepath.Dir(filename); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("open receipt exclusively: %w", err)
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		_ = os.Remove(filename)
		return err
	}
	return f.Close()
}
