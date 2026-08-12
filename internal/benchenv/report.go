package benchenv

// Status represents the health status of a diagnostic check.
type Status string

const (
	StatusOK          Status = "ok"
	StatusWarn        Status = "warn"
	StatusUnavailable Status = "unavailable"
)

// Check is the result of a single diagnostic probe.
type Check struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Detail string `json:"detail"`
	Remedy string `json:"remedy,omitempty"`
}

// Summary counts check results by status.
type Summary struct {
	OK          int `json:"ok"`
	Warn        int `json:"warn"`
	Unavailable int `json:"unavailable"`
}

// Report is the top-level structure for JSON output.
type Report struct {
	OS      string  `json:"os"`
	Arch    string  `json:"arch"`
	NumCPU  int     `json:"numcpu"`
	Checks  []Check `json:"checks"`
	Summary Summary `json:"summary"`
}

// Summarize counts checks by status.
func Summarize(checks []Check) Summary {
	var summary Summary
	for _, check := range checks {
		switch check.Status {
		case StatusOK:
			summary.OK++
		case StatusWarn:
			summary.Warn++
		case StatusUnavailable:
			summary.Unavailable++
		}
	}
	return summary
}
