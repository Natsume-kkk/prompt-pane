package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	processutil "github.com/Natsume-kkk/prompt-pane/internal/process"
	"github.com/Natsume-kkk/prompt-pane/internal/provider"
)

const maxTranscriptLine = 8 << 20

type transcriptRecord struct {
	Type    string `json:"type"`
	Payload struct {
		ID     string `json:"id"`
		Type   string `json:"type"`
		CWD    string `json:"cwd"`
		Model  string `json:"model"`
		Effort string `json:"effort"`
		Info   *struct {
			TotalTokenUsage struct {
				InputTokens  int64 `json:"input_tokens"`
				OutputTokens int64 `json:"output_tokens"`
				TotalTokens  int64 `json:"total_tokens"`
			} `json:"total_token_usage"`
			LastTokenUsage struct {
				InputTokens int64 `json:"input_tokens"`
			} `json:"last_token_usage"`
			ModelContextWindow int64 `json:"model_context_window"`
		} `json:"info"`
		RateLimits struct {
			LimitID   string           `json:"limit_id"`
			Primary   *rateLimitBucket `json:"primary"`
			Secondary *rateLimitBucket `json:"secondary"`
		} `json:"rate_limits"`
	} `json:"payload"`
}

type rateLimitBucket struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int64   `json:"window_minutes"`
	ResetsAt      int64   `json:"resets_at"`
}

func readMetrics(path, expectedSessionID, hookCWD, hookModel string) (*provider.SessionMetrics, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open current transcript: %w", err)
	}
	defer file.Close()

	metrics := &provider.SessionMetrics{Model: hookModel}
	cwd := hookCWD
	transcriptSessionID := ""
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), maxTranscriptLine)
	for scanner.Scan() {
		var record transcriptRecord
		if json.Unmarshal(scanner.Bytes(), &record) != nil {
			continue
		}
		switch record.Type {
		case "session_meta":
			if record.Payload.ID != "" {
				transcriptSessionID = record.Payload.ID
			}
			if cwd == "" && record.Payload.CWD != "" {
				cwd = record.Payload.CWD
			}
		case "turn_context":
			if record.Payload.Model != "" {
				metrics.Model = record.Payload.Model
			}
			if record.Payload.Effort != "" {
				metrics.Effort = record.Payload.Effort
			}
		case "event_msg":
			if record.Payload.Type != "token_count" || record.Payload.Info == nil {
				continue
			}
			usage := record.Payload.Info.TotalTokenUsage
			metrics.TotalTokens = usage.TotalTokens
			if metrics.TotalTokens == 0 {
				metrics.TotalTokens = usage.InputTokens + usage.OutputTokens
			}
			window := record.Payload.Info.ModelContextWindow
			metrics.ContextWindow = window
			if window > 0 {
				metrics.ContextUsedPercent = float64(record.Payload.Info.LastTokenUsage.InputTokens) * 100 / float64(window)
			}
			if record.Payload.RateLimits.LimitID != "codex" {
				continue
			}
			metrics.FiveHour = nil
			metrics.SevenDay = nil
			for _, limit := range []*rateLimitBucket{record.Payload.RateLimits.Primary, record.Payload.RateLimits.Secondary} {
				if limit == nil {
					continue
				}
				usedPercent := limit.UsedPercent
				if limit.ResetsAt > 0 && limit.ResetsAt <= time.Now().Unix() {
					usedPercent = 0
				}
				quota := &provider.QuotaWindow{UsedPercent: usedPercent, ResetsAt: limit.ResetsAt}
				if limit.WindowMinutes < 24*60 {
					metrics.FiveHour = quota
				} else {
					metrics.SevenDay = quota
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read current transcript: %w", err)
	}
	if transcriptSessionID == "" || transcriptSessionID != expectedSessionID {
		return nil, fmt.Errorf("current transcript does not match the active session")
	}
	addGitMetrics(metrics, cwd)
	return metrics, nil
}

func addGitMetrics(metrics *provider.SessionMetrics, cwd string) {
	if cwd == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	run := func(arguments ...string) (string, error) {
		args := append([]string{"-C", cwd}, arguments...)
		output, err := processutil.Output(ctx, "git", args, processutil.OutputOptions{
			Env:   append(os.Environ(), "GIT_OPTIONAL_LOCKS=0"),
			Limit: 1 << 20,
		})
		return strings.TrimSpace(string(output)), err
	}
	branch, err := run("branch", "--show-current")
	if err != nil {
		return
	}
	if branch == "" {
		return
	}
	metrics.Branch = branch
	if status, err := run("status", "--porcelain", "--untracked-files=no"); err == nil && status != "" {
		metrics.Branch += "*"
	}
	if diff, err := run("diff", "HEAD", "--numstat"); err == nil {
		for _, line := range strings.Split(diff, "\n") {
			parts := strings.Split(line, "\t")
			if len(parts) < 2 {
				continue
			}
			if value, err := strconv.Atoi(parts[0]); err == nil {
				metrics.Added += value
			}
			if value, err := strconv.Atoi(parts[1]); err == nil {
				metrics.Deleted += value
			}
		}
	}
	if files, err := run("ls-files", "--others", "--exclude-standard"); err == nil && files != "" {
		metrics.Untracked = len(strings.Split(files, "\n"))
	}
}
