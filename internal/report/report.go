package report

import "time"

type Result struct {
	Name      string
	Type      string
	StartedAt time.Time
	Duration  time.Duration
	Files     int
	Err       error
}

func (r Result) Successful() bool { return r.Err == nil }
