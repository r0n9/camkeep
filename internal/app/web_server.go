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
	webAuth = loadAuthConfigFromEnv()
	if webAuth.Enabled {
		log.Printf("Web 登录鉴权已启用，管理员用户: %s", webAuth.Username)
	} else {
		log.Println("Web 登录鉴权未启用；设置 CAMKEEP_AUTH_PASSWORD 后启用")
	}

	r.Static("/static", "./static")

	// 1. 渲染前端页面
	r.LoadHTMLGlob("template/*")

	r.GET("/login", handleLoginPage(webAuth))
	r.POST("/login", handleLoginPost(webAuth))
	r.POST("/logout", handleLogout(webAuth))

	protected := r.Group("/")
	protected.Use(authRequired(webAuth))

	protected.GET("/", handleIndex)
	protected.GET("/api/status", handleStatus)
	protected.GET("/api/config", handleGetConfig)
	protected.GET("/api/config/form", handleGetConfigForm)
	protected.POST("/api/config/form/parse", handleParseConfigForm)
	protected.POST("/api/config/form", handleSaveConfigForm)
	protected.POST("/api/config/validate", handleValidateConfig)
	protected.POST("/api/config", handleSaveConfig)
	protected.POST("/api/camera/:id/:action", handleCameraAction)
	protected.GET("/api/records/:id", handleRecords)
	protected.GET("/api/record/probe", handleProbeRecord)
	protected.GET("/api/record/download", handleDownloadRecord)
	protected.DELETE("/api/record", handleDeleteRecord)
	protected.GET("/api/go2rtc/unmanaged", handleUnmanagedStreams)

	protected.StaticFS("/play", http.Dir(constant.DefaultRecordBaseDir))
	protected.GET("/play_hls/*filepath", handlePlayHLS)
	protected.GET("/play_transcode/*filepath", handlePlayTranscode)
	protected.GET("/play_remux/*filepath", handlePlayRemux)

	// 让 CamKeep 作为统一网关，直接代理 go2rtc 的全能自适应直播功能
	go2rtcURL, _ := url.Parse(fmt.Sprintf("http://%s:%d", constant.DefaultGo2rtcHost, constant.DefaultGo2rtcApiPort))
	go2rtcProxy := httputil.NewSingleHostReverseProxy(go2rtcURL)

	// 1. 代理 go2rtc 的自适应播放器页面及内部依赖的 JS (解决跨域和单端口问题)
	protected.GET("/stream.html", gin.WrapH(go2rtcProxy))
	protected.GET("/video-stream.js", gin.WrapH(go2rtcProxy))
	protected.GET("/video-rtc.js", gin.WrapH(go2rtcProxy))
	protected.GET("/webrtc.html", gin.WrapH(go2rtcProxy))

	// 2. 代理流媒体协商相关的 API (含 WebSocket 支持，自动兼容不同浏览器的降级)
	protected.Any("/api/ws", gin.WrapH(go2rtcProxy))
	protected.Any("/api/webrtc", gin.WrapH(go2rtcProxy))
	protected.Any("/api/stream.mp4", gin.WrapH(go2rtcProxy))
	protected.Any("/api/stream.m3u8", gin.WrapH(go2rtcProxy))

	// 5. 【全新】WebRTC 代理接口 (替代原来的 FLV 转码)
	protected.POST("/webrtc/:id", handleWebRTCProxy)

	log.Println("Web 管理后台已启动: http://localhost:9110")
	r.Run(":9110")
}
