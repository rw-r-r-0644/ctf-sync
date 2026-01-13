package jeopardy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func init() {
	Register(BackendDef{
		ID:   "rctf",
		Name: "rCTF",
		Settings: []SettingDef{
			{ID: "base_url", Name: "Base URL", Required: true},
			{ID: "team_token", Name: "Team Token", Required: true},
		},
		Build: func(s map[string]string) (Backend, error) {
			return newRCTF(s["base_url"], s["team_token"])
		},
	})
}

type rctfClient struct {
	baseURL   string
	teamToken string
	authToken string
	client    *http.Client
}

type rctfFile struct {
	name string
	url  string
}

func (f *rctfFile) Name() string { return f.name }

func (f *rctfFile) DownloadURL(ctx context.Context) (*DownloadInfo, error) {
	return &DownloadInfo{URL: f.url}, nil
}

func newRCTF(baseURL, teamToken string) (*rctfClient, error) {
	return &rctfClient{
		baseURL:   strings.TrimRight(baseURL, "/"),
		teamToken: teamToken,
		client:    &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (c *rctfClient) Fetch(ctx context.Context) ([]Challenge, error) {
	authToken, err := c.login(ctx)
	if err != nil {
		return nil, err
	}

	challenges, err := c.fetchChallenges(ctx, authToken)
	if err != nil {
		return nil, err
	}

	results := make([]Challenge, 0, len(challenges))
	for _, chal := range challenges {
		challenge := Challenge{
			ID:          chal.ID,
			Name:        chal.Name,
			Category:    chal.Category,
			Description: chal.Description,
			Points:      chal.Points,
		}

		if len(chal.Files) > 0 {
			challenge.Files = make([]File, 0, len(chal.Files))
			for _, file := range chal.Files {
				challenge.Files = append(challenge.Files, &rctfFile{
					name: file.Name,
					url:  file.URL,
				})
			}
		}
		results = append(results, challenge)
	}
	return results, nil
}

func (c *rctfClient) Submit(ctx context.Context, challengeID, flag string) (*SubmitResult, error) {
	if flag == "" {
		return nil, fmt.Errorf("flag is required")
	}
	if challengeID == "" {
		return nil, fmt.Errorf("challenge ID is required")
	}

	authToken, err := c.login(ctx)
	if err != nil {
		return nil, err
	}

	payload := rctfSubmitRequest{Flag: flag}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode submission: %w", err)
	}

	reqURL := fmt.Sprintf("%s/api/v1/challs/%s/submit", c.baseURL, challengeID)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+authToken)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rctf submission failed: %s", strings.TrimSpace(string(respBody)))
	}

	var parsed rctfSubmitResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("parse rctf response: %w", err)
	}

	return c.parseSubmitResponse(parsed), nil
}

func (c *rctfClient) Solves(ctx context.Context) ([]Solve, error) {
	authToken, err := c.login(ctx)
	if err != nil {
		return nil, err
	}

	solves, err := c.fetchUserSolves(ctx, authToken)
	if err != nil {
		return nil, err
	}

	results := make([]Solve, 0, len(solves))
	for _, solve := range solves {
		solvedAt := time.Unix(solve.CreatedAt, 0).UTC()
		results = append(results, Solve{
			ChallengeID: solve.ID,
			SolvedAt:    &solvedAt,
		})
	}
	return results, nil
}

func (c *rctfClient) login(ctx context.Context) (string, error) {
	if c.authToken != "" {
		return c.authToken, nil
	}

	payload := rctfLoginRequest{TeamToken: c.teamToken}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode login request: %w", err)
	}

	loginURL := c.baseURL + "/api/v1/auth/login"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", loginURL, bytes.NewBuffer(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("rctf login failed: %s", strings.TrimSpace(string(respBody)))
	}

	var parsed rctfLoginResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("parse rctf login response: %w", err)
	}

	if parsed.Kind != "goodLogin" {
		return "", fmt.Errorf("rctf login failed: %s", parsed.Message)
	}

	c.authToken = parsed.Data.AuthToken
	return c.authToken, nil
}

func (c *rctfClient) fetchChallenges(ctx context.Context, authToken string) ([]rctfChallenge, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/v1/challs", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+authToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("rctf challenges fetch failed: %s", strings.TrimSpace(string(body)))
	}

	var payload rctfChallengesResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	if payload.Kind != "goodChallenges" {
		return nil, fmt.Errorf("rctf challenges error: %s", payload.Message)
	}

	return payload.Data, nil
}

func (c *rctfClient) fetchUserSolves(ctx context.Context, authToken string) ([]rctfUserSolve, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/v1/users/me", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+authToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("rctf user profile fetch failed: %s", strings.TrimSpace(string(body)))
	}

	var payload rctfUserProfileResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	if payload.Kind != "goodUserSelfData" {
		return nil, fmt.Errorf("rctf user profile error: %s", payload.Message)
	}

	return payload.Data.Solves, nil
}

func (c *rctfClient) parseSubmitResponse(parsed rctfSubmitResponse) *SubmitResult {
	kind := strings.ToLower(strings.TrimSpace(parsed.Kind))
	message := strings.TrimSpace(parsed.Message)

	switch kind {
	case "goodflag":
		return &SubmitResult{Status: Accepted, Message: message}
	case "badflag":
		return &SubmitResult{Status: Rejected, Message: message}
	case "badalreadysolvedchallenge":
		return &SubmitResult{Status: Duplicate, Message: message}
	case "badratelimit":
		return &SubmitResult{Status: RateLimited, Message: message}
	case "badnotstarted":
		return &SubmitResult{Status: Error, Message: "CTF has not started yet: " + message}
	default:
		return &SubmitResult{Status: Error, Message: message}
	}
}

type rctfChallenge struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Points      int    `json:"points"`
	Files       []struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"files"`
}

type rctfLoginRequest struct {
	TeamToken string `json:"teamToken"`
}

type rctfLoginResponse struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Data    struct {
		AuthToken string `json:"authToken"`
	} `json:"data"`
}

type rctfChallengesResponse struct {
	Kind    string          `json:"kind"`
	Message string          `json:"message"`
	Data    []rctfChallenge `json:"data"`
}

type rctfSubmitRequest struct {
	Flag string `json:"flag"`
}

type rctfSubmitResponse struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type rctfUserSolve struct {
	ID        string `json:"id"`
	CreatedAt int64  `json:"createdAt"`
}

type rctfUserProfileResponse struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Data    struct {
		Solves []rctfUserSolve `json:"solves"`
	} `json:"data"`
}
