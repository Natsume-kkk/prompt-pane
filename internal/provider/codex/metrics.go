package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
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
		RateLimits          *rateLimitSnapshot           `json:"rate_limits"`
		RateLimitsByLimitID map[string]rateLimitSnapshot `json:"rate_limits_by_limit_id"`
	} `json:"payload"`
}

type rateLimitSnapshot struct {
	LimitID   string           `json:"limit_id"`
	Primary   *rateLimitBucket `json:"primary"`
	Secondary *rateLimitBucket `json:"secondary"`
}

type rateLimitBucket struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int64   `json:"window_minutes"`
	ResetsAt      int64   `json:"resets_at"`
}

func readMetrics(path, expectedSessionID, hookCWD, hookModel string) (*provider.SessionMetrics, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open current transcript")
	}
	defer file.Close()

	metrics := &provider.SessionMetrics{Model: hookModel}
	cwd := hookCWD
	transcriptSessionID := ""
	var latestRateLimits *rateLimitSnapshot
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
			if limits, ok := activeRateLimits(record.Payload.RateLimits, record.Payload.RateLimitsByLimitID); ok {
				latestRateLimits = limits
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read current transcript")
	}
	if transcriptSessionID == "" || transcriptSessionID != expectedSessionID {
		return nil, fmt.Errorf("current transcript does not match the active session")
	}
	metrics.QuotaStatus = provider.QuotaUnavailable
	if latestRateLimits != nil {
		metrics.Quotas = quotaWindows(latestRateLimits, time.Now())
		if len(metrics.Quotas) > 0 {
			metrics.QuotaStatus = provider.QuotaAvailable
		}
	}
	addGitMetrics(metrics, cwd)
	return metrics, nil
}

func activeRateLimits(single *rateLimitSnapshot, byID map[string]rateLimitSnapshot) (*rateLimitSnapshot, bool) {
	if limits, ok := byID["codex"]; ok && hasQuotaWindow(&limits) {
		return &limits, true
	}
	if single != nil && single.LimitID == "codex" && hasQuotaWindow(single) {
		return single, true
	}
	return nil, false
}

func hasQuotaWindow(limits *rateLimitSnapshot) bool {
	return limits != nil && (limits.Primary != nil && limits.Primary.WindowMinutes > 0 || limits.Secondary != nil && limits.Secondary.WindowMinutes > 0)
}

func quotaWindows(limits *rateLimitSnapshot, now time.Time) []provider.QuotaWindow {
	if limits == nil {
		return nil
	}
	quotas := make([]provider.QuotaWindow, 0, 2)
	for _, limit := range []*rateLimitBucket{limits.Primary, limits.Secondary} {
		if limit == nil || limit.WindowMinutes <= 0 || limit.ResetsAt > 0 && limit.ResetsAt <= now.Unix() {
			continue
		}
		quotas = append(quotas, provider.QuotaWindow{
			WindowMinutes: limit.WindowMinutes,
			UsedPercent:   limit.UsedPercent,
			ResetsAt:      limit.ResetsAt,
		})
	}
	sort.SliceStable(quotas, func(i, j int) bool {
		return quotas[i].WindowMinutes < quotas[j].WindowMinutes
	})
	return quotas
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
