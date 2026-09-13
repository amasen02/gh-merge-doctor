package diagnose

import (
	"testing"

	"github.com/amasen02/gh-merge-doctor/internal/model"
)

func TestDiagnoseMissingRequiredContextWhileMatrixIsGreen(t *testing.T) {
	s := baseSnapshot()
	s.Policy = model.Policy{Known: true, RequiredStatusChecks: []model.RequiredCheck{{Context: "ci"}}}
	s.CheckRuns = []model.CheckRun{{Name: "build-test (ubuntu)", SHA: s.HeadSHA, Status: "completed", Conclusion: "success", SourceKnown: true}}

	r := Diagnose(s)
	if r.Status != StatusBlocked || r.ExitCode != 1 {
		t.Fatalf("got status=%s exit=%d, want blocked/1", r.Status, r.ExitCode)
	}
	if !hasCode(r, "MISSING_REQUIRED_CHECK") {
		t.Fatalf("findings=%+v, want missing required context", r.Findings)
	}
}

func TestDiagnoseCompletedNeutralAndSkippedAreAcceptable(t *testing.T) {
	for _, conclusion := range []string{"neutral", "skipped"} {
		s := baseSnapshot()
		s.Policy = model.Policy{Known: true, RequiredStatusChecks: []model.RequiredCheck{{Context: "ci"}}}
		s.CheckRuns = []model.CheckRun{{Name: "ci", SHA: s.HeadSHA, Status: "completed", Conclusion: conclusion, SourceKnown: true}}
		r := Diagnose(s)
		if r.Status != StatusReady || r.ExitCode != 0 {
			t.Fatalf("conclusion=%s got status=%s exit=%d, want ready/0", conclusion, r.Status, r.ExitCode)
		}
	}
}

func TestDiagnoseSkippedWorkflowWithMissingContextIsBlocked(t *testing.T) {
	s := baseSnapshot()
	s.Policy = model.Policy{Known: true, RequiredStatusChecks: []model.RequiredCheck{{Context: "ci"}}}
	s.CheckRuns = []model.CheckRun{{Name: "lint", SHA: s.HeadSHA, Status: "completed", Conclusion: "success", SourceKnown: true}}
	r := Diagnose(s)
	if !hasCode(r, "MISSING_REQUIRED_CHECK") || r.Status != StatusBlocked {
		t.Fatalf("got status=%s findings=%+v", r.Status, r.Findings)
	}
}

func TestDiagnoseUnknownPolicyWinsOverObservedBlocker(t *testing.T) {
	s := baseSnapshot()
	s.Policy = model.Policy{Known: false}
	s.MergeableState = "blocked"
	r := Diagnose(s)
	if r.Status != StatusUnknown || r.ExitCode != 3 {
		t.Fatalf("got status=%s exit=%d, want unknown/3", r.Status, r.ExitCode)
	}
}

func TestDiagnoseTerminalPRIsNotReady(t *testing.T) {
	s := baseSnapshot()
	s.State = "closed"
	s.Merged = true
	r := Diagnose(s)
	if r.Status != StatusTerminal || r.ExitCode != 3 {
		t.Fatalf("got status=%s exit=%d, want terminal/3", r.Status, r.ExitCode)
	}
}

func TestDiagnoseHeadChangeIsUnknown(t *testing.T) {
	s := baseSnapshot()
	s.HeadChanged = true
	r := Diagnose(s)
	if r.Status != StatusUnknown || r.ExitCode != 3 {
		t.Fatalf("got status=%s exit=%d, want unknown/3", r.Status, r.ExitCode)
	}
}

func TestDiagnoseRequiresBothCheckRunAndCommitStatus(t *testing.T) {
	s := baseSnapshot()
	s.Policy = model.Policy{Known: true, RequiredStatusChecks: []model.RequiredCheck{{Context: "ci"}}}
	s.CheckRuns = []model.CheckRun{{Name: "ci", SHA: s.HeadSHA, Status: "completed", Conclusion: "success", SourceKnown: true}}
	s.Statuses = []model.Status{{Context: "ci", SHA: s.HeadSHA, State: "failure", Creator: "actions"}}
	r := Diagnose(s)
	if !hasCode(r, "REQUIRED_CHECK_FAILED") || r.Status != StatusBlocked {
		t.Fatalf("got status=%s findings=%+v", r.Status, r.Findings)
	}
}

func TestDiagnoseUnknownMergeStateDoesNotReportReady(t *testing.T) {
	s := baseSnapshot()
	s.MergeableState = "unstable"
	r := Diagnose(s)
	if r.Status != StatusUnknown || r.ExitCode != 3 {
		t.Fatalf("got status=%s exit=%d", r.Status, r.ExitCode)
	}
}

func TestDiagnoseNewerAttemptWinsEvenWhenOlderCompletesLater(t *testing.T) {
	s := baseSnapshot()
	s.Policy = model.Policy{Known: true, RequiredStatusChecks: []model.RequiredCheck{{Context: "ci"}}}
	s.CheckRuns = []model.CheckRun{
		{Name: "ci", SHA: s.HeadSHA, Status: "completed", Conclusion: "failure", SourceKnown: true, StartedAt: "2026-01-01T00:01:00Z", CompletedAt: "2026-01-01T00:04:00Z", ID: 1},
		{Name: "ci", SHA: s.HeadSHA, Status: "completed", Conclusion: "success", SourceKnown: true, StartedAt: "2026-01-01T00:02:00Z", CompletedAt: "2026-01-01T00:03:00Z", ID: 2},
	}
	r := Diagnose(s)
	if r.Status != StatusReady || hasCode(r, "REQUIRED_CHECK_FAILED") {
		t.Fatalf("got status=%s findings=%+v", r.Status, r.Findings)
	}
}

func TestDiagnoseFiltersNonRequiredAppAttempts(t *testing.T) {
	appID := int64(7)
	s := baseSnapshot()
	s.Policy = model.Policy{Known: true, RequiredStatusChecks: []model.RequiredCheck{{Context: "ci", AppID: &appID}}}
	s.CheckRuns = []model.CheckRun{
		{Name: "ci", SHA: s.HeadSHA, Status: "completed", Conclusion: "failure", AppID: ptr(8), SourceKnown: true, StartedAt: "2026-01-01T00:02:00Z", ID: 2},
		{Name: "ci", SHA: s.HeadSHA, Status: "completed", Conclusion: "success", AppID: ptr(7), SourceKnown: true, StartedAt: "2026-01-01T00:01:00Z", ID: 1},
	}
	r := Diagnose(s)
	if r.Status != StatusReady || hasCode(r, "REQUIRED_CHECK_FAILED") {
		t.Fatalf("got status=%s findings=%+v", r.Status, r.Findings)
	}
}

func TestDiagnosePendingRequiresCompletedStatus(t *testing.T) {
	s := baseSnapshot()
	s.Policy = model.Policy{Known: true, RequiredStatusChecks: []model.RequiredCheck{{Context: "ci"}}}
	s.CheckRuns = []model.CheckRun{{Name: "ci", SHA: s.HeadSHA, Status: "queued", Conclusion: "success", SourceKnown: true}}
	r := Diagnose(s)
	if r.Status != StatusBlocked || !hasCode(r, "REQUIRED_CHECK_PENDING") {
		t.Fatalf("got status=%s findings=%+v", r.Status, r.Findings)
	}
}

func TestDiagnoseUnknownCommitStatusProvider(t *testing.T) {
	s := baseSnapshot()
	appID := int64(7)
	s.Policy = model.Policy{Known: true, RequiredStatusChecks: []model.RequiredCheck{{Context: "ci", AppID: &appID}}}
	s.Statuses = []model.Status{{Context: "ci", SHA: s.HeadSHA, State: "success"}}
	r := Diagnose(s)
	if r.Status != StatusUnknown || r.ExitCode != 3 || !hasCode(r, "CHECK_SOURCE_MISMATCH") {
		t.Fatalf("got status=%s exit=%d findings=%+v", r.Status, r.ExitCode, r.Findings)
	}
}

func TestDiagnoseAppMismatchIsUnknown(t *testing.T) {
	appID := int64(7)
	s := baseSnapshot()
	s.Policy = model.Policy{Known: true, RequiredStatusChecks: []model.RequiredCheck{{Context: "ci", AppID: &appID}}}
	s.CheckRuns = []model.CheckRun{{Name: "ci", SHA: s.HeadSHA, Status: "completed", Conclusion: "success", AppID: ptr(int64(8)), SourceKnown: true}}
	r := Diagnose(s)
	if !hasCode(r, "CHECK_SOURCE_MISMATCH") || r.Status != StatusUnknown || r.ExitCode != 3 {
		t.Fatalf("got status=%s exit=%d findings=%+v", r.Status, r.ExitCode, r.Findings)
	}
}

func TestDiagnoseSelectsTestMergeOnlyWhenItHasEvidence(t *testing.T) {
	s := baseSnapshot()
	s.TestMergeSHA = "merge-sha"
	s.Policy = model.Policy{Known: true, RequiredStatusChecks: []model.RequiredCheck{{Context: "ci"}}}
	s.CheckRuns = []model.CheckRun{{Name: "ci", SHA: s.HeadSHA, Status: "completed", Conclusion: "success", SourceKnown: true}}
	r := Diagnose(s)
	if r.SelectedSHA != s.HeadSHA || r.Status != StatusReady {
		t.Fatalf("got selected=%s status=%s, want head/ready", r.SelectedSHA, r.Status)
	}
	s.CheckRuns = append(s.CheckRuns, model.CheckRun{Name: "ci", SHA: s.TestMergeSHA, Status: "completed", Conclusion: "failure", SourceKnown: true})
	r = Diagnose(s)
	if r.SelectedSHA != s.TestMergeSHA || !hasCode(r, "REQUIRED_CHECK_FAILED") {
		t.Fatalf("got selected=%s findings=%+v", r.SelectedSHA, r.Findings)
	}
}

func baseSnapshot() model.Snapshot {
	return model.Snapshot{SchemaVersion: 1, Repo: "owner/repo", PR: 21, State: "open", HeadSHA: "head-sha", BaseSHA: "base-sha", BaseRef: "master", Mergeable: "MERGEABLE", MergeableState: "clean"}
}

func hasCode(r Report, code string) bool {
	for _, f := range r.Findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

func ptr(v int64) *int64 { return &v }
