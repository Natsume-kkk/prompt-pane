package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const turnObservationPollInterval = 100 * time.Millisecond

type TurnEnd uint8

const (
	TurnEndComplete TurnEnd = iota + 1
	TurnEndAborted
)

type TurnObservation struct {
	SessionID      string
	TurnID         string
	TranscriptPath string
	Offset         int64
}

type turnLifecycleRecord struct {
	Type    string `json:"type"`
	Payload struct {
		ID     string `json:"id"`
		Type   string `json:"type"`
		TurnID string `json:"turn_id"`
		Reason string `json:"reason"`
	} `json:"payload"`
}

func PrepareTurnObservation(observation TurnObservation) (TurnObservation, error) {
	if err := validateTurnObservation(observation, false); err != nil {
		return TurnObservation{}, err
	}
	info, err := os.Stat(observation.TranscriptPath)
	if err != nil {
		return TurnObservation{}, fmt.Errorf("inspect current transcript")
	}
	if !info.Mode().IsRegular() {
		return TurnObservation{}, fmt.Errorf("current transcript is not a regular file")
	}
	observation.Offset = info.Size()
	return observation, nil
}

func StartTurnObserver(executable string, observation TurnObservation) error {
	if strings.TrimSpace(executable) == "" {
		return fmt.Errorf("observer executable is empty")
	}
	if err := validateTurnObservation(observation, true); err != nil {
		return err
	}
	command := exec.Command(executable,
		"_observe", "codex", observation.SessionID, observation.TurnID,
		strconv.FormatInt(observation.Offset, 10), observation.TranscriptPath,
	)
	command.Env = os.Environ()
	if err := command.Start(); err != nil {
		return fmt.Errorf("start turn observer: %w", err)
	}
	if err := command.Process.Release(); err != nil {
		return fmt.Errorf("release turn observer: %w", err)
	}
	return nil
}

func ParseTurnObservation(args []string) (TurnObservation, error) {
	if len(args) != 5 || args[0] != "codex" {
		return TurnObservation{}, fmt.Errorf("invalid internal observer invocation")
	}
	offset, err := strconv.ParseInt(args[3], 10, 64)
	if err != nil {
		return TurnObservation{}, fmt.Errorf("invalid transcript offset")
	}
	observation := TurnObservation{
		SessionID:      args[1],
		TurnID:         args[2],
		Offset:         offset,
		TranscriptPath: args[4],
	}
	if err := validateTurnObservation(observation, true); err != nil {
		return TurnObservation{}, err
	}
	return observation, nil
}

func WaitForTurnEnd(ctx context.Context, observation TurnObservation) (TurnEnd, error) {
	if err := validateTurnObservation(observation, true); err != nil {
		return 0, err
	}
	file, err := os.Open(observation.TranscriptPath)
	if err != nil {
		return 0, fmt.Errorf("open current transcript")
	}
	defer file.Close()

	if err := validateTranscriptSession(file, observation); err != nil {
		return 0, err
	}
	if _, err := file.Seek(observation.Offset, io.SeekStart); err != nil {
		return 0, fmt.Errorf("seek current transcript")
	}

	buffer := make([]byte, 64<<10)
	pending := make([]byte, 0, 64<<10)
	droppingOversizedLine := false
	ticker := time.NewTicker(turnObservationPollInterval)
	defer ticker.Stop()
	for {
		count, readErr := file.Read(buffer)
		if count > 0 {
			if end, matched := consumeLifecycleBytes(buffer[:count], &pending, &droppingOversizedLine, observation.TurnID); matched {
				return end, nil
			}
		}
		if readErr != nil && readErr != io.EOF {
			return 0, fmt.Errorf("read current transcript")
		}
		if readErr == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-ticker.C:
		}
	}
}

func validateTurnObservation(observation TurnObservation, requireOffset bool) error {
	if strings.TrimSpace(observation.SessionID) == "" || strings.TrimSpace(observation.TurnID) == "" || strings.TrimSpace(observation.TranscriptPath) == "" {
		return fmt.Errorf("turn observation is missing required fields")
	}
	if requireOffset && observation.Offset < 0 {
		return fmt.Errorf("transcript offset is invalid")
	}
	return nil
}

func validateTranscriptSession(file *os.File, observation TurnObservation) error {
	reader := io.LimitReader(file, observation.Offset)
	buffer := make([]byte, 64<<10)
	pending := make([]byte, 0, 64<<10)
	droppingOversizedLine := false
	for {
		count, err := reader.Read(buffer)
		if count > 0 {
			lines := splitBoundedLines(buffer[:count], &pending, &droppingOversizedLine)
			for _, line := range lines {
				var record turnLifecycleRecord
				if json.Unmarshal(line, &record) == nil && record.Type == "session_meta" && record.Payload.ID != "" {
					if record.Payload.ID != observation.SessionID {
						return fmt.Errorf("current transcript does not match the active session")
					}
					return nil
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read current transcript identity")
		}
	}
	return fmt.Errorf("current transcript has no matching session identity")
}

func consumeLifecycleBytes(data []byte, pending *[]byte, dropping *bool, turnID string) (TurnEnd, bool) {
	for _, line := range splitBoundedLines(data, pending, dropping) {
		var record turnLifecycleRecord
		if json.Unmarshal(line, &record) != nil || record.Type != "event_msg" || record.Payload.TurnID != turnID {
			continue
		}
		switch record.Payload.Type {
		case "turn_complete":
			return TurnEndComplete, true
		case "turn_aborted":
			return TurnEndAborted, true
		}
	}
	return 0, false
}

func splitBoundedLines(data []byte, pending *[]byte, dropping *bool) [][]byte {
	var lines [][]byte
	for len(data) > 0 {
		newline := bytes.IndexByte(data, '\n')
		if newline < 0 {
			if !*dropping {
				if len(*pending)+len(data) > maxTranscriptLine {
					*pending = (*pending)[:0]
					*dropping = true
				} else {
					*pending = append(*pending, data...)
				}
			}
			break
		}
		if !*dropping {
			segment := data[:newline]
			if len(*pending)+len(segment) <= maxTranscriptLine {
				*pending = append(*pending, segment...)
				line := append([]byte(nil), (*pending)...)
				lines = append(lines, line)
			}
		}
		*pending = (*pending)[:0]
		*dropping = false
		data = data[newline+1:]
	}
	return lines
}
