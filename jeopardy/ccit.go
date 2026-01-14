package jeopardy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func init() {
	Register(BackendDef{
		ID:   "ccit",
		Name: "CCIT",
		Settings: []SettingDef{
			{ID: "base_url", Name: "Base URL", Required: true},
			{ID: "token", Name: "API Token", Required: true},
			{ID: "x-version", Name: "X-Version Header (e.g. v5.0.2)", Required: true},
		},
		Build: func(s map[string]string) (Backend, error) {
			return newCCIT(s["base_url"], s["token"], s["x-version"])
		},
	})
}

type ccitClient struct {
	baseURL    string
	token      string
	version    string
	client     *http.Client
	filesToken string
}

type ccitFile struct {
	name   string
	url    string
	client *ccitClient
}

func (f *ccitFile) Name() string { return f.name }

func (f *ccitFile) DownloadURL(ctx context.Context) (*DownloadInfo, error) {
	if f.client.filesToken == "" {
		// Attempt to fetch file token if missing
		if err := f.client.refreshToken(ctx); err != nil {
			return nil, err
		}
	}

	dlURL := f.url
	if strings.HasPrefix(dlURL, "/") {
		dlURL = f.client.baseURL + dlURL
	}

	// If it's an API URL, append auth token
	if strings.Contains(dlURL, "/api/") {
		parsed, err := url.Parse(dlURL)
		if err != nil {
			return nil, err
		}
		q := parsed.Query()
		q.Set("auth", f.client.filesToken)
		parsed.RawQuery = q.Encode()
		dlURL = parsed.String()
	}

	return &DownloadInfo{URL: dlURL}, nil
}

func newCCIT(baseURL, token, version string) (*ccitClient, error) {
	return &ccitClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		version: version,
		client:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (c *ccitClient) Fetch(ctx context.Context) ([]Challenge, error) {
	if err := c.refreshToken(ctx); err != nil {
		return nil, err
	}

	var eventsResp ccitEventsResponse
	if err := c.doRequest(ctx, "GET", "/api/challenges?noFreeze=false", nil, &eventsResp); err != nil {
		return nil, err
	}

	var results []Challenge
	for _, event := range eventsResp.Events {
		for _, section := range event.Sections {
			for _, challRef := range section.Challenges {
				// Fetch full challenge details
				var detail ccitChallengeDetail
				path := fmt.Sprintf("/api/challenges/%s?noFreeze=false", challRef.ID)
				if err := c.doRequest(ctx, "GET", path, nil, &detail); err != nil {
					// Add dummy challenge if detail fetch fails? No, better to fail or skip.
					// Let's return error to be safe.
					return nil, fmt.Errorf("fetch challenge %s: %w", challRef.ID, err)
				}

				chal := Challenge{
					ID:          detail.ID.String(),
					Name:        detail.Title,
					Category:    section.Name, // Using section as category seems map to structure
					Description: detail.Description,
					Points:      detail.Points,
					Solved:      detail.Solved,
				}

				if len(detail.Tags) > 0 {
					chal.Tags = make([]string, len(detail.Tags))
					copy(chal.Tags, detail.Tags)
				}

				if len(detail.Files) > 0 {
					chal.Files = make([]File, 0, len(detail.Files))
					for _, f := range detail.Files {
						chal.Files = append(chal.Files, &ccitFile{
							name:   f.Name,
							url:    f.URL,
							client: c,
						})
					}
				}

				results = append(results, chal)
			}
		}
	}
	return results, nil
}

func (c *ccitClient) Submit(ctx context.Context, challengeID, flag string) (*SubmitResult, error) {
	if flag == "" {
		return nil, fmt.Errorf("flag is required")
	}
	if challengeID == "" {
		return nil, fmt.Errorf("challenge ID is required")
	}

	payload := map[string]string{
		"flag": flag,
	}

	path := fmt.Sprintf("/api/challenges/%s/flag", challengeID)

	var submitResp struct {
		Valid   bool   `json:"valid"`
		Message string `json:"message"`
	}

	// doRequest handles header injection, body marshaling, result unmarshaling, and basic status checks.
	// Since the API returns 200 OK even for invalid results, doRequest's OK check will pass,
	// and we can then inspect the submitResp content.
	if err := c.doRequest(ctx, "POST", path, payload, &submitResp); err != nil {
		return nil, err
	}

	if submitResp.Valid {
		return &SubmitResult{Status: Accepted, Message: submitResp.Message}, nil
	}

	return &SubmitResult{Status: Rejected, Message: submitResp.Message}, nil
}

func (c *ccitClient) Solves(ctx context.Context) ([]Solve, error) {
	var unlocksResp struct {
		Solves []json.Number `json:"solves"`
	}
	if err := c.doRequest(ctx, "GET", "/api/player/unlocks", nil, &unlocksResp); err != nil {
		return nil, fmt.Errorf("fetch unlocks: %w", err)
	}

	results := make([]Solve, len(unlocksResp.Solves))
	for i, id := range unlocksResp.Solves {
		results[i] = Solve{
			ChallengeID: id.String(),
			// API does not return timestamp for all challs
		}
	}
	return results, nil
}

func (c *ccitClient) refreshToken(ctx context.Context) error {
	var userResp ccitUserResponse
	if err := c.doRequest(ctx, "GET", "/api/currentUser", nil, &userResp); err != nil {
		return err
	}
	c.filesToken = userResp.FilesToken
	return nil
}

func (c *ccitClient) doRequest(ctx context.Context, method, path string, body any, out any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewBuffer(data)
	}

	reqURL := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Token "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-version", c.version)

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed status=%d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

type ccitEventsResponse struct {
	Events []struct {
		ID       json.Number `json:"id"`
		Name     string      `json:"name"`
		Sections []struct {
			ID         json.Number `json:"id"`
			Name       string      `json:"name"`
			Challenges []struct {
				ID json.Number `json:"id"`
			} `json:"challenges"`
		} `json:"sections"`
	} `json:"events"`
}

type ccitUserResponse struct {
	ID         json.Number `json:"id"`
	FilesToken string      `json:"filesToken"`
}

type ccitChallengeDetail struct {
	ID          json.Number `json:"id"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Points      int         `json:"points"`
	Solved      bool        `json:"completed"`
	Files       []struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"files"`
	Tags []string `json:"tags"`
}
