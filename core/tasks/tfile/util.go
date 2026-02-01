package tfile

import "time"

var progressUpdatesLevels = []struct {
	size        int64 // 文件大小阈值
	stepPercent int   // 每多少 % 更新一次
}{
	{10 << 20, 100},
	{50 << 20, 20},
	{200 << 20, 10},
	{500 << 20, 5},
}

func progressStep(total int64) int {
	step := progressUpdatesLevels[len(progressUpdatesLevels)-1].stepPercent
	for _, lvl := range progressUpdatesLevels {
		if total < lvl.size {
			step = lvl.stepPercent
			break
		}
	}
	return step
}

func shouldUpdateProgress(total, downloaded int64, lastUpdatePercent int, lastUpdateAt time.Time, now time.Time) bool {
	if total <= 0 || downloaded <= 0 {
		return false
	}

	percent := int((downloaded * 100) / total)
	if percent <= lastUpdatePercent {
		return false
	}

	step := progressStep(total)
	if now.Sub(lastUpdateAt) >= 2*time.Second {
		return true
	}

	return percent >= lastUpdatePercent+step
}
