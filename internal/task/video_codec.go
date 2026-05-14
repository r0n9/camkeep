package task

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func probeVideoCodecName(ctx context.Context, videoPath string) (string, error) {
	cmd := exec.CommandContext(
		ctx,
		"ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_name",
		"-of", "json",
		videoPath,
	)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return videoCodecNameFromJSON(output)
}

func videoCodecNameFromJSON(output []byte) (string, error) {
	var result struct {
		Streams []struct {
			CodecName string `json:"codec_name"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return "", err
	}
	if len(result.Streams) == 0 {
		return "", fmt.Errorf("未找到视频流")
	}
	return result.Streams[0].CodecName, nil
}

func probeVideoDuration(ctx context.Context, videoPath string) (time.Duration, error) {
	cmd := exec.CommandContext(
		ctx,
		"ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "json",
		videoPath,
	)
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	return videoDurationFromJSON(output)
}

func videoDurationFromJSON(output []byte) (time.Duration, error) {
	var result struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return 0, err
	}
	if result.Format.Duration == "" || result.Format.Duration == "N/A" {
		return 0, fmt.Errorf("未找到视频时长")
	}
	seconds, err := strconv.ParseFloat(result.Format.Duration, 64)
	if err != nil {
		return 0, err
	}
	if seconds <= 0 {
		return 0, fmt.Errorf("无效视频时长: %s", result.Format.Duration)
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

func probeFragmentReadable(ctx context.Context, videoPath string) error {
	cmd := exec.CommandContext(
		ctx,
		"ffmpeg",
		"-hide_banner",
		"-v", "error",
		"-i", videoPath,
		"-map", "0:v:0",
		"-c", "copy",
		"-f", "null",
		"-",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg 读取失败: %v, output=%s", err, strings.TrimSpace(string(output)))
	}
	if outputText := strings.TrimSpace(string(output)); outputText != "" {
		return fmt.Errorf("ffmpeg 读取输出异常: %s", outputText)
	}
	return nil
}

func isHEVCCodec(codec string) bool {
	codec = strings.ToLower(codec)
	return codec == "hevc" || codec == "h265" || codec == "h.265"
}

func appendCodecSpecificMP4Tag(ctx context.Context, args []string, fragments []string) []string {
	if len(fragments) == 0 {
		return args
	}
	codec, err := probeVideoCodecName(ctx, fragments[0])
	if err != nil {
		return args
	}
	if isHEVCCodec(codec) {
		args = append(args, "-tag:v", "hvc1")
	}
	return args
}
