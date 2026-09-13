package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/amasen02/gh-merge-doctor/internal/diagnose"
	"github.com/amasen02/gh-merge-doctor/internal/receipt"
)

const (
	testRepo  = "owner/repo"
	testPR    = 42
	testHead  = "1111111111111111111111111111111111111111"
	testBase  = "2222222222222222222222222222222222222222"
	testMerge = "3333333333333333333333333333333333333333"
)

type scriptedResponse struct {
	args []string
	body []byte
	err  error
}

type fakeCaller struct {
	t     *testing.T
	steps []scriptedResponse
	calls [][]string
}

func (f *fakeCaller) Call(_ context.Context, args ...string) ([]byte, error) {
	f.t.Helper()
	f.calls = append(f.calls, append([]string(nil), args...))
	if len(f.steps) == 0 {
		f.t.Fatalf("unexpected extra gh call: %q", args)
	}
	step := f.steps[0]
	f.steps = f.steps[1:]
	if !reflect.DeepEqual(args, step.args) {
		f.t.Fatalf("gh argv mismatch\n got: %#v\nwant: %#v", args, step.args)
	}
	return step.body, step.err
}

func (f *fakeCaller) done() {
	f.t.Helper()
	if len(f.steps) != 0 {
		f.t.Fatalf("%d scripted gh calls were not consumed", len(f.steps))
	}
}

func TestCollectClassicRequiredCheckDecodesSnakeCaseEvidenceAndDiagnosesReady(t *testing.T) {
	statusBody := statusJSON("lint", "success", "github-actions[bot]", 91)
	checkBody := checkJSON(testHead, "ci", "completed", "success", 17, 123)
	fake := &fakeCaller{t: t, steps: captureSteps(testHead, testBase, "", testHead, map[string][]byte{
		"checks:" + testHead: checkBody,
		"status:" + testHead: statusBody,
	})}

	snapshot, err := Collect(context.Background(), fake, testRepo, testPR)
	if err != nil {
		t.Fatal(err)
	}
	fake.done()

	if !snapshot.Policy.Known || len(snapshot.Policy.RequiredStatusChecks) != 1 || snapshot.Policy.RequiredStatusChecks[0].Context != "ci" {
		t.Fatalf("classic required-check policy was not decoded: %+v", snapshot.Policy)
	}
	if len(snapshot.CheckRuns) != 1 {
		t.Fatalf("check-runs were not decoded: %+v", snapshot.CheckRuns)
	}
	check := snapshot.CheckRuns[0]
	if check.Name != "ci" || check.SHA != testHead || check.Status != "completed" || check.Conclusion != "success" || check.ID != 17 || check.AppID == nil || *check.AppID != 123 {
		t.Fatalf("snake_case check-run response was not normalized: %+v", check)
	}
	if len(snapshot.Statuses) != 1 {
		t.Fatalf("statuses were not decoded: %+v", snapshot.Statuses)
	}
	status := snapshot.Statuses[0]
	if status.Context != "lint" || status.SHA != testHead || status.State != "success" || status.Creator != "github-actions[bot]" || status.ID != 91 || status.UpdatedAt == "" {
		t.Fatalf("snake_case status response was not normalized: %+v", status)
	}

	report := diagnose.Diagnose(snapshot)
	if report.Status != diagnose.StatusReady || report.ExitCode != 0 {
		t.Fatalf("got diagnosis status=%s exit=%d findings=%+v limitations=%v", report.Status, report.ExitCode, report.Findings, report.Limitations)
	}
}

func TestCollectPolicy403And404RemainUnknown(t *testing.T) {
	for _, statusCode := range []string{"403", "404"} {
		t.Run(statusCode, func(t *testing.T) {
			failure := errors.New("gh: HTTP " + statusCode)
			fake := &fakeCaller{t: t, steps: captureStepsWithPolicyErrors(testHead, testBase, testHead, failure)}
			snapshot, err := Collect(context.Background(), fake, testRepo, testPR)
			if err != nil {
				t.Fatal(err)
			}
			fake.done()
			if snapshot.Policy.Known {
				t.Fatalf("policy became known after HTTP %s: %+v", statusCode, snapshot.Policy)
			}
			report := diagnose.Diagnose(snapshot)
			if report.Status != diagnose.StatusUnknown || report.ExitCode != 3 {
				t.Fatalf("HTTP %s got status=%s exit=%d limitations=%v", statusCode, report.Status, report.ExitCode, report.Limitations)
			}
		})
	}
}

func TestCollectThreeFullCheckPagesRemainUnknown(t *testing.T) {
	// A full page must request the next page; replace the first check page with
	// three exact pages so the bounded collector proves it stops at page three.
	checkPage := checkPageJSON(testHead, 100, 1)
	steps := captureStepsWithoutEvidence(testHead, testBase, "", testHead)
	steps = append(steps[:4],
		scriptedResponse{args: apiArgs(fmt.Sprintf("/repos/%s/commits/%s/check-runs?per_page=100&page=1", testRepo, testHead)), body: checkPage},
		scriptedResponse{args: apiArgs(fmt.Sprintf("/repos/%s/commits/%s/check-runs?per_page=100&page=2", testRepo, testHead)), body: checkPageJSON(testHead, 100, 101)},
		scriptedResponse{args: apiArgs(fmt.Sprintf("/repos/%s/commits/%s/check-runs?per_page=100&page=3", testRepo, testHead)), body: checkPageJSON(testHead, 100, 201)},
		scriptedResponse{args: apiArgs(fmt.Sprintf("/repos/%s/commits/%s/status?per_page=100&page=1", testRepo, testHead)), body: []byte(`{"statuses":[]}`)},
		steps[len(steps)-1],
	)
	fake := &fakeCaller{t: t, steps: steps}
	snapshot, err := Collect(context.Background(), fake, testRepo, testPR)
	if err != nil {
		t.Fatal(err)
	}
	fake.done()
	if countCalls(fake.calls, "check-runs") != 3 {
		t.Fatalf("got %d check-run pages, want 3: %v", countCalls(fake.calls, "check-runs"), fake.calls)
	}
	report := diagnose.Diagnose(snapshot)
	if report.Status != diagnose.StatusUnknown || report.ExitCode != 3 {
		t.Fatalf("full-page truncation got status=%s exit=%d limitations=%v", report.Status, report.ExitCode, report.Limitations)
	}
	if !containsText(report.Limitations, "bounded page limit") {
		t.Fatalf("truncation limitation missing: %v", report.Limitations)
	}
}

func TestCollectThreeFullStatusPagesRemainUnknown(t *testing.T) {
	steps := captureStepsWithoutEvidence(testHead, testBase, "", testHead)
	steps = append(steps[:4],
		scriptedResponse{args: apiArgs(fmt.Sprintf("/repos/%s/commits/%s/check-runs?per_page=100&page=1", testRepo, testHead)), body: []byte(`{"check_runs":[]}`)},
		scriptedResponse{args: apiArgs(fmt.Sprintf("/repos/%s/commits/%s/status?per_page=100&page=1", testRepo, testHead)), body: statusPageJSON(testHead, 100, 1)},
		scriptedResponse{args: apiArgs(fmt.Sprintf("/repos/%s/commits/%s/status?per_page=100&page=2", testRepo, testHead)), body: statusPageJSON(testHead, 100, 101)},
		scriptedResponse{args: apiArgs(fmt.Sprintf("/repos/%s/commits/%s/status?per_page=100&page=3", testRepo, testHead)), body: statusPageJSON(testHead, 100, 201)},
		steps[len(steps)-1],
	)
	fake := &fakeCaller{t: t, steps: steps}
	snapshot, err := Collect(context.Background(), fake, testRepo, testPR)
	if err != nil {
		t.Fatal(err)
	}
	fake.done()
	if countCalls(fake.calls, "/status?") != 3 {
		t.Fatalf("got %d status pages, want 3: %v", countCalls(fake.calls, "/status?"), fake.calls)
	}
	report := diagnose.Diagnose(snapshot)
	if report.Status != diagnose.StatusUnknown || report.ExitCode != 3 {
		t.Fatalf("full-page status truncation got status=%s exit=%d limitations=%v", report.Status, report.ExitCode, report.Limitations)
	}
	if !containsText(report.Limitations, "statuses exceeded the bounded page limit") {
		t.Fatalf("status truncation limitation missing: %v", report.Limitations)
	}
}

func TestCollectMalformedEvidencePreservesUnknown(t *testing.T) {
	fake := &fakeCaller{t: t, steps: captureSteps(testHead, testBase, "", testHead, map[string][]byte{
		"checks:" + testHead: []byte(`{"check_runs":`),
		"status:" + testHead: []byte(`{"statuses":[]}`),
	})}
	snapshot, err := Collect(context.Background(), fake, testRepo, testPR)
	if err != nil {
		t.Fatal(err)
	}
	fake.done()
	report := diagnose.Diagnose(snapshot)
	if report.Status != diagnose.StatusUnknown || report.ExitCode != 3 {
		t.Fatalf("malformed evidence got status=%s exit=%d limitations=%v", report.Status, report.ExitCode, report.Limitations)
	}
	if !containsText(report.Limitations, "check runs response was invalid") {
		t.Fatalf("malformed-response limitation missing: %v", report.Limitations)
	}
}

func TestCollectDoesNotCarryStatusTargetURLIntoStatusOrReceipt(t *testing.T) {
	secretMarker := "TOKEN-MUST-NOT-APPEAR"
	fake := &fakeCaller{t: t, steps: captureSteps(testHead, testBase, "", testHead, map[string][]byte{
		"checks:" + testHead: []byte(`{"check_runs":[]}`),
		"status:" + testHead: statusJSONWithTarget("ci", "success", "github-actions[bot]", "https://secret.invalid/"+secretMarker, 92),
	})}
	snapshot, err := Collect(context.Background(), fake, testRepo, testPR)
	if err != nil {
		t.Fatal(err)
	}
	fake.done()
	if len(snapshot.Statuses) != 1 || strings.Contains(snapshot.Statuses[0].URL, secretMarker) {
		t.Fatalf("status target URL leaked into normalized evidence: %+v", snapshot.Statuses)
	}
	envelope := receipt.Build(snapshot, diagnose.Diagnose(snapshot), time.Unix(0, 0).UTC())
	b, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), secretMarker) {
		t.Fatalf("status target URL leaked into receipt: %s", b)
	}
}

func TestCollectHeadBaseAndMergeSHAChangeDuringCaptureIsUnknown(t *testing.T) {
	changedHead := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	changedBase := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	changedMerge := "cccccccccccccccccccccccccccccccccccccccc"
	steps := captureSteps(testHead, testBase, testMerge, changedHead, map[string][]byte{
		"checks:" + testHead:  []byte(`{"check_runs":[]}`),
		"status:" + testHead:  []byte(`{"statuses":[]}`),
		"checks:" + testMerge: checkJSON(testMerge, "ci", "completed", "success", 18, 123),
		"status:" + testMerge: []byte(`{"statuses":[]}`),
	})
	// Keep the base and merge SHA changed as well; the collector must reject
	// this capture even though it found valid evidence for the test merge SHA.
	steps[len(steps)-1].body = pullJSON(changedHead, changedBase, changedMerge)
	fake := &fakeCaller{t: t, steps: steps}
	snapshot, err := Collect(context.Background(), fake, testRepo, testPR)
	if err != nil {
		t.Fatal(err)
	}
	fake.done()
	if !snapshot.HeadChanged {
		t.Fatalf("capture race was not recorded: %+v", snapshot)
	}
	report := diagnose.Diagnose(snapshot)
	if report.Status != diagnose.StatusUnknown || report.ExitCode != 3 {
		t.Fatalf("capture race got status=%s exit=%d limitations=%v", report.Status, report.ExitCode, report.Limitations)
	}
}

func TestCollectUsesTestMergeEvidenceWhenAvailable(t *testing.T) {
	fake := &fakeCaller{t: t, steps: captureSteps(testHead, testBase, testMerge, testHead, map[string][]byte{
		"checks:" + testHead:  []byte(`{"check_runs":[]}`),
		"status:" + testHead:  []byte(`{"statuses":[]}`),
		"checks:" + testMerge: checkJSON(testMerge, "ci", "completed", "success", 19, 123),
		"status:" + testMerge: []byte(`{"statuses":[]}`),
	})}
	snapshot, err := Collect(context.Background(), fake, testRepo, testPR)
	if err != nil {
		t.Fatal(err)
	}
	fake.done()
	if len(snapshot.CheckRuns) != 1 || snapshot.CheckRuns[0].SHA != testMerge {
		t.Fatalf("test-merge evidence was not collected: %+v", snapshot.CheckRuns)
	}
	report := diagnose.Diagnose(snapshot)
	if report.SelectedSHA != testMerge || report.Status != diagnose.StatusReady || report.ExitCode != 0 {
		t.Fatalf("test-merge precedence got selected=%s status=%s exit=%d findings=%+v limitations=%v", report.SelectedSHA, report.Status, report.ExitCode, report.Findings, report.Limitations)
	}
}

func captureSteps(head, base, merge, secondHead string, evidence map[string][]byte) []scriptedResponse {
	steps := captureStepsWithoutEvidence(head, base, merge, secondHead)
	insert := 4
	for _, sha := range []string{head, merge} {
		if sha == "" || sha == head && merge == head {
			continue
		}
		steps = append(steps[:insert], append([]scriptedResponse{
			{args: apiArgs(fmt.Sprintf("/repos/%s/commits/%s/check-runs?per_page=100&page=1", testRepo, sha)), body: evidence["checks:"+sha]},
			{args: apiArgs(fmt.Sprintf("/repos/%s/commits/%s/status?per_page=100&page=1", testRepo, sha)), body: evidence["status:"+sha]},
		}, steps[insert:]...)...)
		insert += 2
	}
	return steps
}

func captureStepsWithoutEvidence(head, base, merge, secondHead string) []scriptedResponse {
	return []scriptedResponse{
		{args: apiArgs(fmt.Sprintf("/repos/%s/pulls/%d", testRepo, testPR)), body: pullJSON(head, base, merge)},
		{args: []string{"pr", "view", "--repo", "github.com/" + testRepo, "42", "--json", "mergeStateStatus,reviewDecision"}, body: []byte(`{"mergeStateStatus":"CLEAN","reviewDecision":""}`)},
		{args: apiArgs("/repos/" + testRepo + "/branches/main/protection"), body: []byte(`{"required_status_checks":{"strict":true,"contexts":["ci"],"checks":[]}}`)},
		{args: apiArgs("/repos/" + testRepo + "/rules/branches/main"), body: []byte(`[]`)},
		{args: apiArgs(fmt.Sprintf("/repos/%s/pulls/%d", testRepo, testPR)), body: pullJSON(secondHead, base, merge)},
	}
}

func captureStepsWithPolicyErrors(head, base, secondHead string, policyErr error) []scriptedResponse {
	steps := captureSteps(head, base, "", secondHead, map[string][]byte{
		"checks:" + head: []byte(`{"check_runs":[]}`),
		"status:" + head: []byte(`{"statuses":[]}`),
	})
	steps[2].body, steps[2].err = nil, policyErr
	steps[3].body, steps[3].err = nil, policyErr
	return steps
}

func apiArgs(endpoint string) []string {
	return []string{"api", "--hostname", "github.com", "--method", "GET", endpoint}
}

func pullJSON(head, base, merge string) []byte {
	return []byte(fmt.Sprintf(`{"state":"open","merged":false,"draft":false,"head":{"sha":%q},"base":{"sha":%q,"ref":"main"},"merge_commit_sha":%q,"mergeable":true,"mergeable_state":"clean"}`, head, base, merge))
}

func checkJSON(sha, name, status, conclusion string, id, appID int64) []byte {
	return checkPageJSONWithEntries(sha, []map[string]any{{"name": name, "status": status, "conclusion": conclusion, "id": id, "head_sha": sha, "app": map[string]any{"id": appID}, "started_at": "2026-09-13T00:00:00Z", "completed_at": "2026-09-13T00:01:00Z", "html_url": "https://github.com/owner/repo/runs/" + fmt.Sprint(id)}})
}

func checkPageJSON(sha string, count int, startID int64) []byte {
	entries := make([]map[string]any, count)
	for i := range entries {
		entries[i] = map[string]any{"name": fmt.Sprintf("noise-%d", i), "status": "completed", "conclusion": "success", "id": startID + int64(i), "head_sha": sha, "app": map[string]any{"id": 123}}
	}
	return checkPageJSONWithEntries(sha, entries)
}

func checkPageJSONWithEntries(_ string, entries []map[string]any) []byte {
	body, err := json.Marshal(map[string]any{"check_runs": entries})
	if err != nil {
		panic(err)
	}
	return body
}

func statusJSON(context, state, creator string, id int64) []byte {
	return statusJSONWithTarget(context, state, creator, "https://example.invalid/status", id)
}

func statusJSONWithTarget(context, state, creator, targetURL string, id int64) []byte {
	body, err := json.Marshal(map[string]any{"statuses": []map[string]any{{"context": context, "state": state, "target_url": targetURL, "id": id, "creator": map[string]any{"login": creator}, "updated_at": "2026-09-13T00:02:00Z"}}})
	if err != nil {
		panic(err)
	}
	return body
}

func statusPageJSON(sha string, count int, startID int64) []byte {
	statuses := make([]map[string]any, count)
	for i := range statuses {
		statuses[i] = map[string]any{
			"context":    fmt.Sprintf("noise-%d", i),
			"state":      "success",
			"target_url": "https://example.invalid/status",
			"id":         startID + int64(i),
			"creator":    map[string]any{"login": "github-actions[bot]"},
			"updated_at": "2026-09-13T00:02:00Z",
			"sha":        sha,
		}
	}
	body, err := json.Marshal(map[string]any{"statuses": statuses})
	if err != nil {
		panic(err)
	}
	return body
}

func countCalls(calls [][]string, fragment string) int {
	count := 0
	for _, call := range calls {
		if strings.Contains(strings.Join(call, " "), fragment) {
			count++
		}
	}
	return count
}

func containsText(values []string, fragment string) bool {
	for _, value := range values {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}
