# CamKeep 飞牛OS（fnOS）原生应用网关集成与双轨共存实施方案

> **文档状态**：待评审 / 实施就绪  
> **创建日期**：2026-11-11  
> **适配版本**：CamKeep v1.7.6+ / 飞牛 fnOS 1.2.0+

---

## 1. 背景与目标

### 1.1 核心目标
1. **深度集成 Web 桌面**：将 CamKeep 桌面图标由当前的外链跳转（`type: "url"` 跳出新标签页），升级为飞牛原生窗口化桌面应用（`type: "iframe"` 内部多任务悬浮窗口）。
2. **复用系统穿透（fnConnect）**：接入飞牛统一反向代理网关（Trim Gateway），外网用户通过飞牛自带的 **fnConnect 穿透通道即可免公网 IP、免端口映射流畅访问 CamKeep**（包含实时直播、WebSocket 协商、录像切片与历史回放）。
3. **实现免密单点登录（SSO）**：经由飞牛统一网关访问时，自动信任飞牛系统的登录身份（根据网关注入的 `X-Trim-Isadmin` 等可信头自动赋予权限），无缝进入控制台。
4. **与现有外部独立端口（`:9110`）100% 兼容共存**：
   - 保持现有 Docker、通用 Linux、群晖、威联通等平台的使用习惯不变；
   - 局域网仍可通过 `http://<NAS_IP>:9110` 直接访问，沿用原有的 Cookie + `users.json` 密码鉴权体系。

---

## 2. 总体架构与双轨共存设计

### 2.1 架构拓扑图

```
                           [外网用户 / 飞牛手机 App / fnConnect]
                                           │
                                           ▼ (统一域名，免开放高位端口)
                                    [fnOS 统一网关] 
                         (Nginx / Trim Gateway，已校验系统登录态)
                                           │
                                           ▼ (前缀 /app/camkeep/*)
                                  [${TRIM_APPDEST}/app.sock]
                                           │
      [局域网独立访问]                       │ (通道 1: Unix Domain Socket)
             │                             │
             ▼ (:9110)                     │
    [通道 2: TCP Listener]                 │
             │                             │
             └─────────────┬───────────────┘
                           │
                           ▼
               [CamKeep 接入与网关中间件]
                           │
          ┌────────────────┴────────────────┐
          ▼                                 ▼
   【Unix Socket 来源】              【TCP :9110 来源】
   - 剥离 /app/camkeep 前缀          - 保持根路径路由
   - 提取 X-Trim-* 可信头            - 严格忽略 X-Trim-* 伪造头
   - 飞牛登录态免密放行 (SSO)        - 强制 Cookie / users.json 鉴权
          └────────────────┬────────────────┘
                           │
                           ▼
                [Gin Web 引擎 & 业务路由]
                           │
      ┌────────────────────┼────────────────────┐
      ▼                    ▼                    ▼
 [RESTful API]       [HLS/MP4 回放流]     [go2rtc 实时流代理]
 (/api/status, ...)  (/play/*, ...)       (/stream.html, /api/ws, /webrtc/:id)
```

### 2.2 安全隔离与信任边界
* **安全红线**：飞牛统一网关通过 Unix Domain Socket 进行通信。由于 Socket 文件在宿主机上受 Linux 文件权限保护（仅允许系统网关和 CamKeep 访问），**来自 Socket 的请求视为受信任请求**；
* **防伪造设计**：来自常规 TCP 端口（`:9110`）的外部请求，**中间件强制清除或忽略请求头中的 `X-Trim-*` 字段**，防止局域网内任意客户端伪造管理员身份免密登录。

---

## 3. 详细实施方案与核心代码改动

### 3.1 接入层改造：TCP + Unix Socket 双监听器

修改文件：[`internal/app/web_server.go`](internal/app/web_server.go)

在 `startWebServer()` 中，支持同时启动 TCP 与 Unix Socket 监听，且在程序退出时优雅关闭并清理 socket 临时文件。

```go
package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	// ... 现有依赖
)

func startWebServer() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	ensureWebAuthInitialized()

	// 注册全局网关处理与前缀剥离中间件
	const gatewayPrefix = "/app/camkeep"
	r.Use(gatewayContextMiddleware(gatewayPrefix))

	// 注册前端静态资源与路由（路径适配见 3.3 节）
	registerRoutes(r)

	// ------------------ 1. 启动常规 TCP 监听 (:9110) ------------------
	tcpAddr := fmt.Sprintf(":%d", constant.DefaultWebPort)
	tcpListener, err := net.Listen("tcp", tcpAddr)
	if err != nil {
		log.Fatalf("致命错误: 无法监听 TCP 端口 %s: %v", tcpAddr, err)
	}
	defer tcpListener.Close()

	tcpServer := &http.Server{Handler: r}
	go func() {
		log.Printf("Web 管理后台已启动 (TCP 模式): http://localhost%s", tcpAddr)
		if err := tcpServer.Serve(tcpListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("TCP Server 退出异常: %v", err)
		}
	}()

	// ------------------ 2. 探测并启动 Unix Socket 监听 ------------------
	// 由 fnOS 传入环境变量 CAMKEEP_SOCK，或直接指定路径
	sockPath := os.Getenv("CAMKEEP_SOCK")
	var unixServer *http.Server
	if sockPath != "" {
		_ = os.Remove(sockPath) // 清理历史残留 socket
		unixListener, err := net.Listen("unix", sockPath)
		if err != nil {
			log.Printf("警告: 无法监听 Unix Socket %s: %v", sockPath, err)
		} else {
			defer unixListener.Close()
			defer os.Remove(sockPath)
			// 必须赋予 0666 权限，允许 fnOS 统一网关进程 (nginx/trim) 读写
			_ = os.Chmod(sockPath, 0666)

			unixServer = &http.Server{Handler: r}
			go func() {
				log.Printf("fnOS 统一网关已接入 (Unix Socket): %s (前缀: %s)", sockPath, gatewayPrefix)
				if err := unixServer.Serve(unixListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
					log.Printf("Unix Socket Server 退出异常: %v", err)
				}
			}()
		}
	}

	// ------------------ 3. 优雅退出控制 ------------------
	// 挂入主流程退出通道
	<-ctxGlobal.Done()
	log.Println("正在关闭 Web 服务...")
	ctxTimeout, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = tcpServer.Shutdown(ctxTimeout)
	if unixServer != nil {
		_ = unixServer.Shutdown(ctxTimeout)
	}
}
```

---

### 3.2 网关上下文与前缀剥离中间件

新建文件：`internal/app/gateway_middleware.go`

```go
package app

import (
	"net"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	CtxKeyViaGateway = "viaGateway"
	CtxKeyTrimUser   = "trimUser"
	CtxKeyIsAdmin    = "isAdmin"
)

// gatewayContextMiddleware 处理网关前缀剔除和安全上下文注入
func gatewayContextMiddleware(prefix string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 判断是否来自 Unix Domain Socket 连接
		isUnixSocket := false
		if _, ok := c.Request.Context().Value(http.LocalAddrContextKey).(*net.UnixAddr); ok {
			isUnixSocket = true
		} else if strings.HasPrefix(c.Request.RemoteAddr, "@") || c.Request.RemoteAddr == "" {
			isUnixSocket = true
		}

		if isUnixSocket {
			c.Set(CtxKeyViaGateway, true)
			// 信任网关注入的用户身份
			trimUser := c.GetHeader("X-Trim-Username")
			isAdmin := c.GetHeader("X-Trim-Isadmin") == "true"
			c.Set(CtxKeyTrimUser, trimUser)
			c.Set(CtxKeyIsAdmin, isAdmin)

			// 自动抹除 /app/camkeep 前缀，复用现有的路由树
			if strings.HasPrefix(c.Request.URL.Path, prefix) {
				c.Request.URL.Path = strings.TrimPrefix(c.Request.URL.Path, prefix)
				if c.Request.URL.Path == "" {
					c.Request.URL.Path = "/"
				}
			}
		} else {
			// 来自普通 TCP (:9110) 端口：严防伪造，清除相关字段
			c.Set(CtxKeyViaGateway, false)
			c.Set(CtxKeyIsAdmin, false)
			c.Request.Header.Del("X-Trim-Username")
			c.Request.Header.Del("X-Trim-Userid")
			c.Request.Header.Del("X-Trim-Isadmin")
		}

		c.Next()
	}
}
```

---

### 3.3 鉴权与单点登录（SSO）双模融合

修改文件：[`internal/app/auth.go`](internal/app/auth.go)

让 `authRequired` 支持网关信任分支：

```go
func authRequired(wa *webAuthContext) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 分支 1：如果是来自网关的可信连接，直接放行，免密登录
		if viaGateway, _ := c.Get(CtxKeyViaGateway); viaGateway == true {
			c.Next()
			return
		}

		// 分支 2：外部 TCP 端口访问，继续沿用现有 Cookie 检查
		if !wa.isEnabled() {
			c.Next()
			return
		}

		session, err := c.Cookie(cookieSessionName)
		if err != nil || session == "" {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}

		user := wa.UserStore.ValidateSession(session)
		if user == nil {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}

		c.Set("currentUser", user.Username)
		c.Set("currentUserRole", user.Role)
		c.Next()
	}
}
```

---

### 3.4 前端资源与嵌套 iframe 路径适配

#### A. 核心改动：嵌套 iframe 播放器源路径修正
修改文件：[`static/js/index.js` 第 3479 行](static/js/index.js#L3479)
```javascript
// 修改前：
liveIframe.src = `/stream.html?src=${encodeURIComponent(source)}`;

// 修改后（移除开头斜杠，采用当前页面上下文的相对路径）：
liveIframe.src = `stream.html?src=${encodeURIComponent(source)}`;
```
* **效果**：无论在根路径 `http://ip:9110/` 还是飞牛网关子路径 `https://nas/app/camkeep/` 下，都能正确加载到 go2rtc 播放页面。

#### B. 动态 BasePath 注入与全站静态资源相对化
在 HTML 模板顶部注入基础路径探测变量：
修改文件：[`template/index.html`](template/index.html) 的 `<head>` 部分：
```html
<head>
  <meta charset="utf-8">
  <script>
    // 自动探测是否处于飞牛网关前缀环境下
    window.__BASE_PATH__ = window.location.pathname.startsWith('/app/camkeep') ? '/app/camkeep' : '';
  </script>
  <!-- 将静态资源引用的绝对根路径改为相对路径 -->
  <link rel="stylesheet" href="static/css/index.css">
  <script src="static/js/index.js"></script>
</head>
```

#### C. API Fetch 路径标准化
在 [`static/js/index.js`](static/js/index.js) 开头定义统一的请求辅助函数：
```javascript
function apiUrl(path) {
    const base = window.__BASE_PATH__ || '';
    return base + (path.startsWith('/') ? path : '/' + path);
}

// 统一替换 fetch('/api/status') => fetch(apiUrl('/api/status'))
```

---

### 3.5 飞牛应用套件配置更新

#### A. 桌面与网关配置：[`fnnas.camkeep/app/ui/config`](fnnas.camkeep/app/ui/config)
```json
{
  ".url": {
    "camkeep.Application": {
      "title": "CamKeep",
      "icon": "images/icon_{0}.png",
      "type": "iframe",
      "protocol": "",
      "gatewayPrefix": "/app/camkeep",
      "gatewaySocket": "app.sock",
      "url": "/app/camkeep",
      "allUsers": false
    }
  }
}
```

#### B. 容器挂载与环境变量：[`fnnas.camkeep/app/docker/docker-compose.yaml`](fnnas.camkeep/app/docker/docker-compose.yaml)
在现有 Docker 方案中，将宿主机安装目录映射进容器，供其写入 socket 文件：
```yaml
version: "3.8"

services:
  camkeep:
    image: ghcr.io/r0n9/camkeep:latest
    container_name: camkeep
    restart: unless-stopped
    network_mode: "host"
    shm_size: "512m"
    environment:
      - TZ=Asia/Shanghai
      - CAMKEEP_AUTH_PASSWORD=admin
      - CAMKEEP_SOCK=/app/run/app.sock
    volumes:
      - "${wizard_config_dir:-/vol1/1000/camkeep/config}:/app/config"
      - "${wizard_records_dir:-/vol1/1000/camkeep/records}:/app/records"
      # 将 fnOS 分配给本应用的宿主机运行目录挂入容器
      - "${TRIM_APPDEST}:/app/run"
```

---

## 4. 实施阶段计划（Milestones & Timeline）

| 阶段 | 重点任务 | 涉及主要文件 | 预计耗时 |
| :--- | :--- | :--- | :--- |
| **阶段 1：后端双监听与网关中间件** | 1. 实现 TCP + Unix Socket 双监听器<br>2. 编写 `gateway_middleware.go`，处理前缀抹除与安全头隔离<br>3. 适配 `authRequired` 网关免密分支 | `internal/app/web_server.go`<br>`internal/app/gateway_middleware.go`<br>`internal/app/auth.go` | 0.5 天 |
| **阶段 2：前端相对路径改造** | 1. 修正 `liveIframe.src = 'stream.html...'`（解决嵌套 iframe 核心阻断）<br>2. 模板 `<head>` 注入 `window.__BASE_PATH__`<br>3. 修正静态资源引用为相对路径，封装 `apiUrl()` | `static/js/index.js`<br>`template/index.html`<br>`template/login.html` | 0.5 天 |
| **阶段 3：套件打包与本地验证** | 1. 更新 `fnnas.camkeep` 的 `ui/config` 和 `docker-compose.yaml`<br>2. 本地模拟 Unix Socket 发送 HTTP 请求测试（curl `--unix-socket`）<br>3. 验证原 9110 端口常规访问不受影响 | `fnnas.camkeep/app/ui/config`<br>`fnnas.camkeep/app/docker/*` | 0.5 天 |
| **阶段 4：fnOS 实机联调与穿透验收** | 1. 在飞牛 NAS 上安装最新 fpk 套件<br>2. 验证 Web 桌面多任务窗口内免密加载<br>3. 验证 fnConnect 外网实时直播（MSE/WebRTC）与录像回放 | fnOS 实机测试环境 | 0.5 天 |

---

## 5. 验收测试用例矩阵

| 序号 | 测试场景 | 预期结果 | 状态 |
| :---: | :--- | :--- | :---: |
| 1 | **独立端口访问** (`http://<IP>:9110`) | 正常重定向到 `/login`，输入账号密码后进入后台，功能如常 | 待测试 |
| 2 | **TCP 伪造 Header 测试** | 外部通过 `curl -H "X-Trim-Isadmin: true" :9110` 访问，被拦截拒绝并跳至登录页 | 待测试 |
| 3 | **Unix Socket 本地连通性** | 通过 `curl --unix-socket app.sock http://localhost/api/status` 能正常返回状态 JSON | 待测试 |
| 4 | **飞牛 Web 桌面窗口体验** | 点击桌面 CamKeep 图标，在系统内部直接弹窗打开，**免输密码直接进入控制台** | 待测试 |
| 5 | **多宫格实时直播（嵌套 iframe）** | 1/4/9 宫格的 `stream.html` 正常出流，无 404 报错，双击能正常进入物理全屏 | 待测试 |
| 6 | **历史录像回放** | 24 小时时间轴能正常拖动，DPlayer 播放器切片流加载正常 | 待测试 |
| 7 | **fnConnect 外网穿透访问** | 断开局域网 Wi-Fi，外网访问飞牛，通过统一网关能顺畅查看实时监控与回放 | 待测试 |

---

## 6. 后续演进路线（可选优化）

1. **去掉内层 iframe（Web Component 直出）**：
   未来可将单元格内的 `<iframe>` 替换为 go2rtc 提供的 `<video-stream>` 原生自定义标签，页面完全扁平化，渲染速度更快，消除嵌套 iframe 结构；
2. **纯原生 Go 二进制 FPK**：
   随着功能稳定，可探索剥离 Docker，将 CamKeep 编译为静态二进制配合系统自带的 FFmpeg 直接运行，安装包仅数十 MB，内存消耗可缩减至 100MB 以内。
