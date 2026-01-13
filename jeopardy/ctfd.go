package jeopardy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func init() {
	Register(BackendDef{
		ID:   "ctfd_token",
		Name: "CTFd (Token)",
		Settings: []SettingDef{
			{ID: "base_url", Name: "Base URL", Required: true},
			{ID: "token", Name: "API Token", Required: true},
		},
		Build: func(s map[string]string) (Backend, error) {
			return newCTFd(s["base_url"], tokenAuth(s["token"]))
		},
	})

	Register(BackendDef{
		ID:   "ctfd_cookie",
		Name: "CTFd (Cookie)",
		Settings: []SettingDef{
			{ID: "base_url", Name: "Base URL", Required: true},
			{ID: "cookie", Name: "Session Cookie", Required: true},
		},
		Build: func(s map[string]string) (Backend, error) {
			return newCTFd(s["base_url"], cookieAuth(s["cookie"]))
		},
	})
}

type ctfdClient struct {
	baseURL   string
	applyAuth func(*http.Request)
	client    *http.Client
	authType  string
}

type ctfdFile struct {
	name   string
	path   string
	client *ctfdClient
}

func (f *ctfdFile) Name() string { return f.name }

func (f *ctfdFile) DownloadURL(ctx context.Context) (*DownloadInfo, error) {
	return &DownloadInfo{
		URL:     f.client.resolveFileURL(f.path),
		Headers: f.client.authHeaders(),
	}, nil
}

func tokenAuth(token string) func(*http.Request) {
	return func(r *http.Request) {
		r.Header.Set("Authorization", "Token "+token)
	}
}

func cookieAuth(cookie string) func(*http.Request) {
	return func(r *http.Request) {
		r.Header.Set("Cookie", cookie)
	}
}

func newCTFd(baseURL string, auth func(*http.Request)) (*ctfdClient, error) {
	authType := "token"
	return &ctfdClient{
		baseURL:   strings.TrimRight(baseURL, "/"),
		applyAuth: auth,
		client:    &http.Client{Timeout: 30 * time.Second},
		authType:  authType,
	}, nil
}

func (c *ctfdClient) Fetch(ctx context.Context) ([]Challenge, error) {
	summaries, err := c.fetchChallengeSummaries(ctx)
	if err != nil {
		return nil, err
	}

	results := make([]Challenge, 0, len(summaries))
	for _, summary := range summaries {
		detail, err := c.fetchChallengeDetail(ctx, summary.ID)
		if err != nil {
			return nil, err
		}

		challenge := Challenge{
			ID:          strconv.Itoa(summary.ID),
			Name:        nonEmpty(detail.Name, summary.Name),
			Category:    nonEmpty(detail.Category, summary.Category),
			Description: detail.Description,
			Points:      detail.Value,
		}

		if len(detail.Files) > 0 {
			challenge.Files = make([]File, 0, len(detail.Files))
			for _, fileRef := range detail.Files {
				if fileRef == "" {
					continue
				}
				challenge.Files = append(challenge.Files, &ctfdFile{
					name:   filenameFromURL(fileRef),
					path:   fileRef,
					client: c,
				})
			}
		}
		results = append(results, challenge)
	}
	return results, nil
}

func (c *ctfdClient) Submit(ctx context.Context, challengeID, flag string) (*SubmitResult, error) {
	if flag == "" {
		return nil, fmt.Errorf("flag is required")
	}
	if challengeID == "" {
		return nil, fmt.Errorf("challenge ID is required")
	}

	payload := map[string]any{
		"challenge_id": challengeID,
		"submission":   flag,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode submission: %w", err)
	}

	reqURL := c.baseURL + "/api/v1/challenges/attempt"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	c.applyAuth(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	if c.authType == "cookie" {
		csrfToken, err := c.fetchCSRFToken(ctx)
		if err == nil && csrfToken != "" {
			httpReq.Header.Set("CSRF-Token", csrfToken)
		}
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ctfd submission failed: %s", strings.TrimSpace(string(respBody)))
	}

	var parsed ctfdSubmitResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("parse ctfd response: %w", err)
	}

	return c.parseSubmitResponse(parsed), nil
}

func (c *ctfdClient) Solves(ctx context.Context) ([]Solve, error) {
	urls := []string{
		c.baseURL + "/api/v1/users/me/solves",
		c.baseURL + "/api/v1/teams/me/solves",
	}

	var lastErr error
	for _, reqURL := range urls {
		httpReq, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
		if err != nil {
			lastErr = err
			continue
		}
		c.applyAuth(httpReq)
		httpReq.Header.Set("Accept", "application/json")

		resp, err := c.client.Do(httpReq)
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("ctfd solves request failed (%s): %s", resp.Status, strings.TrimSpace(string(body)))
			continue
		}

		var parsed ctfdSolvesResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			lastErr = fmt.Errorf("parse ctfd solves response: %w", err)
			continue
		}
		if !parsed.Success {
			lastErr = fmt.Errorf("ctfd solves response error: %s", strings.TrimSpace(parsed.Message))
			continue
		}

		result := make([]Solve, 0, len(parsed.Data))
		for _, entry := range parsed.Data {
			if entry.ChallengeID == 0 {
				continue
			}
			solve := Solve{ChallengeID: strconv.Itoa(entry.ChallengeID)}
			if t := parseCTFdSolveTime(entry.Date); t != nil {
				solve.SolvedAt = t
			}
			result = append(result, solve)
		}
		return result, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("ctfd solves request failed")
}

func (c *ctfdClient) fetchChallengeSummaries(ctx context.Context) ([]ctfdChallengeSummary, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/v1/challenges", nil)
	if err != nil {
		return nil, err
	}
	c.applyAuth(req)
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ctfd list failed: %s", strings.TrimSpace(string(body)))
	}

	var payload ctfdListResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Data, nil
}

func (c *ctfdClient) fetchChallengeDetail(ctx context.Context, id int) (*ctfdChallengeDetail, error) {
	reqURL := fmt.Sprintf("%s/api/v1/challenges/%d", c.baseURL, id)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	c.applyAuth(req)
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ctfd detail failed: %s", strings.TrimSpace(string(body)))
	}

	var payload ctfdDetailResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return &payload.Data, nil
}

func (c *ctfdClient) fetchCSRFToken(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/challenges", nil)
	if err != nil {
		return "", err
	}
	c.applyAuth(req)
	req.Header.Set("Accept", "text/html")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	re := regexp.MustCompile(`csrfNonce['"]?\s*:\s*['"]([^'"]+)['"]`)
	match := re.FindSubmatch(body)
	if len(match) > 1 {
		return string(match[1]), nil
	}
	return "", nil
}

func (c *ctfdClient) resolveFileURL(fileRef string) string {
	if fileRef == "" {
		return ""
	}
	if strings.HasPrefix(fileRef, "http://") || strings.HasPrefix(fileRef, "https://") {
		return fileRef
	}
	return c.baseURL + "/" + strings.TrimLeft(fileRef, "/")
}

func (c *ctfdClient) authHeaders() map[string]string {
	headers := map[string]string{}
	req := &http.Request{Header: make(http.Header)}
	c.applyAuth(req)
	for k, v := range req.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	return headers
}

func (c *ctfdClient) parseSubmitResponse(parsed ctfdSubmitResponse) *SubmitResult {
	status := strings.ToLower(strings.TrimSpace(parsed.Data.Status))
	message := strings.TrimSpace(parsed.Data.Message)
	if message == "" {
		message = strings.TrimSpace(parsed.Message)
	}

	switch status {
	case "correct":
		if strings.Contains(strings.ToLower(message), "already solved") {
			return &SubmitResult{Status: Duplicate, Message: message}
		}
		return &SubmitResult{Status: Accepted, Message: message}
	case "already_solved":
		return &SubmitResult{Status: Duplicate, Message: message}
	case "incorrect":
		return &SubmitResult{Status: Rejected, Message: message}
	case "queued", "pending", "processing", "received", "submitted":
		return &SubmitResult{Status: Pending, Message: message}
	case "rate_limited", "ratelimited", "too_fast":
		return &SubmitResult{Status: RateLimited, Message: message}
	default:
		msgLower := strings.ToLower(message)
		if strings.Contains(msgLower, "already solved") {
			return &SubmitResult{Status: Duplicate, Message: message}
		}
		if strings.Contains(msgLower, "submission received") || strings.Contains(msgLower, "queued") {
			return &SubmitResult{Status: Pending, Message: message}
		}
		return &SubmitResult{Status: Error, Message: message}
	}
}

type ctfdChallengeSummary struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Value    int    `json:"value"`
}

type ctfdChallengeDetail struct {
	ID          int      `json:"id"`
	Name        string   `json:"name"`
	Category    string   `json:"category"`
	Description string   `json:"description"`
	Value       int      `json:"value"`
	Files       []string `json:"files"`
}

type ctfdListResponse struct {
	Success bool                   `json:"success"`
	Data    []ctfdChallengeSummary `json:"data"`
}

type ctfdDetailResponse struct {
	Success bool                `json:"success"`
	Data    ctfdChallengeDetail `json:"data"`
}

type ctfdSubmitResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	} `json:"data"`
	Message string `json:"message"`
}

type ctfdSolveEntry struct {
	ChallengeID int    `json:"challenge_id"`
	Date        string `json:"date"`
}

type ctfdSolvesResponse struct {
	Success bool             `json:"success"`
	Data    []ctfdSolveEntry `json:"data"`
	Message string           `json:"message"`
}

func nonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func filenameFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err == nil && parsed.Path != "" {
		name := path.Base(parsed.Path)
		if name != "" && name != "/" && name != "." {
			return name
		}
	}
	name := path.Base(raw)
	if name == "" || name == "/" || name == "." {
		return ""
	}
	return name
}

func parseCTFdSolveTime(value string) *time.Time {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			parsed = parsed.UTC()
			return &parsed
		}
	}
	return nil
}
