package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/amasen02/gh-merge-doctor/internal/collector"
	"github.com/amasen02/gh-merge-doctor/internal/diagnose"
	"github.com/amasen02/gh-merge-doctor/internal/model"
	"github.com/amasen02/gh-merge-doctor/internal/receipt"
)

const version = "0.1.0"

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gh-merge-doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var repo, fixture, receiptPath string
	var pr int
	var demo, jsonOutput, showVersion bool
	fs.StringVar(&repo, "repo", "", "GitHub repository as owner/name")
	fs.IntVar(&pr, "pr", 0, "pull request number")
	fs.StringVar(&fixture, "fixture", "", "read a strict local JSON snapshot")
	fs.StringVar(&receiptPath, "receipt", "", "write a redacted JSON receipt to this path")
	fs.BoolVar(&demo, "demo", false, "run the offline missing-required-context demo")
	fs.BoolVar(&jsonOutput, "json", false, "emit machine-readable JSON")
	fs.BoolVar(&showVersion, "version", false, "print the version")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: gh-merge-doctor --repo owner/name --pr N [--json] [--receipt PATH]")
		fmt.Fprintln(stderr, "       gh-merge-doctor --fixture PATH [--json] [--receipt PATH]")
		fmt.Fprintln(stderr, "       gh-merge-doctor --demo [--json]")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if showVersion {
		fmt.Fprintln(stdout, version)
		return 0
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "unexpected positional argument")
		return 2
	}
	if demo && (fixture != "" || repo != "" || pr != 0) {
		fmt.Fprintln(stderr, "--demo cannot be combined with --fixture, --repo, or --pr")
		return 2
	}
	if fixture != "" && (repo != "" || pr != 0) {
		fmt.Fprintln(stderr, "--fixture cannot be combined with --repo or --pr")
		return 2
	}
	if !demo && fixture == "" && (repo == "" || pr == 0) {
		fs.Usage()
		return 2
	}
	if !demo && fixture == "" {
		if err := collector.ValidateRepo(repo); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 2
		}
		if err := collector.ValidatePR(pr); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 2
		}
	}

	var snapshot model.Snapshot
	var err error
	switch {
	case demo:
		snapshot = demoSnapshot()
	case fixture != "":
		snapshot, err = readFixture(fixture)
	default:
		snapshot, err = collector.Collect(context.Background(), &collector.Runner{}, repo, pr)
	}
	if err != nil {
		if fixture != "" || repo == "" {
			fmt.Fprintln(stderr, "error:", err)
			return 2
		}
		snapshot.Repo, snapshot.PR = repo, pr
		snapshot.Limitations = append(snapshot.Limitations, err.Error())
	}
	r := diagnose.Diagnose(snapshot)
	envelope := receipt.Build(snapshot, r, time.Now().UTC())
	if receiptPath != "" {
		if err := receipt.Write(receiptPath, envelope); err != nil {
			fmt.Fprintln(stderr, "receipt:", err)
			return 2
		}
	}
	if jsonOutput {
		b, _ := json.MarshalIndent(envelope, "", "  ")
		fmt.Fprintln(stdout, string(b))
	} else {
		renderHuman(stdout, envelope)
	}
	return r.ExitCode
}

func demoSnapshot() model.Snapshot {
	return model.Snapshot{SchemaVersion: 1, Repo: "demo/example", PR: 1, State: "open", HeadSHA: "demo-head-sha", BaseSHA: "demo-base-sha", BaseRef: "main", Mergeable: "MERGEABLE", MergeableState: "blocked", Policy: model.Policy{Known: true, RequiredStatusChecks: []model.RequiredCheck{{Context: "ci"}}}, CheckRuns: []model.CheckRun{{Name: "build-test (ubuntu-latest)", SHA: "demo-head-sha", Status: "completed", Conclusion: "success", SourceKnown: true}, {Name: "build-test (windows-latest)", SHA: "demo-head-sha", Status: "completed", Conclusion: "success", SourceKnown: true}}}
}

func readFixture(filename string) (model.Snapshot, error) {
	f, err := os.Open(filename)
	if err != nil {
		return model.Snapshot{}, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 2<<20+1))
	if err != nil {
		return model.Snapshot{}, err
	}
	if len(b) > 2<<20 {
		return model.Snapshot{}, errors.New("fixture exceeds 2 MiB")
	}
	b = []byte(strings.TrimPrefix(string(b), "\ufeff"))
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	var s model.Snapshot
	if err := dec.Decode(&s); err != nil {
		return s, fmt.Errorf("invalid fixture: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return s, errors.New("fixture has trailing JSON")
		}
		return s, fmt.Errorf("invalid fixture trailer: %w", err)
	}
	if err := collector.ValidateRepo(s.Repo); err != nil {
		return s, err
	}
	if err := collector.ValidatePR(s.PR); err != nil {
		return s, err
	}
	if s.SchemaVersion != 1 {
		return s, errors.New("fixture schema_version must be 1")
	}
	if s.HeadSHA == "" || s.BaseSHA == "" {
		return s, errors.New("fixture must include head_sha and base_sha")
	}
	return s, nil
}

func renderHuman(w io.Writer, e receipt.Envelope) {
	fmt.Fprintf(w, "%s (%s)\n", safeText(e.Summary), safeText(e.Status))
	if e.HeadSHA != "" {
		fmt.Fprintf(w, "head: %s\n", safeText(e.HeadSHA))
	}
	if e.SelectedSHA != "" && e.SelectedSHA != e.HeadSHA {
		fmt.Fprintf(w, "evidence SHA: %s\n", safeText(e.SelectedSHA))
	}
	for _, f := range e.Findings {
		fmt.Fprintf(w, "- %s [%s]: %s\n", safeText(f.Code), safeText(f.Severity), safeText(f.Summary))
		for _, n := range f.NextSteps {
			fmt.Fprintf(w, "  next: %s\n", safeText(n))
		}
	}
	for _, l := range e.Limitations {
		fmt.Fprintf(w, "- limitation: %s\n", safeText(l))
	}
	fmt.Fprintf(w, "receipt response sha256: %s\n", safeText(e.ResponseSHA256))
}

func safeText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '\uFFFD'
		}
		return r
	}, s)
}
