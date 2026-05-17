package app

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/r0n9/camkeep/constant"
)

func startWebServer() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	r.Static("/static", "./static")

	// 1. 渲染前端页面
	r.LoadHTMLGlob("template/*")

	r.GET("/", handleIndex)
	r.GET("/api/status", handleStatus)
	r.GET("/api/config", handleGetConfig)
	r.GET("/api/config/form", handleGetConfigForm)
	r.POST("/api/config/form/parse", handleParseConfigForm)
	r.POST("/api/config/form", handleSaveConfigForm)
	r.POST("/api/config/validate", handleValidateConfig)
	r.POST("/api/config", handleSaveConfig)
	r.POST("/api/camera/:id/:action", handleCameraAction)
	r.GET("/api/records/:id", handleRecords)
	r.GET("/api/record/probe", handleProbeRecord)
	r.GET("/api/record/download", handleDownloadRecord)
	r.DELETE("/api/record", handleDeleteRecord)
	r.GET("/api/go2rtc/unmanaged", handleUnmanagedStreams)

	r.StaticFS("/play", http.Dir(constant.DefaultRecordBaseDir))
	r.GET("/play_hls/*filepath", handlePlayHLS)
	r.GET("/play_transcode/*filepath", handlePlayTranscode)
	r.GET("/play_remux/*filepath", handlePlayRemux)

	// 让 CamKeep 作为统一网关，直接代理 go2rtc 的全能自适应直播功能
	go2rtcURL, _ := url.Parse(fmt.Sprintf("http://%s:%d", constant.DefaultGo2rtcHost, constant.DefaultGo2rtcApiPort))
	go2rtcProxy := httputil.NewSingleHostReverseProxy(go2rtcURL)

	// 1. 代理 go2rtc 的自适应播放器页面及内部依赖的 JS (解决跨域和单端口问题)
	r.GET("/stream.html", gin.WrapH(go2rtcProxy))
	r.GET("/video-stream.js", gin.WrapH(go2rtcProxy))
	r.GET("/video-rtc.js", gin.WrapH(go2rtcProxy))
	r.GET("/webrtc.html", gin.WrapH(go2rtcProxy))

	// 2. 代理流媒体协商相关的 API (含 WebSocket 支持，自动兼容不同浏览器的降级)
	r.Any("/api/ws", gin.WrapH(go2rtcProxy))
	r.Any("/api/webrtc", gin.WrapH(go2rtcProxy))
	r.Any("/api/stream.mp4", gin.WrapH(go2rtcProxy))
	r.Any("/api/stream.m3u8", gin.WrapH(go2rtcProxy))

	// 5. 【全新】WebRTC 代理接口 (替代原来的 FLV 转码)
	r.POST("/webrtc/:id", handleWebRTCProxy)

	log.Println("Web 管理后台已启动: http://localhost:9110")
	r.Run(":9110")
}
