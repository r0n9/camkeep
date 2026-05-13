package app

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/r0n9/camkeep/constant"
	"gopkg.in/yaml.v3"
)

// loadOrInitConfig 如果配置文件不存在则生成一个带示例的默认配置
func loadOrInitConfig() constant.Config {
	os.MkdirAll(filepath.Dir(constant.ConfigFilePath), 0755)

	data, err := os.ReadFile(constant.ConfigFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Println("conf.yaml 不存在，自动创建默认模板配置...")

			// 定义默认配置模板
			defaultCfg := constant.Config{
				DailyMerge: constant.DailyMergeConfig{
					Enabled: false,
					Time:    "03:30",
				},
				Cameras: []constant.Camera{
					{
						ID:                         "camkeep",
						RTSPUrl:                    "rtsp://admin:password@192.168.1.100:554/live",
						RetentionDays:              7,
						SegmentDuration:            600,
						Format:                     "ts",
						MinSizeKb:                  1024,
						RecordTime:                 "00:00-23:59",
						Mode:                       "normal",
						MotionDetect:               false,
						MotionDetectRatioThreshold: 0.01,
					},
				},
			}

			// 将默认配置序列化为 YAML
			out, err := yaml.Marshal(defaultCfg)
			if err != nil {
				log.Printf("生成默认配置失败: %v", err)
				return defaultCfg
			}

			// 可以在文件开头写入一些注释说明（可选）
			header := []byte("# CamKeep 配置文件\n# mode: normal (普通录制), timelapse (延时摄影)\n")
			finalOut := append(header, out...)

			if err := os.WriteFile(constant.ConfigFilePath, finalOut, 0644); err != nil {
				log.Printf("写入配置文件失败: %v", err)
			}

			return defaultCfg
		}
		log.Fatalf("读取配置文件失败: %v", err)
	}

	var c constant.Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		log.Fatalf("解析配置文件失败: %v", err)
	}

	// 启动时如果发现文件里有重复 ID，自动去重
	c = validateAndFixConfig(c)
	return c
}

func parseConfigYAML(yamlBytes []byte) (constant.Config, error) {
	var cfg constant.Config
	if strings.TrimSpace(string(yamlBytes)) == "" {
		return cfg, fmt.Errorf("conf.yaml 不能为空")
	}

	decoder := yaml.NewDecoder(bytes.NewReader(yamlBytes))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("YAML 格式有误: %w", err)
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return cfg, fmt.Errorf("YAML 格式有误: %w", err)
		}
		return cfg, fmt.Errorf("conf.yaml 只能包含一个 YAML 文档")
	}

	if err := checkDuplicateIDs(cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// checkDuplicateIDs 检查配置中是否有重复的摄像头ID (用于 API 严格校验)
func checkDuplicateIDs(cfg constant.Config) error {
	if cfg.DailyMerge.Enabled && cfg.DailyMerge.Time == "" {
		return fmt.Errorf("daily_merge.time 不能为空")
	}
	if cfg.DailyMerge.Time != "" {
		if _, err := time.Parse("15:04", cfg.DailyMerge.Time); err != nil {
			return fmt.Errorf("daily_merge.time 必须使用 HH:mm 格式")
		}
	}

	seen := make(map[string]bool)
	for _, cam := range cfg.Cameras {
		if cam.ID == "" {
			return fmt.Errorf("摄像头 ID 不能为空")
		}
		if seen[cam.ID] {
			return fmt.Errorf("发现重复的摄像头 ID: %s", cam.ID)
		}
		seen[cam.ID] = true
	}
	return nil
}

// validateAndFixConfig 修复文件读取时的配置 (用于启动时静默去重防崩溃)
func validateAndFixConfig(cfg constant.Config) constant.Config {
	if cfg.DailyMerge.Time == "" {
		cfg.DailyMerge.Time = "03:30"
	}

	seen := make(map[string]bool)
	var uniqueCams []constant.Camera

	for _, cam := range cfg.Cameras {
		if cam.ID == "" {
			log.Println("警告: 发现空 ID 的摄像头配置，已跳过")
			continue
		}
		if seen[cam.ID] {
			log.Printf("警告: 发现重复的摄像头 ID [%s]，已自动去重", cam.ID)
			continue
		}
		seen[cam.ID] = true

		// 预置默认录像策略。如果用户在 conf.yaml 中没写，就走这里的兜底。
		if cam.RetentionDays == 0 {
			cam.RetentionDays = 7
		}
		if cam.SegmentDuration == 0 {
			cam.SegmentDuration = 600
		}
		if cam.Format == "" {
			cam.Format = "ts"
		}
		if cam.MinSizeKb == 0 {
			cam.MinSizeKb = 1024
		}
		if cam.RecordTime == "" {
			cam.RecordTime = "00:00-23:59"
		}
		if cam.Mode == "" {
			cam.Mode = "normal" // 普通录制模式
		}

		uniqueCams = append(uniqueCams, cam)
	}
	cfg.Cameras = uniqueCams
	return cfg
}
