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
