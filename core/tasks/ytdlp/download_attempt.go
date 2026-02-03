package ytdlp

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	ytdlp "github.com/lrstanley/go-ytdlp"
)

var errFormatUnavailable = errors.New("format unavailable")

type downloadPlan struct {
	format    string
	proxy     string
	ipMode    string
	userAgent string
	headers   []string
	limitRate string
}

func (t *Task) buildAttemptPlans(url string, attempt int) []downloadPlan {
	plans := make([]downloadPlan, 0, len(t.Config.FormatFallbacks)+1)
	formats := append([]string{""}, t.Config.FormatFallbacks...)
	seen := make(map[string]struct{}, len(formats))
	for _, format := range formats {
		if _, ok := seen[format]; ok {
			continue
		}
		seen[format] = struct{}{}
		plans = append(plans, downloadPlan{
			format:    format,
			proxy:     t.pickProxy(),
			ipMode:    t.pickIPMode(attempt),
			userAgent: t.pickUserAgent(),
			headers:   t.pickHeaders(),
			limitRate: t.pickLimitRate(),
		})
	}
	return plans
}

func (t *Task) runDownloadAttempt(ctx context.Context, logger *log.Logger, tempDir string, item queueItem, plan downloadPlan) ([]string, error) {
	outputTemplate := filepath.Join(tempDir, "%(title).80s-%(id)s.%(ext)s")
	cmd := t.buildCommand().
		Output(outputTemplate).
		RestrictFilenames().
		EmbedMetadata().
		EmbedThumbnail().
		ResizeBuffer().
		HLSUseMPEGTS()

	if plan.format != "" {
		cmd.Format(plan.format)
	}
	if t.Config.FormatSort != "" {
		cmd.FormatSort(t.Config.FormatSort)
	}
	if t.Config.RecodeVideo != "" {
		cmd.RecodeVideo(t.Config.RecodeVideo)
	}
	if t.Config.MergeOutputFormat != "" {
		cmd.MergeOutputFormat(t.Config.MergeOutputFormat)
	}

	if t.Config.EnableResume {
		cmd.Continue()
	}

	switch t.Config.OverwritePolicy {
	case OverwritePolicyOverwrite:
		cmd.ForceOverwrites()
	case OverwritePolicySkip:
		cmd.NoOverwrites()
	default:
		cmd.NoOverwrites()
	}

	if t.Config.FragmentConcurrency > 0 {
		cmd.ConcurrentFragments(t.Config.FragmentConcurrency)
	}

	cmd.FragmentRetries("infinite").
		Retries("infinite").
		FileAccessRetries("infinite")

	cmd.RetrySleep("fragment:exp=1:20")
	cmd.RetrySleep("http:exp=1:10")
	cmd.RetrySleep("file_access:exp=1:10")

	if plan.proxy != "" {
		cmd.Proxy(plan.proxy)
	}
	if plan.ipMode == "4" {
		cmd.ForceIPv4()
	} else if plan.ipMode == "6" {
		cmd.ForceIPv6()
	}
	if plan.limitRate != "" {
		cmd.LimitRate(plan.limitRate)
	} else if t.Config.LimitRate != "" {
		cmd.LimitRate(t.Config.LimitRate)
	}
	if t.Config.ThrottledRate != "" {
		cmd.ThrottledRate(t.Config.ThrottledRate)
	}
	if t.Config.ExternalDownloader != "" {
		cmd.Downloader(t.Config.ExternalDownloader)
		for _, arg := range t.Config.ExternalDownloaderArg {
			cmd.DownloaderArgs(arg)
		}
	}
	if plan.userAgent != "" {
		cmd.UserAgent(plan.userAgent)
	}
	for _, header := range plan.headers {
		cmd.AddHeaders(header)
	}

	start := time.Now()
	t.configureProgress(ctx, cmd, item, plan)

	logger.Infof("⬇️ Downloading %s", item.url)
	if t.stateManager != nil {
		t.stateManager.Update(t.ID, urlState{
			URL:         item.url,
			Status:      "starting",
			Proxy:       plan.proxy,
			IPMode:      plan.ipMode,
			Format:      plan.format,
			Percent:     0,
			StartedAt:   time.Now(),
			LastUpdated: time.Now(),
		})
	}

	args := append(t.Flags, item.url)
	runResult, runErr := cmd.Run(ctx, args...)
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) {
			t.reportAttempt(item.url, plan, false, time.Since(start), runErr)
			return nil, newTaskError(ErrorCodeCanceled, "download", runErr)
		}
		logger.Warnf("yt-dlp exited with warning/error: %v (validating files)", runErr)
	}

	files, err := collectValidFiles(logger, tempDir)
	if err != nil {
		t.reportAttempt(item.url, plan, false, time.Since(start), err)
		return nil, newTaskError(ErrorCodeDownloadFailed, "scan files", err)
	}

	if len(files) == 0 {
		if t.Config.EnableRepair && t.Config.RepairPasses > 0 {
			repaired, repairErr := t.repairFragments(ctx, logger, tempDir, item, plan)
			if repairErr == nil && len(repaired) > 0 {
				files = repaired
			} else if repairErr != nil {
				runErr = repairErr
			}
		}
	}

	if len(files) == 0 {
		if hasUnmergedStreams(tempDir) {
			t.reportAttempt(item.url, plan, false, time.Since(start), errors.New("unmerged streams"))
			return nil, newTaskError(
				ErrorCodeDownloadFailed,
				"merge streams",
				errors.New("yt-dlp produced separate video/audio streams; ffmpeg is required to merge them"),
			)
		}
		if detail := summarizeYtdlpFailure(runErr, runResult); detail != "" {
			if isFormatUnavailable(detail) {
				t.reportAttempt(item.url, plan, false, time.Since(start), errors.New(detail))
				return nil, errFormatUnavailable
			}
			t.reportAttempt(item.url, plan, false, time.Since(start), errors.New(detail))
			return nil, newTaskError(ErrorCodeDownloadFailed, "validate files", fmt.Errorf("no valid file produced; %s", detail))
		}
		t.reportAttempt(item.url, plan, false, time.Since(start), errors.New("no valid file produced"))
		return nil, newTaskError(ErrorCodeDownloadFailed, "validate files", errors.New("no valid file produced"))
	}

	validated, err := t.verifyFiles(logger, files)
	if err != nil {
		t.reportAttempt(item.url, plan, false, time.Since(start), err)
		return nil, err
	}

	latency := time.Since(start)
	t.reportAttempt(item.url, plan, true, latency, runErr)
	t.markComplete(item.url)
	if t.stateManager != nil {
		t.stateManager.MarkComplete(t.ID, item.url)
	}
	return validated, nil
}

func (t *Task) configureProgress(ctx context.Context, cmd *ytdlp.Command, item queueItem, plan downloadPlan) {
	if t.Progress == nil && t.stateManager == nil {
		return
	}
	cmd.ProgressFunc(250*time.Millisecond, func(prog ytdlp.ProgressUpdate) {
		percent := prog.Percent()
		if percent == 0 && prog.TotalBytes > 0 && prog.DownloadedBytes > 0 {
			percent = (float64(prog.DownloadedBytes) / float64(prog.TotalBytes)) * 100
		}

		speed := calcSpeed(prog.DownloadedBytes, prog.Duration())
		eta := prog.ETA()
		status := fmt.Sprintf("%s", prog.Status)

		t.updateStats(item.url, percent, status)
		if t.Progress != nil {
			totalPercent := t.totalPercent(percent)
			t.Progress.OnProgress(ctx, t, ProgressUpdate{
				Status:        status,
				FilePercent:   percent,
				TotalPercent:  totalPercent,
				Speed:         speed,
				ETA:           eta,
				Filename:      prog.Filename,
				ItemIndex:     item.index + 1,
				ItemTotal:     len(t.URLs),
				FragmentIndex: prog.FragmentIndex,
				FragmentCount: prog.FragmentCount,
			})
		}

		if t.stateManager != nil {
			t.stateManager.Update(t.ID, urlState{
				URL:             item.url,
				Status:          status,
				Filename:        prog.Filename,
				Percent:         percent,
				DownloadedBytes: int64(prog.DownloadedBytes),
				TotalBytes:      int64(prog.TotalBytes),
				FragmentIndex:   prog.FragmentIndex,
				FragmentCount:   prog.FragmentCount,
				Proxy:           plan.proxy,
				IPMode:          plan.ipMode,
				Format:          plan.format,
			})
		}
	})
}

func (t *Task) reportAttempt(url string, plan downloadPlan, success bool, latency time.Duration, runErr error) {
	if t.proxyPool != nil {
		t.proxyPool.Report(plan.proxy, success, latency)
	}

	class := classifyRetryError(runErr)
	if t.rateLimiter != nil {
		t.rateLimiter.Report(hostFromURL(url), class, success)
	}
	if t.rateController != nil {
		t.rateController.Report(success, class)
	}
}

func isFormatUnavailable(detail string) bool {
	lower := strings.ToLower(detail)
	return strings.Contains(lower, "requested format is not available") ||
		strings.Contains(lower, "format not available") ||
		strings.Contains(lower, "no video formats found")
}
