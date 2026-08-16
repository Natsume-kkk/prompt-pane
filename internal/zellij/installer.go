package zellij

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Natsume-kkk/prompt-pane/internal/paths"
)

const (
	windowsURL  = "https://github.com/zellij-org/zellij/releases/download/v0.44.3/zellij-x86_64-pc-windows-msvc.zip"
	windowsHash = "d0b6541330d0f632850bdd35e7af8682630fb0793d2f8db5a38bcfac9c0c7de4"
	maxDownload = 100 << 20
)

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
		return "", fmt.Errorf("managed Zellij is only supported on Windows x64 in v1.1.0")
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
		return "", fmt.Errorf("download Zellij: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download Zellij: HTTP %d", response.StatusCode)
	}
	if progress != nil {
		progress(0, response.ContentLength)
	}

	temp, err := os.CreateTemp(filepath.Dir(target), "zellij-*.zip")
	if err != nil {
		return "", fmt.Errorf("create temporary archive: %w", err)
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
		return "", fmt.Errorf("save Zellij archive: %w", copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close Zellij archive: %w", closeErr)
	}
	if written > maxDownload {
		return "", fmt.Errorf("Zellij archive exceeds size limit")
	}

	reader, err := zip.OpenReader(tempName)
	if err != nil {
		return "", fmt.Errorf("open Zellij archive: %w", err)
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
			return "", fmt.Errorf("Zellij checksum does not match the pinned release")
		}
		temporaryTarget := target + ".tmp"
		if err := os.WriteFile(temporaryTarget, binary, 0o700); err != nil {
			return "", fmt.Errorf("write managed Zellij: %w", err)
		}
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			_ = os.Remove(temporaryTarget)
			return "", fmt.Errorf("replace managed Zellij: %w", err)
		}
		if err := os.Rename(temporaryTarget, target); err != nil {
			_ = os.Remove(temporaryTarget)
			return "", fmt.Errorf("activate managed Zellij: %w", err)
		}
		return target, nil
	}
	return "", fmt.Errorf("Zellij archive has no Windows executable")
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
