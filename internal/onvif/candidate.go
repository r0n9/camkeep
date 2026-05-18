package onvif

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"

	"github.com/r0n9/camkeep/constant"
)

const (
	SourceTypeDirect = "direct"
	SourceTypeGo2rtc = "go2rtc"

	defaultDeviceServicePath = "/onvif/device_service"
)

type Candidate struct {
	ID              string
	SourceType      string
	SourceURL       string
	Endpoint        string
	Username        string
	Password        string
	ManagedByGo2rtc bool
}

func BuildCandidates(cfg constant.Config, go2rtcSources map[string][]string) []Candidate {
	candidates := make([]Candidate, 0, len(cfg.Cameras))
	for _, cam := range cfg.Cameras {
		candidate, ok := CandidateFromCamera(cam, go2rtcSources[cam.ID])
		if ok {
			candidates = append(candidates, candidate)
		}
	}
	return candidates
}

func CandidateFromCamera(cam constant.Camera, go2rtcSources []string) (Candidate, bool) {
	if constant.CameraManagedByGo2rtc(cam) {
		source, ok := FirstONVIFSource(go2rtcSources)
		if !ok {
			return Candidate{}, false
		}
		return newCandidate(cam.ID, SourceTypeGo2rtc, source, true), true
	}

	source, ok := ExtractONVIFSource(cam.EffectiveStreamURL())
	if !ok {
		return Candidate{}, false
	}
	return newCandidate(cam.ID, SourceTypeDirect, source, false), true
}

func HasONVIFSource(sources []string) bool {
	_, ok := FirstONVIFSource(sources)
	return ok
}

func FirstONVIFSource(sources []string) (string, bool) {
	for _, source := range sources {
		onvifSource, ok := ExtractONVIFSource(source)
		if ok {
			return onvifSource, true
		}
	}
	return "", false
}

func ExtractONVIFSource(source string) (string, bool) {
	source = strings.TrimSpace(source)
	if source == "" {
		return "", false
	}

	if strings.HasPrefix(strings.ToLower(source), "onvif://") {
		return source, true
	}

	colon := strings.Index(source, ":")
	schemeSep := strings.Index(source, "://")
	if colon > 0 && (schemeSep < 0 || colon < schemeSep) {
		return ExtractONVIFSource(source[colon+1:])
	}
	return "", false
}

func MaskSourceURL(source string) string {
	u, err := url.Parse(strings.TrimSpace(source))
	if err != nil || u.User == nil {
		return strings.TrimSpace(source)
	}

	username := u.User.Username()
	if _, hasPassword := u.User.Password(); hasPassword {
		u.User = url.UserPassword(username, "redacted")
	} else {
		u.User = url.User(username)
	}
	return u.String()
}

func SourceFingerprint(source string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(source)))
	return hex.EncodeToString(sum[:])
}

func newCandidate(id, sourceType, sourceURL string, managedByGo2rtc bool) Candidate {
	return Candidate{
		ID:              id,
		SourceType:      sourceType,
		SourceURL:       sourceURL,
		Endpoint:        DeviceServiceEndpoint(sourceURL),
		Username:        sourceUsername(sourceURL),
		Password:        sourcePassword(sourceURL),
		ManagedByGo2rtc: managedByGo2rtc,
	}
}

func DeviceServiceEndpoint(source string) string {
	u, err := url.Parse(strings.TrimSpace(source))
	if err != nil || !strings.EqualFold(u.Scheme, "onvif") {
		return ""
	}

	u.Scheme = "http"
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	if u.Path == "" || u.Path == "/" {
		u.Path = defaultDeviceServicePath
	}
	return u.String()
}

func sourceUsername(source string) string {
	u, err := url.Parse(strings.TrimSpace(source))
	if err != nil || u.User == nil {
		return ""
	}
	return u.User.Username()
}

func sourcePassword(source string) string {
	u, err := url.Parse(strings.TrimSpace(source))
	if err != nil || u.User == nil {
		return ""
	}
	password, _ := u.User.Password()
	return password
}
