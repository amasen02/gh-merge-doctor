package diagnose

import (
	"fmt"
	"sort"
	"strings"

	"github.com/amasen02/gh-merge-doctor/internal/model"
)

const (
	StatusReady    = "ready"
	StatusBlocked  = "blocked"
	StatusUnknown  = "unknown"
	StatusTerminal = "terminal"
)

type Finding struct {
	Code      string   `json:"code"`
	Severity  string   `json:"severity"`
	Summary   string   `json:"summary"`
	Evidence  []string `json:"evidence,omitempty"`
	NextSteps []string `json:"next_steps,omitempty"`
}

type Report struct {
	Status      string    `json:"status"`
	ExitCode    int       `json:"-"`
	Summary     string    `json:"summary"`
	HeadSHA     string    `json:"head_sha"`
	SelectedSHA string    `json:"selected_sha,omitempty"`
	Findings    []Finding `json:"findings,omitempty"`
	Blockers    []Finding `json:"blockers,omitempty"`
	NextSteps   []string  `json:"next_steps,omitempty"`
	Limitations []string  `json:"limitations,omitempty"`
}

func Diagnose(s model.Snapshot) Report {
	r := Report{HeadSHA: s.HeadSHA, Limitations: append([]string(nil), s.Limitations...)}
	r.SelectedSHA = selectSHA(s)

	if s.State == "closed" || s.Merged {
		r.Status, r.ExitCode = StatusTerminal, 3
		r.Summary = "pull request is terminal; merge readiness no longer applies"
		r.Findings = append(r.Findings, Finding{Code: "TERMINAL_PR", Severity: "info", Summary: r.Summary, Evidence: []string{s.State}})
		return r
	}

	if s.HeadSHA == "" || s.HeadChanged {
		r.Limitations = append(r.Limitations, "pull request head changed or was not available for the complete capture")
	}
	if !s.Policy.Known {
		r.Limitations = append(r.Limitations, "required-check policy could not be established")
	}
	if s.Draft {
		r.Findings = append(r.Findings, Finding{Code: "DRAFT_PR", Severity: "blocker", Summary: "pull request is still a draft", NextSteps: []string{"mark the pull request ready for review when the change is ready"}})
	}
	if strings.EqualFold(s.ReviewDecision, "CHANGES_REQUESTED") {
		r.Findings = append(r.Findings, Finding{Code: "CHANGES_REQUESTED", Severity: "blocker", Summary: "reviewers requested changes", NextSteps: []string{"address requested changes and request another review"}})
	} else if strings.EqualFold(s.ReviewDecision, "REVIEW_REQUIRED") {
		r.Findings = append(r.Findings, Finding{Code: "REVIEW_REQUIRED", Severity: "blocker", Summary: "required review is not complete", NextSteps: []string{"obtain the required review"}})
	}
	if !strings.EqualFold(s.State, "open") {
		r.Limitations = append(r.Limitations, "pull request state is not explicitly open")
	}

	opaqueBlocked := false
	switch strings.ToLower(s.MergeableState) {
	case "clean":
		// Explicitly clean is the only merge state that can support readiness.
	case "conflicting", "dirty":
		r.Findings = append(r.Findings, Finding{Code: "MERGE_CONFLICT", Severity: "blocker", Summary: "GitHub reports merge conflicts", NextSteps: []string{"update the branch and resolve the reported conflicts"}})
	case "behind":
		r.Findings = append(r.Findings, Finding{Code: "BRANCH_BEHIND", Severity: "blocker", Summary: "branch is behind its base branch", NextSteps: []string{"update the branch from the base branch, then rerun the diagnosis"}})
	case "blocked":
		// A supported check/review finding may explain blocked. Defer the
		// opaque-state decision until required checks have been evaluated.
		opaqueBlocked = true
	case "", "unknown":
		if s.Mergeable == "" || strings.EqualFold(s.Mergeable, "UNKNOWN") {
			r.Limitations = append(r.Limitations, "GitHub mergeability is unknown")
		}
	default:
		r.Limitations = append(r.Limitations, "GitHub returned an unsupported merge state: "+safe(s.MergeableState))
	}
	if strings.EqualFold(s.MergeableState, "clean") && !strings.EqualFold(s.Mergeable, "MERGEABLE") {
		r.Limitations = append(r.Limitations, "GitHub did not explicitly report the pull request mergeable")
	}

	for _, required := range s.Policy.RequiredStatusChecks {
		evaluateRequired(&r, s, required)
	}
	if opaqueBlocked && !hasBlockingCheck(r.Findings) && len(r.Findings) == 0 {
		r.Limitations = append(r.Limitations, "GitHub reports blocked without a supported cause")
	}
	for _, f := range r.Findings {
		if f.Severity == "unknown" {
			r.Limitations = append(r.Limitations, "one or more observed findings have unknown certainty")
			break
		}
	}

	if len(r.Limitations) > 0 {
		r.Status, r.ExitCode = StatusUnknown, 3
		r.Summary = "merge readiness is unknown because the capture is incomplete"
	} else if len(r.Findings) > 0 {
		r.Status, r.ExitCode = StatusBlocked, 1
		r.Summary = fmt.Sprintf("merge is blocked by %d observed prerequisite(s)", len(r.Findings))
	} else {
		r.Status, r.ExitCode = StatusReady, 0
		r.Summary = "no supported merge blocker observed for this pull request"
	}
	for _, f := range r.Findings {
		if f.Severity == "blocker" {
			r.Blockers = append(r.Blockers, f)
		}
		r.NextSteps = append(r.NextSteps, f.NextSteps...)
	}
	return r
}

func selectSHA(s model.Snapshot) string {
	if s.TestMergeSHA != "" {
		for _, c := range s.CheckRuns {
			if c.SHA == s.TestMergeSHA {
				return s.TestMergeSHA
			}
		}
		for _, c := range s.Statuses {
			if c.SHA == s.TestMergeSHA {
				return s.TestMergeSHA
			}
		}
	}
	return s.HeadSHA
}

func evaluateRequired(r *Report, s model.Snapshot, required model.RequiredCheck) {
	selected := r.SelectedSHA
	runs := latestRuns(s.CheckRuns, required.Context, selected)
	statuses := latestStatuses(s.Statuses, required.Context, selected)
	if len(runs) == 0 && len(statuses) == 0 {
		r.Findings = append(r.Findings, Finding{Code: "MISSING_REQUIRED_CHECK", Severity: "blocker", Summary: fmt.Sprintf("required check %q has no result on %s", safe(required.Context), safe(selected)), Evidence: []string{"sha=" + safe(selected), "required-context=" + safe(required.Context)}, NextSteps: []string{"restore or run the required workflow for this exact commit"}})
		return
	}
	if required.AppID != nil {
		matched := false
		for _, run := range runs {
			if run.AppID != nil && *run.AppID == *required.AppID {
				matched = true
				break
			}
		}
		if !matched {
			r.Findings = append(r.Findings, Finding{Code: "CHECK_SOURCE_MISMATCH", Severity: "unknown", Summary: fmt.Sprintf("required check %q has no result from app %d on %s", safe(required.Context), *required.AppID, safe(selected)), NextSteps: []string{"check that the required workflow reports under the configured provider and rerun it"}})
			return
		}
		filtered := runs[:0]
		for _, run := range runs {
			if run.AppID != nil && *run.AppID == *required.AppID {
				filtered = append(filtered, run)
			}
		}
		runs = filtered
	}
	if required.AppID == nil {
		for _, run := range runs {
			if !run.SourceKnown {
				r.Findings = append(r.Findings, Finding{Code: "CHECK_SOURCE_MISMATCH", Severity: "unknown", Summary: fmt.Sprintf("required check %q has no identifiable check-run provider", safe(required.Context)), Evidence: cleanEvidence([]string{run.URL, "sha=" + safe(run.SHA)}), NextSteps: []string{"inspect the workflow provider and rerun the diagnosis"}})
				break
			}
		}
	}
	for _, run := range runs {
		evaluateRun(r, run, required.Context)
	}
	for _, status := range statuses {
		if required.AppID != nil {
			r.Findings = append(r.Findings, Finding{Code: "CHECK_SOURCE_MISMATCH", Severity: "unknown", Summary: fmt.Sprintf("required check %q has a commit status with no identifiable provider", safe(required.Context)), Evidence: cleanEvidence([]string{status.URL, "sha=" + safe(status.SHA)}), NextSteps: []string{"inspect the status provider and rerun the diagnosis"}})
			continue
		}
		switch strings.ToLower(status.State) {
		case "success":
		case "pending":
			r.Findings = append(r.Findings, pendingFinding(required.Context, status.URL))
		default:
			r.Findings = append(r.Findings, failedFinding(required.Context, status.URL))
		}
	}
}

func evaluateRun(r *Report, run model.CheckRun, context string) {
	evidence := []string{run.URL}
	if run.SHA != "" {
		evidence = append(evidence, "sha="+safe(run.SHA))
	}
	if !strings.EqualFold(run.Status, "completed") {
		r.Findings = append(r.Findings, pendingFinding(context, evidence...))
		return
	}
	switch strings.ToLower(run.Conclusion) {
	case "success", "neutral", "skipped":
		return
	case "", "pending":
		r.Findings = append(r.Findings, pendingFinding(context, evidence...))
	default:
		r.Findings = append(r.Findings, failedFinding(context, evidence...))
	}
}

func latestRuns(all []model.CheckRun, context, sha string) []model.CheckRun {
	seen := map[string]model.CheckRun{}
	for _, run := range all {
		if run.Name != context || run.SHA != sha {
			continue
		}
		key := fmt.Sprintf("%s/%d", run.Name, ptrValue(run.AppID))
		if prior, ok := seen[key]; !ok || newerRun(run, prior) {
			seen[key] = run
		}
	}
	out := make([]model.CheckRun, 0, len(seen))
	for _, run := range seen {
		out = append(out, run)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return ptrValue(out[i].AppID) < ptrValue(out[j].AppID)
	})
	return out
}

func newerRun(a, b model.CheckRun) bool {
	// Start time identifies the attempt; completion time must not make an
	// older, slower run replace a newer retry. IDs break ties deterministically.
	if a.ID != 0 && b.ID != 0 && a.ID != b.ID {
		return a.ID > b.ID
	}
	if a.StartedAt != b.StartedAt {
		return a.StartedAt > b.StartedAt
	}
	return a.ID > b.ID
}

func latestStatuses(all []model.Status, context, sha string) []model.Status {
	var best *model.Status
	for i := range all {
		status := all[i]
		if status.Context != context || status.SHA != sha {
			continue
		}
		if best == nil || newerStatus(status, *best) {
			copy := status
			best = &copy
		}
	}
	if best == nil {
		return nil
	}
	return []model.Status{*best}
}

func newerStatus(a, b model.Status) bool {
	if a.UpdatedAt != b.UpdatedAt {
		return a.UpdatedAt > b.UpdatedAt
	}
	return a.ID > b.ID
}

func pendingFinding(context string, evidence ...string) Finding {
	return Finding{Code: "REQUIRED_CHECK_PENDING", Severity: "blocker", Summary: fmt.Sprintf("required check %q is still pending", safe(context)), Evidence: cleanEvidence(evidence), NextSteps: []string{"wait for the required workflow or inspect its run for the pending step"}}
}
func failedFinding(context string, evidence ...string) Finding {
	return Finding{Code: "REQUIRED_CHECK_FAILED", Severity: "blocker", Summary: fmt.Sprintf("required check %q did not succeed", safe(context)), Evidence: cleanEvidence(evidence), NextSteps: []string{"fix the required workflow failure and rerun it for this commit"}}
}
func hasBlockingCheck(findings []Finding) bool {
	for _, f := range findings {
		if f.Severity == "blocker" && (strings.HasPrefix(f.Code, "REQUIRED_CHECK_") || strings.HasPrefix(f.Code, "MISSING_REQUIRED")) {
			return true
		}
	}
	return false
}
func ptrValue(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
func cleanEvidence(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s != "" {
			out = append(out, safe(s))
		}
	}
	return out
}
func safe(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '\uFFFD'
		}
		return r
	}, s)
}
