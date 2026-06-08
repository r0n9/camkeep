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

			caps, err := probeOnvifCapabilities(probeCtx, candidate)
			if err != nil {
				service.UpdateOnvifProbeError(candidate, err)
				log.Printf("[%s] ONVIF capability probe 失败: %v", candidate.ID, err)
				return
			}

			service.UpdateOnvifProbeResult(candidate, caps)
			log.Printf("[%s] ONVIF capability probe 完成: media=%t ptz=%t imaging=%t event=%t pullpoint=%t profile=%t source=%t",
				candidate.ID,
				caps.MediaXAddr != "",
				caps.PTZXAddr != "",
				caps.ImagingXAddr != "",
				caps.EventXAddr != "",
				caps.PullPointSupport,
				caps.ProfileToken != "",
				caps.VideoSourceToken != "",
			)
		}()
	}
}

func probeOnvifCapabilities(ctx context.Context, candidate onvif.Candidate) (onvif.Capabilities, error) {
	client := onvif.NewClient(candidate)
	caps, err := client.GetCapabilities(ctx)
	if err != nil {
		return onvif.Capabilities{}, err
	}
	if caps.MediaXAddr != "" && (caps.PTZXAddr != "" || caps.ImagingXAddr != "") {
		profiles, err := client.GetProfiles(ctx, caps.MediaXAddr)
		if err != nil {
			log.Printf("[%s] ONVIF media profile probe 失败: %v", candidate.ID, err)
		} else if profile, ok := onvif.SelectPTZProfile(profiles); ok {
			caps.ProfileToken = profile.Token
			caps.ProfileName = profile.Name
			if profile.VideoSourceToken != "" {
				caps.VideoSourceToken = profile.VideoSourceToken
			}
		}
		if caps.VideoSourceToken == "" {
			if profile, ok := onvif.SelectVideoSourceProfile(profiles); ok {
				caps.VideoSourceToken = profile.VideoSourceToken
			}
		}
	}
	return caps, nil
}
