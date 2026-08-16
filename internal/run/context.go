package run

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
)

const (
	EnvRunID    = "PROMPT_PANE_RUN_ID"
	EnvToken    = "PROMPT_PANE_TOKEN"
	EnvEndpoint = "PROMPT_PANE_ENDPOINT"
)

type Context struct {
	ID       string
	Token    string
	Endpoint string
}

func New() (Context, error) {
	id, err := randomHex(16)
	if err != nil {
		return Context{}, fmt.Errorf("generate run id: %w", err)
	}
	token, err := randomHex(32)
	if err != nil {
		return Context{}, fmt.Errorf("generate run token: %w", err)
	}
	return Context{ID: id, Token: token, Endpoint: endpointFor(id)}, nil
}

func FromEnvironment() (Context, error) {
	c := Context{
		ID:       os.Getenv(EnvRunID),
		Token:    os.Getenv(EnvToken),
		Endpoint: os.Getenv(EnvEndpoint),
	}
	if c.ID == "" || c.Token == "" || c.Endpoint == "" {
		return Context{}, fmt.Errorf("Prompt Pane run environment is incomplete")
	}
	if !validHex(c.ID, 16) || !validHex(c.Token, 32) || c.Endpoint != endpointFor(c.ID) {
		return Context{}, fmt.Errorf("Prompt Pane run environment is invalid")
	}
	return c, nil
}

func (c Context) Environment() []string {
	return []string{
		EnvRunID + "=" + c.ID,
		EnvToken + "=" + c.Token,
		EnvEndpoint + "=" + c.Endpoint,
	}
}

func randomHex(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func validHex(value string, bytes int) bool {
	if len(value) != bytes*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == bytes
}

func endpointFor(id string) string {
	return `\\.\pipe\prompt-pane-` + id
}
