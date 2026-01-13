package jeopardy

import "context"

// Backend is the interface for jeopardy-style CTF platform integrations.
type Backend interface {
	// Fetch retrieves all challenges from the platform.
	Fetch(ctx context.Context) ([]Challenge, error)

	// Submit attempts to submit a flag for the given challenge.
	Submit(ctx context.Context, challengeID, flag string) (*SubmitResult, error)

	// Solves returns the list of solved challenges.
	// Returns empty slice if not supported by the platform.
	Solves(ctx context.Context) ([]Solve, error)
}
