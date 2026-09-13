package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestDemoIsOfflineAndReportsMissingRequiredContext(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"--demo", "--json"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit=%d stderr=%s stdout=%s", code, errOut.String(), out.String())
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["status"] != "blocked" || !strings.Contains(out.String(), "MISSING_REQUIRED_CHECK") {
		t.Fatalf("output=%s", out.String())
	}
}

func TestInvalidLiveArgumentsAreUsageErrors(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"--repo", "../repo", "--pr", "1"}, &out, &errOut); code != 2 {
		t.Fatalf("exit=%d stderr=%s", code, errOut.String())
	}
}
