package zellij

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Natsume-kkk/prompt-pane/internal/paths"
)

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
