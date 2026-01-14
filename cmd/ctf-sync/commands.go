package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/rw-r-r-0644/ctf-sync/jeopardy"
)

func runList(ctx context.Context, b jeopardy.Backend) error {
	challenges, err := b.Fetch(ctx)
	if err != nil {
		return err
	}

	// Try to fetch solves to mark status
	if solves, err := b.Solves(ctx); err == nil {
		solvedMap := make(map[string]bool)
		for _, s := range solves {
			solvedMap[s.ChallengeID] = true
		}
		for i := range challenges {
			if solvedMap[challenges[i].ID] {
				challenges[i].Solved = true
			}
		}
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "ID\tName\tCategory\tPoints\tSolved")
	for _, c := range challenges {
		solvedStr := "No"
		if c.Solved {
			solvedStr = "Yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", c.ID, c.Name, c.Category, c.Points, solvedStr)
	}
	return w.Flush()
}

func runInfo(ctx context.Context, b jeopardy.Backend, id string) error {
	c, err := findChallenge(ctx, b, id)
	if err != nil {
		return err
	}

	fmt.Printf("ID:          %s\n", c.ID)
	fmt.Printf("Name:        %s\n", c.Name)
	fmt.Printf("Category:    %s\n", c.Category)
	fmt.Printf("Points:      %d\n", c.Points)
	fmt.Printf("Solved:      %v\n", c.Solved)
	if len(c.Tags) > 0 {
		fmt.Printf("Tags:        %s\n", strings.Join(c.Tags, ", "))
	}
	fmt.Printf("Description:\n%s\n", c.Description)
	if len(c.Files) > 0 {
		fmt.Println("Files:")
		for _, f := range c.Files {
			fmt.Printf("  - %s\n", f.Name())
		}
	}
	return nil
}

func runGet(ctx context.Context, b jeopardy.Backend, id string) error {
	c, err := findChallenge(ctx, b, id)
	if err != nil {
		return err
	}

	dirName := sanitizeFilename(c.Name)
	if dirName == "" {
		dirName = c.ID
	}

	if err := os.MkdirAll(dirName, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	type FileDTO struct {
		Name string `json:"name"`
	}
	type ChallengeDTO struct {
		ID          string    `json:"id"`
		Name        string    `json:"name"`
		Category    string    `json:"category"`
		Description string    `json:"description"`
		Points      int       `json:"points"`
		Tags        []string  `json:"tags"`
		Files       []FileDTO `json:"files"`
		Solved      bool      `json:"solved"`
	}

	dto := ChallengeDTO{
		ID:          c.ID,
		Name:        c.Name,
		Category:    c.Category,
		Description: c.Description,
		Points:      c.Points,
		Tags:        c.Tags,
		Solved:      c.Solved,
	}
	for _, f := range c.Files {
		dto.Files = append(dto.Files, FileDTO{Name: f.Name()})
	}

	jsonData, err := json.MarshalIndent(dto, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}

	if err := os.WriteFile(filepath.Join(dirName, "challenge.json"), jsonData, 0644); err != nil {
		return fmt.Errorf("write challenge.json: %w", err)
	}

	fmt.Printf("Saved challenge info to %s/challenge.json\n", dirName)

	for _, f := range c.Files {
		if err := downloadFile(ctx, f, dirName); err != nil {
			fmt.Printf("Error downloading %s: %v\n", f.Name(), err)
		} else {
			fmt.Printf("Downloaded %s\n", f.Name())
		}
	}

	return nil
}

func runGetFile(ctx context.Context, b jeopardy.Backend, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: get-file <challenge-id> <filename>")
	}
	challID := args[0]
	fileName := args[1]

	c, err := findChallenge(ctx, b, challID)
	if err != nil {
		return err
	}

	var targetFile jeopardy.File
	for _, f := range c.Files {
		if f.Name() == fileName {
			targetFile = f
			break
		}
	}

	if targetFile == nil {
		return fmt.Errorf("file %s not found in challenge %s", fileName, challID)
	}

	if err := downloadFile(ctx, targetFile, "."); err != nil {
		return err
	}
	fmt.Printf("Downloaded %s\n", fileName)
	return nil
}

func runSubmit(ctx context.Context, b jeopardy.Backend, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: submit <challenge-id> <flag>")
	}
	challID := args[0]
	flag := args[1]

	fmt.Printf("Submitting flag for challenge %s...\n", challID)
	res, err := b.Submit(ctx, challID, flag)
	if err != nil {
		return fmt.Errorf("submission failed: %w", err)
	}

	switch res.Status {
	case jeopardy.Accepted:
		fmt.Printf("Correct! %s\n", res.Message)
	case jeopardy.Rejected:
		fmt.Printf("Incorrect. %s\n", res.Message)
	case jeopardy.Duplicate:
		fmt.Printf("Already solved. %s\n", res.Message)
	case jeopardy.RateLimited:
		fmt.Printf("Rate limited. %s\n", res.Message)
	case jeopardy.Pending:
		fmt.Printf("Pending... %s\n", res.Message)
	case jeopardy.Error:
		fmt.Printf("Error: %s\n", res.Message)
	default:
		fmt.Printf("Unknown status: %s\n", res.Message)
	}
	return nil
}

func findChallenge(ctx context.Context, b jeopardy.Backend, id string) (*jeopardy.Challenge, error) {
	challenges, err := b.Fetch(ctx)
	if err != nil {
		return nil, err
	}
	for i := range challenges {
		if challenges[i].ID == id {
			return &challenges[i], nil
		}
	}
	return nil, fmt.Errorf("challenge %s not found", id)
}

func downloadFile(ctx context.Context, f jeopardy.File, dir string) error {
	info, err := f.DownloadURL(ctx)
	if err != nil {
		return fmt.Errorf("get download url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", info.URL, nil)
	if err != nil {
		return err
	}
	for k, v := range info.Headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	outPath := filepath.Join(dir, f.Name())
	outFile, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	_, err = io.Copy(outFile, resp.Body)
	return err
}

func sanitizeFilename(name string) string {
	return strings.ReplaceAll(strings.ReplaceAll(name, "/", "_"), "\\", "_")
}
