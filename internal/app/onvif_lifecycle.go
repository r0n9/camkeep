package app

import (
	"context"
	"log"

	"github.com/r0n9/camkeep/constant"
	"github.com/r0n9/camkeep/internal/onvif"
	"github.com/r0n9/camkeep/internal/service"
	"github.com/r0n9/camkeep/internal/task"
)

func syncOnvifCandidates(ctx context.Context, cfg constant.Config) {
	configSources := loadGo2rtcConfigStreamSources(constant.Go2rtcConfigFilePath, constant.LegacyGo2rtcConfigFilePath)
	candidates := onvif.BuildCandidates(cfg, configSources)
	service.ReplaceOnvifCandidates(candidates)
	task.StartOnvifCapabilityProbes(ctx, candidates)
	log.Printf("ONVIF 控制候选设备同步完成: %d 台", len(candidates))
}
