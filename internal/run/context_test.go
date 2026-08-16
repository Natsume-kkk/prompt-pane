package run

import (
	"strings"
	"testing"
)

func TestNewCreatesIndependentCredentials(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	b, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID || a.Token == b.Token || a.Endpoint == b.Endpoint {
		t.Fatal("independent runs reused identity material")
	}
	if len(a.Token) != 64 {
		t.Fatalf("token length = %d, want 64 hex characters", len(a.Token))
	}
}

func TestFromEnvironmentValidatesEndpointAndCredentialStrength(t *testing.T) {
	context, err := New()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range context.Environment() {
		name, value, _ := strings.Cut(entry, "=")
		t.Setenv(name, value)
	}
	if got, err := FromEnvironment(); err != nil || got != context {
		t.Fatalf("valid environment was rejected: %#v, %v", got, err)
	}

	t.Setenv(EnvEndpoint, `\\.\pipe\prompt-pane-other`)
	if _, err := FromEnvironment(); err == nil {
		t.Fatal("mismatched endpoint was accepted")
	}
	t.Setenv(EnvEndpoint, context.Endpoint)
	t.Setenv(EnvToken, "short")
	if _, err := FromEnvironment(); err == nil {
		t.Fatal("short token was accepted")
	}
}
