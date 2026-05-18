package task

import (
	"context"
	"log"
	"time"

	"github.com/r0n9/camkeep/internal/onvif"
	"github.com/r0n9/camkeep/internal/service"
)

const onvifProbeTimeout = 5 * time.Second

func StartOnvifCapabilityProbes(ctx context.Context, candidates []onvif.Candidate) {
	for _, candidate := range candidates {
		candidate := candidate
		service.MarkOnvifProbeStarted(candidate)

		go func() {
			probeCtx, cancel := context.WithTimeout(ctx, onvifProbeTimeout)
			defer cancel()

			client := onvif.NewClient(candidate)
			caps, err := client.GetCapabilities(probeCtx)
			if err != nil {
				service.UpdateOnvifProbeError(candidate, err)
				log.Printf("[%s] ONVIF capability probe 失败: %v", candidate.ID, err)
				return
			}

			service.UpdateOnvifProbeResult(candidate, caps)
			log.Printf("[%s] ONVIF capability probe 完成: media=%t ptz=%t event=%t pullpoint=%t",
				candidate.ID,
				caps.MediaXAddr != "",
				caps.PTZXAddr != "",
				caps.EventXAddr != "",
				caps.PullPointSupport,
			)
		}()
	}
}
