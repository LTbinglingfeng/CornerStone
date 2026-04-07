package main

import (
	"context"
	"cornerstone/api"
	"cornerstone/config"
	"cornerstone/exacttime"
	"cornerstone/logging"
	"cornerstone/storage"
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

var version = "dev"

func fileExists(path string) bool {
	info, errStat := os.Stat(path)
	return errStat == nil && !info.IsDir()
}

func resolveVersion(exeDir string) string {
	resolved := strings.TrimSpace(version)
	if resolved != "" && resolved != "dev" {
		return resolved
	}

	candidates := []string{}
	if cwd, errGetwd := os.Getwd(); errGetwd == nil {
		candidates = append(candidates, cwd)
	}
	if exeDir != "" {
		candidates = append(candidates, exeDir)
	}

	for _, dir := range candidates {
		cmd := exec.Command("git", "describe", "--tags", "--exact-match")
		cmd.Dir = dir
		output, errRun := cmd.Output()
		if errRun == nil {
			if tag := strings.TrimSpace(string(output)); tag != "" {
				return tag
			}
		}
	}

	if resolved == "" {
		return "dev"
	}
	return resolved
}

func printStartupBanner(appVersion string) {
	const (
		cyan  = "\033[36m"
		bold  = "\033[1m"
		dim   = "\033[2m"
		reset = "\033[0m"
	)

	fmt.Println()
	fmt.Print(bold + cyan)
	fmt.Println(`    ____                          ____  _                   `)
	fmt.Println(`   / ___|___  _ __ _ __   ___ _ _/ ___|| |_ ___  _ __   ___ `)
	fmt.Println(`  | |   / _ \| '__| '_ \ / _ \ '_\___ \| __/ _ \| '_ \ / _ \`)
	fmt.Println(`  | |__| (_) | |  | | | |  __/ |  ___) | || (_) | | | |  __/`)
	fmt.Println(`   \____\___/|_|  |_| |_|\___|_| |____/ \__\___/|_| |_|\___|`)
	fmt.Print(reset)
	fmt.Println()
	fmt.Printf("    %sv%s%s\n", dim, appVersion, reset)
	fmt.Println()
}

func printStartupSummary(scheme string, port int, distDir, baseDir, logPath string, tlsEnabled bool, tlsSource string) {
	const (
		cyan   = "\033[36m"
		green  = "\033[32m"
		yellow = "\033[33m"
		bold   = "\033[1m"
		dim    = "\033[2m"
		reset  = "\033[0m"
	)

	fmt.Printf("    %s▸%s Listen  %s%s://localhost:%d%s\n", cyan, reset, bold, scheme, port, reset)
	if distDir != "" {
		fmt.Printf("    %s▸%s Web UI  %s%s://localhost:%d/%s\n", cyan, reset, bold, scheme, port, reset)
		fmt.Printf("    %s▸%s Assets  %s\n", cyan, reset, distDir)
	} else {
		fmt.Printf("    %s▸%s Web UI  %snot available%s (run: cd web && npm run build)\n", yellow, reset, dim, reset)
	}
	fmt.Printf("    %s▸%s Data    %s\n", cyan, reset, baseDir)
	fmt.Printf("    %s▸%s Logs    %s\n", cyan, reset, logPath)
	if tlsEnabled {
		fmt.Printf("    %s▸%s TLS     %senabled%s (%s)\n", green, reset, green, reset, tlsSource)
	} else {
		fmt.Printf("    %s▸%s TLS     %sdisabled%s\n", cyan, reset, dim, reset)
	}
	fmt.Println()
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
	webDir := flag.String("web", "", "前端构建目录(默认为 web/dist，通过HTTP/HTTPS访问)")
	tlsCertFlag := flag.String("tls-cert", "", "TLS证书路径(PEM)，设置后启用HTTPS（或在 config.json 配置 tls_cert_path/tls_key_path）")
	tlsKeyFlag := flag.String("tls-key", "", "TLS私钥路径(PEM)，设置后启用HTTPS（或在 config.json 配置 tls_cert_path/tls_key_path）")
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
	ttsAudioDir := filepath.Join(baseDir, "tts_audio")
	remindersDir := filepath.Join(baseDir, "reminders")
	idleGreetingsDir := filepath.Join(baseDir, "idle_greetings")

	// 初始化管理器
	configManager := config.NewManager(configPath)
	tlsCertPath := strings.TrimSpace(*tlsCertFlag)
	tlsKeyPath := strings.TrimSpace(*tlsKeyFlag)
	tlsSource := ""
	if tlsCertPath != "" || tlsKeyPath != "" {
		tlsSource = "命令行参数"
	} else {
		cfg := configManager.Get()
		tlsCertPath = strings.TrimSpace(cfg.TLSCertPath)
		tlsKeyPath = strings.TrimSpace(cfg.TLSKeyPath)
		if tlsCertPath != "" || tlsKeyPath != "" {
			tlsSource = "config.json"
		}
	}
	resolveTLSPath := func(value string) string {
		if value == "" {
			return ""
		}
		if filepath.IsAbs(value) {
			return value
		}
		if fileExists(value) {
			return value
		}
		return filepath.Join(baseDir, value)
	}

	tlsEnabled := false
	if tlsCertPath != "" || tlsKeyPath != "" {
		if tlsCertPath == "" || tlsKeyPath == "" {
			logging.Fatalf("启用TLS需同时指定证书与私钥（来源：%s）", tlsSource)
		}
		tlsCertPath = resolveTLSPath(tlsCertPath)
		tlsKeyPath = resolveTLSPath(tlsKeyPath)
		if !fileExists(tlsCertPath) {
			logging.Fatalf("TLS证书文件不存在: %s（来源：%s）", tlsCertPath, tlsSource)
		}
		if !fileExists(tlsKeyPath) {
			logging.Fatalf("TLS私钥文件不存在: %s（来源：%s）", tlsKeyPath, tlsSource)
		}
		tlsEnabled = true
	}
	scheme := "http"
	if tlsEnabled {
		scheme = "https"
	}

	promptManager := storage.NewPromptManager(promptsDir)
	chatManager := storage.NewChatManager(chatsDir)
	userManager := storage.NewUserManager(userAboutDir)
	authManager := storage.NewAuthManager(userAboutDir)
	memoryManager := storage.NewMemoryManager(promptsDir)
	reminderManager := storage.NewReminderManager(remindersDir)
	idleGreetingManager := storage.NewIdleGreetingManager(idleGreetingsDir)
	exactTimeService := exacttime.New(exacttime.DefaultConfig())
	memoryExtractor := storage.NewMemoryExtractor(
		memoryManager,
		configManager,
		chatManager,
		userManager,
		filepath.Join(baseDir, "memory_extraction_prompt.txt"),
		exactTimeService,
	)
	os.MkdirAll(cachePhotoDir, 0755)
	os.MkdirAll(ttsAudioDir, 0755)

	// 创建路由
	mux := http.NewServeMux()

	// 注册API处理器
	handler := api.NewHandler(configManager, promptManager, chatManager, userManager, authManager, cachePhotoDir, ttsAudioDir, memoryManager, memoryExtractor)
	exactTimeCtx, exactTimeCancel := context.WithCancel(context.Background())
	defer exactTimeCancel()
	go exactTimeService.Run(exactTimeCtx)
	handler.SetExactTimeService(exactTimeService)
	reminderService := api.NewReminderService(handler, reminderManager, exactTimeService)
	handler.SetReminderService(reminderService)
	reminderCtx, reminderCancel := context.WithCancel(context.Background())
	defer reminderCancel()
	reminderService.Start(reminderCtx)
	idleGreetingService := api.NewIdleGreetingService(handler, idleGreetingManager, exactTimeService)
	handler.SetIdleGreetingService(idleGreetingService)
	idleGreetingCtx, idleGreetingCancel := context.WithCancel(context.Background())
	defer idleGreetingCancel()
	idleGreetingService.Start(idleGreetingCtx)
	appVersion := resolveVersion(exeDir)
	clawBotService := api.NewClawBotService(handler)
	handler.SetClawBotService(clawBotService)
	defer clawBotService.Close()
	napCatService := api.NewNapCatService(handler)
	handler.SetNapCatService(napCatService)
	defer napCatService.Close()
	handler.RegisterRoutes(mux)

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
	}

	// 启动服务
	addr := fmt.Sprintf(":%d", *port)
	printStartupBanner(appVersion)
	printStartupSummary(scheme, *port, distDir, baseDir, logPath, tlsEnabled, tlsSource)

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20, // 1MB
	}
	var errServe error
	if tlsEnabled {
		server.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
		errServe = server.ListenAndServeTLS(tlsCertPath, tlsKeyPath)
	} else {
		errServe = server.ListenAndServe()
	}
	if errServe != nil {
		logging.Fatalf("服务启动失败: %v", errServe)
	}
}
