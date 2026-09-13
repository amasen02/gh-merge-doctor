package receipt

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/amasen02/gh-merge-doctor/internal/diagnose"
	"github.com/amasen02/gh-merge-doctor/internal/model"
)

func TestBuildHashIgnoresObservationTime(t *testing.T) {
	s := model.Snapshot{Repo: "owner/repo", PR: 1, HeadSHA: "abc", SourceURLs: []string{"https://github.com/owner/repo"}}
	r := diagnose.Report{Status: diagnose.StatusReady, Summary: "ready", HeadSHA: "abc"}
	a, b := Build(s, r, time.Unix(1, 0)), Build(s, r, time.Unix(2, 0))
	if a.ResponseSHA256 == "" || a.ResponseSHA256 != b.ResponseSHA256 {
		t.Fatalf("hashes differ: %q %q", a.ResponseSHA256, b.ResponseSHA256)
	}
	if a.ObservedAt == b.ObservedAt {
		t.Fatal("observation times unexpectedly equal")
	}
}

func TestBuildHashUsesSelectedEvidenceOnlyAndIsOrderStable(t *testing.T) {
	s := model.Snapshot{Repo: "owner/repo", PR: 1, HeadSHA: "abc", CheckRuns: []model.CheckRun{{Name: "ci", SHA: "abc", Status: "completed", Conclusion: "success", ID: 1}, {Name: "optional", SHA: "other", Status: "completed", Conclusion: "failure", ID: 2}}}
	r := diagnose.Report{Status: diagnose.StatusReady, Summary: "ready", HeadSHA: "abc", SelectedSHA: "abc"}
	a := Build(s, r, time.Now()).ResponseSHA256
	s.CheckRuns[1].Conclusion = "success"
	if got := Build(s, r, time.Now()).ResponseSHA256; got != a {
		t.Fatalf("unselected evidence changed hash: %s -> %s", a, got)
	}
	s.CheckRuns[0].Conclusion = "failure"
	if got := Build(s, r, time.Now()).ResponseSHA256; got == a {
		t.Fatal("selected evidence did not change hash")
	}
	s.CheckRuns = []model.CheckRun{s.CheckRuns[1], s.CheckRuns[0]}
	if got := Build(s, r, time.Now()).ResponseSHA256; got == a {
		t.Fatal("changed selected evidence unexpectedly restored original hash")
	}
}

func TestWriteRefusesExistingReceipt(t *testing.T) {
	filename := t.TempDir() + string(os.PathSeparator) + "receipt.json"
	e := Build(model.Snapshot{Repo: "owner/repo", PR: 1, HeadSHA: "abc"}, diagnose.Report{Status: diagnose.StatusReady, Summary: "ready", HeadSHA: "abc"}, time.Now())
	if err := Write(filename, e); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(filename, e); err == nil {
		t.Fatal("second write unexpectedly succeeded")
	}
	got, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatal("existing receipt changed")
	}
	if strings.TrimSpace(string(got)) == "" {
		t.Fatal("receipt is empty")
	}
}
