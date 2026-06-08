package app

import (
	"fmt"
	"log"
	"net/http/httputil"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/r0n9/camkeep/constant"
)

func startWebServer() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	ensureWebAuthInitialized()

	r.Static("/static", "./static")

	// 1. 渲染前端页面
	r.LoadHTMLGlob("template/*")

	r.GET("/login", handleLoginPage(webAuth))
	r.POST("/login", handleLoginPost(webAuth))
	r.POST("/logout", handleLogout(webAuth))

	protected := r.Group("/")
	protected.Use(authRequired(webAuth))

	protected.GET("/", handleIndex)
	protected.GET("/api/me", handleMe(webAuth))
	protected.GET("/api/status", handleStatus)
	protected.GET("/api/camera/:id/onvif/event-summary", handleCameraOnvifEventSummary)
	protected.POST("/api/camera/:id/onvif/event-summary/release", handleReleaseCameraOnvifEventSummaryLease)
	protected.GET("/api/camera/:id/cover", handleCameraCover)
	protected.GET("/api/records/:id", handleRecords)
	protected.GET("/api/record/probe", handleProbeRecord)
	protected.GET("/api/record/download", handleDownloadRecord)

	protected.GET("/play/*filepath", handlePlayRecord)
	protected.GET("/play_hls/*filepath", handlePlayHLS)
	protected.GET("/play_transcode/*filepath", handlePlayTranscode)
	protected.GET("/play_remux/*filepath", handlePlayRemux)

	admin := protected.Group("/")
	admin.Use(adminRequired(webAuth))
	admin.GET("/config-usage", handleConfigUsagePage)
	admin.GET("/api/update/check", handleUpdateCheck)
	admin.GET("/api/config", handleGetConfig)
	admin.GET("/api/config/form", handleGetConfigForm)
	admin.POST("/api/config/form/parse", handleParseConfigForm)
	admin.POST("/api/config/form", handleSaveConfigForm)
	admin.POST("/api/config/validate", handleValidateConfig)
	admin.POST("/api/config", handleSaveConfig)
	admin.POST("/api/camera/:id/start", handleCameraActionFor("start"))
	admin.POST("/api/camera/:id/stop", handleCameraActionFor("stop"))
	admin.POST("/api/camera/:id/auto", handleCameraActionFor("auto"))
	admin.GET("/api/onvif/status", handleOnvifStatus)
	admin.GET("/api/camera/:id/onvif", handleCameraOnvifStatus)
	admin.POST("/api/camera/:id/onvif/event-test", handleOnvifEventTest)
	admin.GET("/api/camera/:id/ptz/status", handlePTZStatus)
	admin.POST("/api/camera/:id/ptz/move", handlePTZMove)
	admin.POST("/api/camera/:id/ptz/stop", handlePTZStop)
	admin.POST("/api/camera/:id/ptz/focus", handlePTZFocus)
	admin.POST("/api/camera/:id/ptz/iris", handlePTZIris)
	admin.DELETE("/api/record", handleDeleteRecord)
	admin.GET("/api/go2rtc/unmanaged", handleUnmanagedStreams)
	admin.GET("/api/users", handleListUsers(webAuth))
	admin.POST("/api/users", handleCreateUser(webAuth))
	admin.PATCH("/api/users/:id", handleUpdateUser(webAuth))
	admin.POST("/api/users/:id/password", handleResetUserPassword(webAuth))
	admin.DELETE("/api/users/:id", handleDeleteUser(webAuth))

	// 让 CamKeep 作为统一网关，直接代理 go2rtc 的全能自适应直播功能
	go2rtcURL, _ := url.Parse(fmt.Sprintf("http://%s:%d", constant.DefaultGo2rtcHost, constant.DefaultGo2rtcApiPort))
	go2rtcProxy := httputil.NewSingleHostReverseProxy(go2rtcURL)
	go2rtcProxyHandler := gin.WrapH(go2rtcProxy)

	// 1. 代理 go2rtc 的自适应播放器页面及内部依赖的 JS (解决跨域和单端口问题)
	protected.GET("/stream.html", requireQueryCameraAccess("src"), go2rtcProxyHandler)
	protected.GET("/video-stream.js", go2rtcProxyHandler)
	protected.GET("/video-rtc.js", go2rtcProxyHandler)
	protected.GET("/webrtc.html", go2rtcProxyHandler)

	// 2. 代理流媒体协商相关的 API (含 WebSocket 支持，自动兼容不同浏览器的降级)
	protected.Any("/api/ws", requireQueryCameraAccess("src"), go2rtcProxyHandler)
	protected.Any("/api/webrtc", requireQueryCameraAccess("src"), go2rtcProxyHandler)
	protected.Any("/api/stream.mp4", requireQueryCameraAccess("src"), go2rtcProxyHandler)
	protected.Any("/api/stream.m3u8", requireQueryCameraAccess("src"), go2rtcProxyHandler)

	// 5. 【全新】WebRTC 代理接口 (替代原来的 FLV 转码)
	protected.POST("/webrtc/:id", handleWebRTCProxy)

	if webAuth.isEnabled() {
		log.Printf("Web 登录鉴权已启用，用户文件: %s", constantUsersFilePath)
	} else {
		log.Println("Web 登录鉴权未启用；创建本地用户后启用")
	}
	log.Println("Web 管理后台已启动: http://localhost:9110")
	r.Run(":9110")
}

func ensureWebAuthInitialized() {
	if webAuth.UserStore != nil {
		return
	}
	webAuth = loadAuthConfigFromEnv()
}
