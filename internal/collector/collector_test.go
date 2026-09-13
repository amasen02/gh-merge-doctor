package collector

import "testing"

func TestValidateRepoRejectsPathAndOptionInjection(t *testing.T) {
	for _, repo := range []string{"../repo", "owner/..", "-owner/repo", "owner/-repo", "owner/repo\nnext", "https://github.com/owner/repo"} {
		if ValidateRepo(repo) == nil {
			t.Errorf("ValidateRepo(%q) accepted unsafe repo", repo)
		}
	}
	for _, repo := range []string{"owner/repo", "org_name/project.name", "a-b/c_d"} {
		if err := ValidateRepo(repo); err != nil {
			t.Errorf("ValidateRepo(%q): %v", repo, err)
		}
	}
}

func TestValidatePRIsBounded(t *testing.T) {
	for _, pr := range []int{0, -1, 1000000001} {
		if ValidatePR(pr) == nil {
			t.Errorf("ValidatePR(%d) accepted", pr)
		}
	}
	if err := ValidatePR(1); err != nil {
		t.Fatal(err)
	}
}
