package main

import (
	"cornerstone/api"
	"cornerstone/config"
	"cornerstone/logging"
	"cornerstone/storage"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

func fileExists(path string) bool {
	info, errStat := os.Stat(path)
	return errStat == nil && !info.IsDir()
}

func registerFrontend(mux *http.ServeMux, distDir string) {
	fileSystem := http.Dir(distDir)
	fileServer := http.FileServer(fileSystem)
	indexPath := filepath.Join(distDir, "index.html")

	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}

		cleanedPath := path.Clean(r.URL.Path)
		if cleanedPath == "/" {
			http.ServeFile(w, r, indexPath)
			return
		}

		relativePath := strings.TrimPrefix(cleanedPath, "/")
		file, errOpen := fileSystem.Open(relativePath)
		if errOpen == nil {
			defer func() {
				if errClose := file.Close(); errClose != nil {
					logging.Errorf("关闭前端静态文件失败: %v", errClose)
				}
			}()

			if info, errInfo := file.Stat(); errInfo == nil && !info.IsDir() {
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		if path.Ext(cleanedPath) != "" {
			http.NotFound(w, r)
			return
		}

		http.ServeFile(w, r, indexPath)
	}))
}

func main() {
	// 命令行参数
	port := flag.Int("port", 1205, "服务端口")
	dataDir := flag.String("data", "", "数据存储目录")
	webDir := flag.String("web", "", "前端构建目录(默认为 web/dist，需通过HTTP访问)")
	flag.Parse()

	// 获取可执行文件所在目录
	exePath, errExecutable := os.Executable()
	if errExecutable != nil {
		logging.Fatalf("无法获取程序路径: %v", errExecutable)
	}
	exeDir := filepath.Dir(exePath)

	// 确定数据目录
	baseDir := *dataDir
	if baseDir == "" {
		baseDir = filepath.Join(exeDir, "src")
	}
	os.MkdirAll(baseDir, 0755)

	logPath := filepath.Join(baseDir, "cornerstone.log")
	logFile, errInitLog := logging.Init(logPath)
	if errInitLog != nil {
		logging.Fatalf("初始化日志失败: %v", errInitLog)
	}
	defer func() {
		if errClose := logFile.Close(); errClose != nil {
			logging.Errorf("关闭日志文件失败: %v", errClose)
		}
	}()

	// 文件路径
	configPath := filepath.Join(baseDir, "config.json")
	promptsDir := filepath.Join(baseDir, "prompts")
	chatsDir := filepath.Join(baseDir, "chats")
	userAboutDir := filepath.Join(baseDir, "user_about")
	cachePhotoDir := filepath.Join(baseDir, "cache_photo")
	momentsDir := filepath.Join(baseDir, "moments")

	// 初始化管理器
	configManager := config.NewManager(configPath)
	promptManager := storage.NewPromptManager(promptsDir)
	chatManager := storage.NewChatManager(chatsDir)
	userManager := storage.NewUserManager(userAboutDir)
	authManager := storage.NewAuthManager(userAboutDir)
	momentManager := storage.NewMomentManager(momentsDir)
	momentGenerator := api.NewMomentGenerator(momentManager, configManager)
	memoryManager := storage.NewMemoryManager(promptsDir)
	memoryExtractor := storage.NewMemoryExtractor(memoryManager, configManager, chatManager, userManager, filepath.Join(baseDir, "memory_extraction_prompt.txt"))
	os.MkdirAll(cachePhotoDir, 0755)

	logging.Infof("日志文件: %s", logPath)
	logging.Infof("数据存储目录: %s", baseDir)
	logging.Infof("  配置文件: %s", configPath)
	logging.Infof("  提示词目录: %s", promptsDir)
	logging.Infof("  聊天记录目录: %s", chatsDir)
	logging.Infof("  用户信息目录: %s", userAboutDir)
	logging.Infof("  图片缓存目录: %s", cachePhotoDir)
	logging.Infof("  朋友圈目录: %s", momentsDir)

	// 创建路由
	mux := http.NewServeMux()

	// 注册API处理器
	handler := api.NewHandler(configManager, promptManager, chatManager, userManager, authManager, cachePhotoDir, memoryManager, memoryExtractor, momentManager, momentGenerator)
	handler.RegisterRoutes(mux)

	// 朋友圈静态文件（图片、背景图）
	mux.Handle("/moments/images/", http.StripPrefix("/moments/images/", http.FileServer(http.Dir(filepath.Join(momentsDir, "images")))))
	mux.Handle("/moments/backgrounds/", http.StripPrefix("/moments/backgrounds/", http.FileServer(http.Dir(filepath.Join(momentsDir, "backgrounds")))))

	// 注册前端静态文件
	distDir := *webDir
	if distDir == "" {
		candidates := []string{
			filepath.Join(exeDir, "web", "dist"),
			filepath.Join(".", "web", "dist"),
		}
		for _, candidateDir := range candidates {
			if fileExists(filepath.Join(candidateDir, "index.html")) {
				distDir = candidateDir
				break
			}
		}
	}
	if distDir != "" && fileExists(filepath.Join(distDir, "index.html")) {
		registerFrontend(mux, distDir)
		logging.Infof("前端静态目录: %s", distDir)
		logging.Infof("前端页面: http://localhost:%d/", *port)
	} else {
		logging.Infof("未找到前端构建产物，请先执行: cd web && npm run build")
	}

	// 启动服务
	addr := fmt.Sprintf(":%d", *port)
	logging.Infof("AI客户端后端启动在 http://localhost%s", addr)
	logging.Infof("API端点:")
	for _, endpoint := range []struct {
		method      string
		path        string
		description string
	}{
		{method: "POST", path: "/api/chat", description: "发送聊天消息"},
		{method: "GET", path: "/management/auth/status", description: "获取鉴权状态"},
		{method: "POST", path: "/management/auth/setup", description: "初始化用户名和密码"},
		{method: "POST", path: "/management/auth/login", description: "登录获取令牌"},
		{method: "GET", path: "/management/config", description: "获取配置"},
		{method: "PUT", path: "/management/config", description: "更新配置"},
		{method: "GET", path: "/management/providers", description: "获取供应商列表"},
		{method: "POST", path: "/management/providers", description: "创建供应商"},
		{method: "GET", path: "/management/providers/{id}", description: "获取单个供应商"},
		{method: "PUT", path: "/management/providers/{id}", description: "更新供应商"},
		{method: "DELETE", path: "/management/providers/{id}", description: "删除供应商"},
		{method: "GET", path: "/management/providers/active", description: "获取激活供应商"},
		{method: "PUT", path: "/management/providers/active", description: "设置激活供应商"},
		{method: "GET", path: "/management/prompts", description: "获取提示词列表"},
		{method: "POST", path: "/management/prompts", description: "创建提示词"},
		{method: "GET", path: "/management/prompts/{id}", description: "获取单个提示词"},
		{method: "PUT", path: "/management/prompts/{id}", description: "更新提示词"},
		{method: "DELETE", path: "/management/prompts/{id}", description: "删除提示词"},
		{method: "GET", path: "/management/prompts-avatar/{id}", description: "获取提示词头像"},
		{method: "POST", path: "/management/prompts-avatar/{id}", description: "上传提示词头像"},
		{method: "DELETE", path: "/management/prompts-avatar/{id}", description: "删除提示词头像"},
		{method: "GET", path: "/management/sessions", description: "获取会话列表"},
		{method: "POST", path: "/management/sessions", description: "创建会话"},
		{method: "GET", path: "/management/sessions/{id}", description: "获取会话详情(含聊天记录)"},
		{method: "PUT", path: "/management/sessions/{id}", description: "更新会话标题"},
		{method: "DELETE", path: "/management/sessions/{id}", description: "删除会话"},
		{method: "GET", path: "/management/user", description: "获取用户信息"},
		{method: "PUT", path: "/management/user", description: "更新用户信息"},
		{method: "GET", path: "/management/user/avatar", description: "获取用户头像"},
		{method: "POST", path: "/management/user/avatar", description: "上传用户头像"},
		{method: "DELETE", path: "/management/user/avatar", description: "删除用户头像"},
		{method: "POST", path: "/management/cache-photo", description: "上传聊天图片"},
		{method: "GET", path: "/management/cache-photo/{name}", description: "获取聊天图片"},
		{method: "GET", path: "/management/health", description: "健康检查"},
	} {
		logging.Infof("  %-6s %-30s - %s", endpoint.method, endpoint.path, endpoint.description)
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20, // 1MB
	}
	if errServe := server.ListenAndServe(); errServe != nil {
		logging.Fatalf("服务启动失败: %v", errServe)
	}
}
