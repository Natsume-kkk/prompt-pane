package zellij

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Natsume-kkk/prompt-pane/internal/filetxn"
	"github.com/Natsume-kkk/prompt-pane/internal/paths"
)

func TestZellijRequestErrorExplainsProxyAndFallback(t *testing.T) {
	for _, err := range []error{errors.New("synthetic network failure"), context.DeadlineExceeded} {
		message := zellijRequestError(err).Error()
		for _, expected := range []string{"GitHub", "HTTPS_PROXY", "Zellij 0.44.3", "PATH"} {
			if !strings.Contains(message, expected) {
				t.Fatalf("error %q is missing %q", message, expected)
			}
		}
	}
}

func TestZellijStatusErrorIncludesHTTPStatusAndAction(t *testing.T) {
	message := zellijStatusError(http.StatusNotFound).Error()
	for _, expected := range []string{"HTTP 404", "pinned Zellij release", "HTTPS_PROXY"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("error %q is missing %q", message, expected)
		}
	}
}

func TestZellijRequestErrorRedactsProxyCredentials(t *testing.T) {
	message := zellijRequestError(errors.New("proxy http://synthetic-user:synthetic-password@proxy.example failed")).Error()
	if strings.Contains(message, "synthetic-user") || strings.Contains(message, "synthetic-password") || !strings.Contains(message, "http://***@proxy.example") {
		t.Fatalf("error did not redact proxy credentials: %q", message)
	}
}

func TestReportingReaderPublishesDownloadedBytes(t *testing.T) {
	var reports []int64
	reader := &reportingReader{
		reader: bytes.NewBufferString("123456"),
		total:  6,
		report: func(downloaded, total int64) {
			if total != 6 {
				t.Fatalf("total = %d", total)
			}
			reports = append(reports, downloaded)
		},
	}
	if _, err := io.ReadAll(reader); err != nil {
		t.Fatal(err)
	}
	if len(reports) == 0 || reports[len(reports)-1] != 6 {
		t.Fatalf("reports = %v", reports)
	}
}

func TestPreflightManagedInstallAccessSupportsUnicodePath(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "Prompt Pane 数据")
	t.Setenv(paths.EnvHome, home)

	if err := PreflightManagedInstallAccess(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("preflight-created directory was not cleaned: %v", err)
	}
}

func TestWriteManagedBinaryPreservesExistingTargetWhenReplaceFails(t *testing.T) {
	target := filepath.Join(t.TempDir(), "zellij.exe")
	if err := os.WriteFile(target, []byte("existing Zellij"), 0o700); err != nil {
		t.Fatal(err)
	}
	replaceErr := errors.New("synthetic replace failure")
	err := writeManagedBinary(target, []byte("new Zellij"), func(source, destination string) error {
		if destination != target {
			t.Fatalf("replacement destination = %q, want %q", destination, target)
		}
		data, readErr := os.ReadFile(source)
		if readErr != nil || string(data) != "new Zellij" {
			t.Fatalf("staged Zellij = %q, err = %v", data, readErr)
		}
		return replaceErr
	})
	if !errors.Is(err, replaceErr) {
		t.Fatalf("replacement error = %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "existing Zellij" {
		t.Fatalf("existing Zellij = %q, err = %v", data, err)
	}
	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(target) {
		t.Fatalf("temporary Zellij file remained: %v", entries)
	}
}

func TestWriteManagedBinaryReplacesExistingTarget(t *testing.T) {
	target := filepath.Join(t.TempDir(), "zellij.exe")
	if err := os.WriteFile(target, []byte("existing Zellij"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeManagedBinary(target, []byte("new Zellij"), filetxn.Replace); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "new Zellij" {
		t.Fatalf("installed Zellij = %q, err = %v", data, err)
	}
}
