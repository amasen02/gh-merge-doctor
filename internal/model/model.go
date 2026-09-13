package model

// Snapshot is the normalized, non-secret evidence used by the diagnosis engine.
// It is also the strict fixture format; fields omitted by a live API are
// represented by zero values and a corresponding limitation.
type Snapshot struct {
	SchemaVersion  int    `json:"schema_version"`
	Repo           string `json:"repo"`
	PR             int    `json:"pr"`
	State          string `json:"state"`
	Merged         bool   `json:"merged"`
	Draft          bool   `json:"draft"`
	HeadSHA        string `json:"head_sha"`
	BaseSHA        string `json:"base_sha"`
	BaseRef        string `json:"base_ref"`
	TestMergeSHA   string `json:"test_merge_sha,omitempty"`
	Mergeable      string `json:"mergeable,omitempty"`
	MergeableState string `json:"mergeable_state,omitempty"`
	ReviewDecision string `json:"review_decision,omitempty"`
	HeadChanged    bool   `json:"head_changed,omitempty"`

	Policy      Policy     `json:"policy"`
	CheckRuns   []CheckRun `json:"check_runs,omitempty"`
	Statuses    []Status   `json:"statuses,omitempty"`
	SourceURLs  []string   `json:"source_urls,omitempty"`
	Limitations []string   `json:"limitations,omitempty"`
}

type Policy struct {
	Known                bool            `json:"known"`
	RequiredStatusChecks []RequiredCheck `json:"required_status_checks,omitempty"`
	Strict               bool            `json:"strict,omitempty"`
}

type RequiredCheck struct {
	Context string `json:"context"`
	AppID   *int64 `json:"app_id,omitempty"`
}

type CheckRun struct {
	Name        string `json:"name"`
	SHA         string `json:"sha"`
	Status      string `json:"status"`
	Conclusion  string `json:"conclusion,omitempty"`
	AppID       *int64 `json:"app_id,omitempty"`
	SourceKnown bool   `json:"source_known"`
	ID          int64  `json:"id,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	URL         string `json:"url,omitempty"`
}

type Status struct {
	Context   string `json:"context"`
	SHA       string `json:"sha"`
	State     string `json:"state"`
	Creator   string `json:"creator,omitempty"`
	ID        int64  `json:"id,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
	URL       string `json:"url,omitempty"`
}
