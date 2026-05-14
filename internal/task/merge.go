package task

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/r0n9/camkeep/constant"
)

const (
	mergedSuffix           = "_merged"
	minMergedDurationRatio = 0.75
)

var mergeFragmentTimePattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}_(?:\d{2}-\d{2}-\d{2}|\d{6})`)

type mergeFragmentScanResult struct {
	dateDir               string
	missing               bool
	totalEntries          int
	skippedDirs           int
	skippedUnsupportedExt int
	skippedMerged         int
	skippedTemp           int
	skippedNoTime         int
	fragments             []string
}

type mergeHourGroup struct {
	hourKey   string
	start     time.Time
	kind      string
	fragments []string
}

// DailyMergeTask 每天定时把前一天的碎片录像合并为单文件。
func DailyMergeTask(ctx context.Context, wg *sync.WaitGroup, cfg constant.Config) {
	defer wg.Done()

	if !cfg.DailyMerge.Enabled {
		return
	}

	for {
		nextRun, err := nextDailyMergeRun(time.Now(), cfg.DailyMerge.Time)
		if err != nil {
			log.Printf("每日录像合并任务配置无效，已跳过: %v", err)
			return
		}

		timer := time.NewTimer(time.Until(nextRun))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			mergeDate := nextRun.AddDate(0, 0, -1).Format("2006-01-02")
			log.Printf("开始执行每日录像合并任务，目标日期: %s", mergeDate)
			for _, cam := range cfg.Cameras {
				log.Printf("开始执行每日录像合并任务，目标日期: %s CamId: %s", mergeDate, cam.ID)
				if err := mergeCameraDate(ctx, cam, mergeDate); err != nil {
					log.Printf("[%s] 合并 %s 录像失败: %v", cam.ID, mergeDate, err)
				}
			}
		}
	}
}

func nextDailyMergeRun(now time.Time, timeStr string) (time.Time, error) {
	parts := strings.Split(timeStr, ":")
	if len(parts) != 2 {
		return time.Time{}, fmt.Errorf("daily_merge.time 必须使用 HH:mm 格式")
	}

	runClock, err := time.ParseInLocation("15:04", timeStr, now.Location())
	if err != nil {
		return time.Time{}, err
	}

	next := time.Date(now.Year(), now.Month(), now.Day(), runClock.Hour(), runClock.Minute(), 0, 0, now.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next, nil
}

func mergeCameraDate(ctx context.Context, cam constant.Camera, date string) error {
	if cam.ID == "" {
		log.Printf("[daily_merge] 跳过合并: cam.id 为空, date=%s", date)
		return nil
	}
	if skipDailyMerge(cam) {
		log.Printf("[%s] 跳过每日合并: mode=%q, date=%s", cam.ID, cam.Mode, date)
		return nil
	}

	dateDir := filepath.Join(constant.DefaultRecordBaseDir, cam.ID, date)
	log.Printf("[%s] 准备执行每日合并: date=%s, mode=%q, dir=%s", cam.ID, date, cam.Mode, dateDir)

	scanResult, err := scanMergeFragments(dateDir)
	if err != nil {
		log.Printf("[%s] 扫描每日合并片段失败: date=%s, dir=%s, err=%v", cam.ID, date, dateDir, err)
		return fmt.Errorf("扫描每日合并片段失败 dir=%s: %w", dateDir, err)
	}
	if scanResult.missing {
		log.Printf("[%s] 跳过每日合并: 日期目录不存在, date=%s, dir=%s", cam.ID, date, dateDir)
		return nil
	}
	log.Printf("[%s] 每日合并扫描完成: date=%s, %s", cam.ID, date, scanResult.summary())

	fragments := scanResult.fragments
	if len(fragments) == 0 {
		log.Printf("[%s] 跳过每日合并: 未找到可合并片段, date=%s, %s", cam.ID, date, scanResult.summary())
		return nil
	}

	groups := groupMergeFragmentsByHour(fragments)
	if len(groups) == 0 {
		log.Printf("[%s] 跳过每日合并: 未找到可按小时分组的片段, date=%s, fragments=%d", cam.ID, date, len(fragments))
		return nil
	}
	log.Printf("[%s] 每日合并按自然小时分组完成: date=%s, groups=%d", cam.ID, date, len(groups))

	var failedGroups []string
	for _, group := range groups {
		if err := mergeOneHourGroup(ctx, cam, date, dateDir, group); err != nil {
			log.Printf("[%s] 每日合并小时分组失败: date=%s, hour=%s, err=%v", cam.ID, date, group.hourKey, err)
			failedGroups = append(failedGroups, fmt.Sprintf("%s: %v", group.hourKey, err))
			if ctx.Err() != nil {
				return err
			}
		}
	}
	if len(failedGroups) > 0 {
		return fmt.Errorf("每日合并部分小时失败: %s", strings.Join(failedGroups, "; "))
	}
	return nil
}

func (g mergeHourGroup) outputNameSuffix() string {
	if g.kind == "motion" {
		return "_motion"
	}
	return ""
}

func mergeOneHourGroup(ctx context.Context, cam constant.Camera, date, dateDir string, group mergeHourGroup) error {
	mergedExt := ".mp4"
	mergedName := fmt.Sprintf("%s_%s%s%s%s", cam.ID, group.hourKey, group.outputNameSuffix(), mergedSuffix, mergedExt)
	mergedPath := filepath.Join(dateDir, mergedName)
	if _, err := os.Stat(mergedPath); err == nil {
		log.Printf("[%s] 跳过每日合并小时分组: 合并文件已存在, date=%s, hour=%s, path=%s",
			cam.ID, date, group.hourKey, mergedPath)
		return nil
	} else if !os.IsNotExist(err) {
		log.Printf("[%s] 检查每日合并小时输出文件失败: date=%s, hour=%s, path=%s, err=%v",
			cam.ID, date, group.hourKey, mergedPath, err)
		return fmt.Errorf("检查每日合并小时输出文件失败 path=%s: %w", mergedPath, err)
	}

	fragments := group.fragments
	if len(fragments) == 0 {
		log.Printf("[%s] 跳过每日合并小时分组: 无片段, date=%s, hour=%s", cam.ID, date, group.hourKey)
		return nil
	}

	log.Printf("[%s] 准备合并自然小时录像: date=%s, hour=%s, fragments=%d, first=%s, last=%s, output=%s",
		cam.ID, date, group.hourKey, len(fragments), filepath.Base(fragments[0]), filepath.Base(fragments[len(fragments)-1]), mergedPath)

	tempOutput := mergedPath + ".tmp" + mergedExt
	listPath, err := writeConcatList(fragments)
	if err != nil {
		log.Printf("[%s] 生成每日合并 concat 列表失败: date=%s, hour=%s, fragments=%d, output=%s, err=%v",
			cam.ID, date, group.hourKey, len(fragments), mergedPath, err)
		return fmt.Errorf("生成每日合并 concat 列表失败: %w", err)
	}
	defer os.Remove(listPath)
	defer os.Remove(tempOutput)
	log.Printf("[%s] 每日合并列表已生成: date=%s, hour=%s, list=%s, fragments=%d, output=%s",
		cam.ID, date, group.hourKey, listPath, len(fragments), mergedPath)

	// FFmpeg 参数，打造纯净完美的 Web 播放格式
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",                                       // 强制覆盖可能存在的异常临时文件
		"-fflags", "+genpts+igndts+discardcorrupt", // 忽略时间戳跳变和轻微损坏的数据包
		"-f", "concat",
		"-safe", "0",
		"-i", listPath,
		"-c:v", "copy", // 视频无损极速拼接 (占用极低 CPU)
		"-c:a", "aac", // 监控音频多为 alaw/ulaw(G.711)，必须转码为 AAC，否则浏览器没声音
		"-max_muxing_queue_size", "8192", // 防止剔除坏帧后音视频轴不同步导致的队列溢出报错
		"-movflags", "+faststart", // 将 moov atom 移到文件头部，完美支持超大文件的 HTTP Range 拖拽秒播
	}
	_, args = appendCodecSpecificMP4Tag(ctx, args, fragments)
	args = append(args, tempOutput)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	log.Printf("[%s] 开始执行每日合并 ffmpeg: date=%s, hour=%s, cmd=%s", cam.ID, date, group.hourKey, cmd.String())
	output, err := cmd.CombinedOutput()
	outputText := strings.TrimSpace(string(output))
	if err != nil {
		log.Printf("[%s] 每日合并 ffmpeg 失败: date=%s, hour=%s, output=%s, err=%v",
			cam.ID, date, group.hourKey, outputText, err)
		if isCorruptFragmentFFmpegOutput(outputText) {
			diagnoseMergeFragments(ctx, cam.ID, date, group.hourKey, fragments)
		}
		return fmt.Errorf("ffmpeg 合并失败 cmd=%s: %v, output=%s", cmd.String(), err, outputText)
	}
	log.Printf("[%s] 每日合并 ffmpeg 完成: date=%s, hour=%s, temp=%s, outputBytes=%d",
		cam.ID, date, group.hourKey, tempOutput, len(output))
	if outputText != "" {
		log.Printf("[%s] 每日合并 ffmpeg 输出: date=%s, hour=%s, output=%s",
			cam.ID, date, group.hourKey, outputText)
		if isCorruptFragmentFFmpegOutput(outputText) {
			diagnoseMergeFragments(ctx, cam.ID, date, group.hourKey, fragments)
			return fmt.Errorf("ffmpeg 合并输出包含损坏片段错误: %s", outputText)
		}
	}

	if err := validateMergedDuration(ctx, fragments, tempOutput); err != nil {
		log.Printf("[%s] 每日合并输出校验失败: date=%s, hour=%s, temp=%s, err=%v",
			cam.ID, date, group.hourKey, tempOutput, err)
		return err
	} else {
		log.Printf("[%s] 每日合并输出校验成功: date=%s, hour=%s, temp=%s, err=%v",
			cam.ID, date, group.hourKey, tempOutput, err)
	}

	if err := os.Rename(tempOutput, mergedPath); err != nil {
		log.Printf("[%s] 每日合并临时文件重命名失败: date=%s, hour=%s, temp=%s, target=%s, err=%v",
			cam.ID, date, group.hourKey, tempOutput, mergedPath, err)
		return fmt.Errorf("每日合并临时文件重命名失败 temp=%s target=%s: %w", tempOutput, mergedPath, err)
	}
	log.Printf("[%s] 每日合并输出文件已落盘: date=%s, hour=%s, path=%s", cam.ID, date, group.hourKey, mergedPath)

	// 合并成功后，删除原始切片
	deleted := 0
	for _, fragment := range fragments {
		if err := os.Remove(fragment); err != nil {
			log.Printf("[%s] 合并成功但删除碎片失败: %s, err=%v", cam.ID, fragment, err)
			continue
		}
		deleted++
	}

	log.Printf("[%s] 已合并 %s %s 点录像，共 %d 个碎片，已删除 %d 个源文件 -> %s",
		cam.ID, date, group.start.Format("15"), len(fragments), deleted, mergedPath)
	return nil
}

func isCorruptFragmentFFmpegOutput(output string) bool {
	output = strings.ToLower(output)
	markers := []string{
		"invalid nal unit size",
		"missing picture in access unit",
		"h264_mp4toannexb filter failed",
		"error during demuxing",
		"invalid data found when processing input",
		"moov atom not found",
	}
	for _, marker := range markers {
		if strings.Contains(output, marker) {
			return true
		}
	}
	return false
}

func diagnoseMergeFragments(ctx context.Context, camID, date, hourKey string, fragments []string) {
	log.Printf("[%s] 开始逐个检测每日合并源片段: date=%s, hour=%s, fragments=%d", camID, date, hourKey, len(fragments))
	bad := 0
	for _, fragment := range fragments {
		if err := probeFragmentReadable(ctx, fragment); err != nil {
			bad++
			log.Printf("[%s] 每日合并源片段疑似损坏: date=%s, hour=%s, fragment=%s, err=%v",
				camID, date, hourKey, fragment, err)
		}
		if ctx.Err() != nil {
			log.Printf("[%s] 逐个检测每日合并源片段已取消: date=%s, hour=%s, checked_bad=%d", camID, date, hourKey, bad)
			return
		}
	}
	log.Printf("[%s] 逐个检测每日合并源片段完成: date=%s, hour=%s, bad=%d, fragments=%d", camID, date, hourKey, bad, len(fragments))
}

type videoDurationProbe func(context.Context, string) (time.Duration, error)

func validateMergedDuration(ctx context.Context, fragments []string, mergedPath string) error {
	return validateMergedDurationWithProbe(ctx, fragments, mergedPath, probeVideoDuration)
}

func validateMergedDurationWithProbe(ctx context.Context, fragments []string, mergedPath string, probe videoDurationProbe) error {
	mergedDuration, err := probe(ctx, mergedPath)
	if err != nil {
		return fmt.Errorf("读取合并输出时长失败 path=%s: %w", mergedPath, err)
	}

	var sourceDuration time.Duration
	var probed int
	for _, fragment := range fragments {
		duration, err := probe(ctx, fragment)
		if err != nil {
			return fmt.Errorf("读取源片段时长失败 path=%s: %w", fragment, err)
		}
		sourceDuration += duration
		probed++
	}
	if probed == 0 {
		return fmt.Errorf("未读取到源片段时长")
	}
	minDuration := time.Duration(float64(sourceDuration) * minMergedDurationRatio)
	if mergedDuration < minDuration {
		return fmt.Errorf("合并输出时长异常: output=%s source=%s fragments=%d threshold=%.0f%%", mergedDuration, sourceDuration, len(fragments), minMergedDurationRatio*100)
	}
	return nil
}

func skipDailyMerge(cam constant.Camera) bool {
	return strings.EqualFold(strings.TrimSpace(cam.Mode), "timelapse")
}

func mergeFragments(dateDir string) ([]string, error) {
	scanResult, err := scanMergeFragments(dateDir)
	if err != nil {
		return nil, err
	}
	return scanResult.fragments, nil
}

func scanMergeFragments(dateDir string) (mergeFragmentScanResult, error) {
	result := mergeFragmentScanResult{dateDir: dateDir}
	entries, err := os.ReadDir(dateDir)
	if err != nil {
		if os.IsNotExist(err) {
			result.missing = true
			return result, nil
		}
		return result, err
	}
	result.totalEntries = len(entries)

	for _, entry := range entries {
		if entry.IsDir() {
			result.skippedDirs++
			continue
		}
		name := entry.Name()
		if _, ok := mergeFragmentStartTime(name); !ok && mergeFragmentSkipReason(name) == "" {
			result.skippedNoTime++
			continue
		}
		switch mergeFragmentSkipReason(name) {
		case "":
			result.fragments = append(result.fragments, filepath.Join(dateDir, name))
		case "merged":
			result.skippedMerged++
		case "temp":
			result.skippedTemp++
		case "unsupported_ext":
			result.skippedUnsupportedExt++
		default:
			continue
		}
	}

	sortMergeFragments(result.fragments)
	return result, nil
}

func (r mergeFragmentScanResult) summary() string {
	return fmt.Sprintf("dir=%s, entries=%d, selected=%d, skipped_dirs=%d, skipped_ext=%d, skipped_merged=%d, skipped_tmp=%d, skipped_no_time=%d",
		r.dateDir, r.totalEntries, len(r.fragments), r.skippedDirs, r.skippedUnsupportedExt, r.skippedMerged, r.skippedTemp, r.skippedNoTime)
}

func isMergeFragmentName(name string) bool {
	return mergeFragmentSkipReason(name) == ""
}

func mergeFragmentSkipReason(name string) string {
	if strings.Contains(name, mergedSuffix) {
		return "merged"
	}
	if strings.Contains(name, ".tmp") {
		return "temp"
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".ts" && ext != ".mp4" {
		return "unsupported_ext"
	}
	return ""
}

func sortMergeFragments(fragments []string) {
	sort.SliceStable(fragments, func(i, j int) bool {
		leftTime, leftOK := mergeFragmentStartTime(fragments[i])
		rightTime, rightOK := mergeFragmentStartTime(fragments[j])
		if leftOK && rightOK && !leftTime.Equal(rightTime) {
			return leftTime.Before(rightTime)
		}
		if leftOK != rightOK {
			return leftOK
		}
		return filepath.Base(fragments[i]) < filepath.Base(fragments[j])
	})
}

func groupMergeFragmentsByHour(fragments []string) []mergeHourGroup {
	groupsByKey := make(map[string][]string)
	startByKey := make(map[string]time.Time)
	kindByKey := make(map[string]string)
	for _, fragment := range fragments {
		start, ok := mergeFragmentStartTime(fragment)
		if !ok {
			continue
		}
		hourStart := time.Date(start.Year(), start.Month(), start.Day(), start.Hour(), 0, 0, 0, start.Location())
		hourKey := hourStart.Format("2006-01-02_15")
		kind := mergeFragmentKind(fragment)
		groupKey := hourKey + "|" + kind
		groupsByKey[groupKey] = append(groupsByKey[groupKey], fragment)
		startByKey[groupKey] = hourStart
		kindByKey[groupKey] = kind
	}

	groups := make([]mergeHourGroup, 0, len(groupsByKey))
	for groupKey, groupFragments := range groupsByKey {
		sortMergeFragments(groupFragments)
		hourKey := strings.SplitN(groupKey, "|", 2)[0]
		groups = append(groups, mergeHourGroup{
			hourKey:   hourKey,
			start:     startByKey[groupKey],
			kind:      kindByKey[groupKey],
			fragments: groupFragments,
		})
	}
	sort.Slice(groups, func(i, j int) bool {
		if !groups[i].start.Equal(groups[j].start) {
			return groups[i].start.Before(groups[j].start)
		}
		return groups[i].kind < groups[j].kind
	})
	return groups
}

func mergeFragmentKind(path string) string {
	if strings.Contains(filepath.Base(path), "_motion") {
		return "motion"
	}
	return "normal"
}

func mergeFragmentStartTime(path string) (time.Time, bool) {
	for _, raw := range mergeFragmentTimePattern.FindAllString(filepath.Base(path), -1) {
		layout := "2006-01-02_150405"
		if strings.Contains(raw, "-") && len(raw) == len("2006-01-02_15-04-05") {
			layout = "2006-01-02_15-04-05"
		}
		start, err := time.ParseInLocation(layout, raw, time.Local)
		if err == nil {
			return start, true
		}
	}
	return time.Time{}, false
}

func writeConcatList(fragments []string) (string, error) {
	file, err := os.CreateTemp("", "camkeep-merge-*.txt")
	if err != nil {
		return "", err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, fragment := range fragments {
		absFragment, err := filepath.Abs(fragment)
		if err != nil {
			return "", err
		}
		if _, err := fmt.Fprintf(writer, "file '%s'\n", escapeConcatPath(absFragment)); err != nil {
			return "", err
		}
	}
	if err := writer.Flush(); err != nil {
		return "", err
	}
	return file.Name(), nil
}

func escapeConcatPath(path string) string {
	return strings.ReplaceAll(path, "'", "'\\''")
}
