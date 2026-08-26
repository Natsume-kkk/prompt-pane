package zellij

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"

	"github.com/Natsume-kkk/prompt-pane/internal/paths"
)

const (
	windowsURL  = "https://github.com/zellij-org/zellij/releases/download/v0.44.3/zellij-x86_64-pc-windows-msvc.zip"
	windowsHash = "d0b6541330d0f632850bdd35e7af8682630fb0793d2f8db5a38bcfac9c0c7de4"
	maxDownload = 100 << 20
)

const zellijNetworkGuidance = "Check access to GitHub. If this network requires a proxy, set HTTPS_PROXY for Prompt Pane, or install Zellij 0.44.3 in PATH before retrying."

var urlUserInfoPattern = regexp.MustCompile(`(?i)(https?://)[^/@\s]+@`)

func PreflightManagedInstallAccess() error {
	target, err := ManagedPath()
	if err != nil {
		return err
	}
	if err := paths.ProbeWritableDirectory(filepath.Dir(target)); err != nil {
		return fmt.Errorf("managed Zellij directory is not writable: %w", err)
	}
	return nil
}

func InstallManagedWithProgress(ctx context.Context, progress func(downloaded, total int64)) (string, error) {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return "", fmt.Errorf("managed Zellij is only supported on Windows x64")
	}
	target, err := ManagedPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return "", fmt.Errorf("create managed tool directory: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, windowsURL, nil)
	if err != nil {
		return "", fmt.Errorf("create Zellij download request: %w", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", zellijRequestError(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", zellijStatusError(response.StatusCode)
	}
	if progress != nil {
		progress(0, response.ContentLength)
	}

	temp, err := os.CreateTemp(filepath.Dir(target), "zellij-*.zip")
	if err != nil {
		return "", fmt.Errorf("create temporary Zellij archive: %w; check Prompt Pane directory permissions", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	body := io.Reader(response.Body)
	if progress != nil {
		body = &reportingReader{reader: body, total: response.ContentLength, report: progress}
	}
	written, copyErr := io.Copy(temp, io.LimitReader(body, maxDownload+1))
	closeErr := temp.Close()
	if copyErr != nil {
		return "", fmt.Errorf("save downloaded Zellij archive: %w; check network stability, disk space, and Prompt Pane directory permissions", copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("finish writing the Zellij archive: %w; check disk space and Prompt Pane directory permissions", closeErr)
	}
	if written > maxDownload {
		return "", fmt.Errorf("downloaded Zellij archive exceeds the 100 MiB safety limit; no file was installed")
	}

	reader, err := zip.OpenReader(tempName)
	if err != nil {
		return "", fmt.Errorf("downloaded Zellij archive is invalid: %w; retry the download", err)
	}
	defer reader.Close()
	for _, file := range reader.File {
		if filepath.Base(file.Name) != "zellij.exe" {
			continue
		}
		source, err := file.Open()
		if err != nil {
			return "", fmt.Errorf("open Zellij binary: %w", err)
		}
		binary, err := io.ReadAll(io.LimitReader(source, maxDownload+1))
		_ = source.Close()
		if err != nil || len(binary) > maxDownload {
			return "", fmt.Errorf("read Zellij binary")
		}
		hash := sha256.Sum256(binary)
		if hex.EncodeToString(hash[:]) != windowsHash {
			return "", fmt.Errorf("downloaded Zellij checksum does not match the pinned release; no file was installed, so retry and report the release if the mismatch persists")
		}
		temporaryTarget := target + ".tmp"
		if err := os.WriteFile(temporaryTarget, binary, 0o700); err != nil {
			return "", fmt.Errorf("write managed Zellij: %w; check Prompt Pane directory permissions and available disk space", err)
		}
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			_ = os.Remove(temporaryTarget)
			return "", fmt.Errorf("replace managed Zellij: %w; close running Zellij processes and retry", err)
		}
		if err := os.Rename(temporaryTarget, target); err != nil {
			_ = os.Remove(temporaryTarget)
			return "", fmt.Errorf("activate managed Zellij: %w; check Prompt Pane directory permissions and retry", err)
		}
		return target, nil
	}
	return "", fmt.Errorf("downloaded Zellij archive has no Windows executable; no file was installed, so verify the pinned release and retry")
}

func zellijRequestError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("Zellij download from GitHub timed out. %s", zellijNetworkGuidance)
	}
	return fmt.Errorf("download Zellij from GitHub: %s. %s", safeNetworkError(err), zellijNetworkGuidance)
}

func zellijStatusError(status int) error {
	return fmt.Errorf("download Zellij from GitHub returned HTTP %d. Verify that the pinned Zellij release is available. %s", status, zellijNetworkGuidance)
}

func safeNetworkError(err error) string {
	return urlUserInfoPattern.ReplaceAllString(err.Error(), `${1}***@`)
}

type reportingReader struct {
	reader     io.Reader
	downloaded int64
	total      int64
	report     func(downloaded, total int64)
}

func (r *reportingReader) Read(buffer []byte) (int, error) {
	read, err := r.reader.Read(buffer)
	r.downloaded += int64(read)
	if read > 0 {
		r.report(r.downloaded, r.total)
	}
	return read, err
}
