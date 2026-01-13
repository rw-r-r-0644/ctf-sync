package jeopardy

import (
	"context"
	"time"
)

// Challenge represents a CTF challenge.
type Challenge struct {
	ID          string
	Name        string
	Category    string
	Description string
	Points      int
	Tags        []string
	Files       []File
	Solved      bool
}

// File represents a challenge attachment.
// Implementations hold backend-specific state for authenticated downloads.
type File interface {
	Name() string
	DownloadURL(ctx context.Context) (*DownloadInfo, error)
}

// DownloadInfo contains the URL and headers needed to download a file.
type DownloadInfo struct {
	URL     string
	Headers map[string]string
}

// SubmitStatus represents the result of a flag submission.
type SubmitStatus string

const (
	Accepted    SubmitStatus = "accepted"
	Rejected    SubmitStatus = "rejected"
	Duplicate   SubmitStatus = "duplicate"
	RateLimited SubmitStatus = "rate_limited"
	Pending     SubmitStatus = "pending"
	Error       SubmitStatus = "error"
)

// SubmitResult is the outcome of a flag submission.
type SubmitResult struct {
	Status  SubmitStatus
	Message string
}

// Solve represents a solved challenge.
type Solve struct {
	ChallengeID string
	SolvedAt    *time.Time
}
