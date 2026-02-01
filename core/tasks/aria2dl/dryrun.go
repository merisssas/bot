package aria2dl

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/merisssas/Bot/common/utils/netutil"
)

const (
	dryRunTimeout = 15 * time.Second
)

type DryRunFile struct {
	URI         string
	FileName    string
	ContentType string
	Length      int64
	StatusCode  int
}

type DryRunResult struct {
	Files   []DryRunFile
	Skipped []string
}

func DryRun(ctx context.Context, taskConfig TaskConfig, uris []string) (*DryRunResult, error) {
	client, err := netutil.NewProxyHTTPClient(taskConfig.ProxyURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy http client: %w", err)
	}

	result := &DryRunResult{}
	for _, uri := range uris {
		parsed, err := url.Parse(uri)
		if err != nil || parsed.Scheme == "" {
			result.Skipped = append(result.Skipped, uri)
			continue
		}

		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			result.Skipped = append(result.Skipped, uri)
			continue
		}

		fileInfo, err := probeHead(ctx, client, taskConfig, uri)
		if err != nil {
			result.Skipped = append(result.Skipped, uri)
			continue
		}

		result.Files = append(result.Files, *fileInfo)
	}

	return result, nil
}

func probeHead(ctx context.Context, client *http.Client, taskConfig TaskConfig, uri string) (*DryRunFile, error) {
	reqCtx, cancel := context.WithTimeout(ctx, dryRunTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodHead, uri, nil)
	if err != nil {
		return nil, err
	}
	applyHeaders(taskConfig, req)

	resp, err := client.Do(req)
	if err != nil || resp.StatusCode == http.StatusMethodNotAllowed {
		return probeRangeGet(ctx, client, taskConfig, uri)
	}
	defer resp.Body.Close()

	return buildDryRunFile(uri, resp), nil
}

func probeRangeGet(ctx context.Context, client *http.Client, taskConfig TaskConfig, uri string) (*DryRunFile, error) {
	reqCtx, cancel := context.WithTimeout(ctx, dryRunTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", "bytes=0-0")
	applyHeaders(taskConfig, req)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return buildDryRunFile(uri, resp), nil
}

func buildDryRunFile(uri string, resp *http.Response) *DryRunFile {
	fileName := parseFileName(resp, uri)
	length := resp.ContentLength

	return &DryRunFile{
		URI:         uri,
		FileName:    fileName,
		ContentType: resp.Header.Get("Content-Type"),
		Length:      length,
		StatusCode:  resp.StatusCode,
	}
}

func parseFileName(resp *http.Response, uri string) string {
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if filename := params["filename"]; filename != "" {
				return filename
			}
		}
	}

	parsed, err := url.Parse(uri)
	if err == nil {
		base := path.Base(parsed.Path)
		if base != "" && base != "." && base != "/" {
			return base
		}
	}

	if resp != nil && resp.Request != nil && resp.Request.URL != nil {
		base := path.Base(resp.Request.URL.Path)
		if base != "" && base != "." && base != "/" {
			return base
		}
	}

	return "unknown"
}

func applyHeaders(taskConfig TaskConfig, req *http.Request) {
	if taskConfig.UserAgent != "" {
		req.Header.Set("User-Agent", taskConfig.UserAgent)
	}
	for key, value := range taskConfig.CustomHeaders {
		if strings.TrimSpace(key) == "" {
			continue
		}
		req.Header.Set(key, value)
	}
}
