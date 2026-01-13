package script

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/rw-r-r-0644/ctf-sync/jeopardy"
)

func init() {
	jeopardy.Register(jeopardy.BackendDef{
		ID:   "script",
		Name: "Custom Script",
		Settings: []jeopardy.SettingDef{
			{ID: "command", Name: "Command", Required: true},
		},
		Build: func(s map[string]string) (jeopardy.Backend, error) {
			return newScript(s["command"])
		},
	})
}

type scriptClient struct {
	command []string
	timeout time.Duration
}

func newScript(command string) (*scriptClient, error) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return nil, fmt.Errorf("command is required")
	}
	return &scriptClient{
		command: parts,
		timeout: 2 * time.Minute,
	}, nil
}

func (c *scriptClient) Fetch(ctx context.Context) ([]jeopardy.Challenge, error) {
	payload := scriptFetchRequest{Action: "fetch"}
	output, err := c.run(ctx, payload)
	if err != nil {
		return nil, err
	}

	var resp scriptFetchResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		return nil, fmt.Errorf("parse fetch result: %w", err)
	}

	challenges := make([]jeopardy.Challenge, 0, len(resp.Challenges))
	for _, ch := range resp.Challenges {
		challenge := jeopardy.Challenge{
			ID:          ch.ID,
			Name:        ch.Name,
			Category:    ch.Category,
			Description: ch.Description,
			Points:      ch.Points,
		}
		if len(ch.Files) > 0 {
			challenge.Files = make([]jeopardy.File, 0, len(ch.Files))
			for _, f := range ch.Files {
				challenge.Files = append(challenge.Files, &scriptFile{
					name:    f.Name,
					url:     f.URL,
					headers: f.Headers,
				})
			}
		}
		challenges = append(challenges, challenge)
	}
	return challenges, nil
}

func (c *scriptClient) Submit(ctx context.Context, challengeID, flag string) (*jeopardy.SubmitResult, error) {
	payload := scriptSubmitRequest{
		Action:      "submit",
		ChallengeID: challengeID,
		Flag:        flag,
	}
	output, err := c.run(ctx, payload)
	if err != nil {
		return nil, err
	}

	var resp scriptSubmitResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		return nil, fmt.Errorf("parse submit result: %w", err)
	}

	status := parseSubmitStatus(resp.Status)
	return &jeopardy.SubmitResult{Status: status, Message: resp.Message}, nil
}

func (c *scriptClient) Solves(ctx context.Context) ([]jeopardy.Solve, error) {
	payload := scriptSolvesRequest{Action: "solves"}
	output, err := c.run(ctx, payload)
	if err != nil {
		return nil, err
	}

	var resp scriptSolvesResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		return nil, fmt.Errorf("parse solves result: %w", err)
	}

	solves := make([]jeopardy.Solve, 0, len(resp.Solves))
	for _, s := range resp.Solves {
		solve := jeopardy.Solve{ChallengeID: s.ChallengeID}
		if s.SolvedAt != nil {
			solve.SolvedAt = s.SolvedAt
		}
		solves = append(solves, solve)
	}
	return solves, nil
}

func (c *scriptClient) run(ctx context.Context, payload any) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode script request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.command[0], c.command[1:]...)
	cmd.Stdin = bytes.NewReader(data)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			if stderr != "" {
				return nil, fmt.Errorf("script error: %s", stderr)
			}
		}
		return nil, fmt.Errorf("script error: %w", err)
	}
	return output, nil
}

func parseSubmitStatus(s string) jeopardy.SubmitStatus {
	switch strings.ToLower(s) {
	case "accepted":
		return jeopardy.Accepted
	case "rejected":
		return jeopardy.Rejected
	case "duplicate":
		return jeopardy.Duplicate
	case "rate_limited":
		return jeopardy.RateLimited
	case "pending":
		return jeopardy.Pending
	default:
		return jeopardy.Error
	}
}

type scriptFile struct {
	name    string
	url     string
	headers map[string]string
}

func (f *scriptFile) Name() string { return f.name }

func (f *scriptFile) DownloadURL(ctx context.Context) (*jeopardy.DownloadInfo, error) {
	return &jeopardy.DownloadInfo{URL: f.url, Headers: f.headers}, nil
}

type scriptFetchRequest struct {
	Action string `json:"action"`
}

type scriptFetchResponse struct {
	Challenges []scriptChallenge `json:"challenges"`
}

type scriptChallenge struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Category    string           `json:"category"`
	Description string           `json:"description"`
	Points      int              `json:"points"`
	Files       []scriptFileInfo `json:"files"`
}

type scriptFileInfo struct {
	Name    string            `json:"name"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

type scriptSubmitRequest struct {
	Action      string `json:"action"`
	ChallengeID string `json:"challenge_id"`
	Flag        string `json:"flag"`
}

type scriptSubmitResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type scriptSolvesRequest struct {
	Action string `json:"action"`
}

type scriptSolvesResponse struct {
	Solves []scriptSolve `json:"solves"`
}

type scriptSolve struct {
	ChallengeID string     `json:"challenge_id"`
	SolvedAt    *time.Time `json:"solved_at"`
}
