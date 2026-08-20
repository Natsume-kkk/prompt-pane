package process

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"sync"
)

var ErrOutputLimit = errors.New("command output exceeded limit")

type OutputOptions struct {
	Env   []string
	Limit int
}

func Output(ctx context.Context, path string, args []string, options OutputOptions) ([]byte, error) {
	commandContext, cancel := context.WithCancel(ctx)
	defer cancel()
	command := exec.CommandContext(commandContext, path, args...)
	if options.Env != nil {
		command.Env = options.Env
	}
	output := &limitedOutput{limit: options.Limit, onLimit: cancel}
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	if ctx.Err() != nil {
		return output.Bytes(), ctx.Err()
	}
	if output.Truncated() {
		return output.Bytes(), ErrOutputLimit
	}
	if err != nil {
		return output.Bytes(), err
	}
	return output.Bytes(), nil
}

type limitedOutput struct {
	mu        sync.Mutex
	data      bytes.Buffer
	limit     int
	truncated bool
	onLimit   func()
	limitOnce sync.Once
}

func (o *limitedOutput) Write(data []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	remaining := o.limit - o.data.Len()
	if remaining > 0 {
		_, _ = o.data.Write(data[:min(len(data), remaining)])
	}
	if len(data) > remaining {
		o.truncated = true
		o.limitOnce.Do(func() {
			if o.onLimit != nil {
				o.onLimit()
			}
		})
	}
	return len(data), nil
}

func (o *limitedOutput) Bytes() []byte {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]byte(nil), o.data.Bytes()...)
}

func (o *limitedOutput) Truncated() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.truncated
}
