package main

import (
	"cornerstone/api"
	"cornerstone/config"
	"cornerstone/storage"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func main() {
	// 命令行参数
	port := flag.Int("port", 1205, "服务端口")
	dataDir := flag.String("data", "", "数据存储目录")
	flag.Parse()

	// 确定数据目录
	baseDir := *dataDir
	if baseDir == "" {
		// 获取可执行文件所在目录
		exePath, err := os.Executable()
		if err != nil {
			log.Fatal("无法获取程序路径:", err)
		}
		exeDir := filepath.Dir(exePath)
		baseDir = filepath.Join(exeDir, "src")
	}
	os.MkdirAll(baseDir, 0755)

	// 文件路径
	configPath := filepath.Join(baseDir, "config.json")
	promptsDir := filepath.Join(baseDir, "prompts")
	chatsDir := filepath.Join(baseDir, "chats")
	userAboutDir := filepath.Join(baseDir, "user_about")
	cachePhotoDir := filepath.Join(baseDir, "cache_photo")

	// 初始化管理器
	configManager := config.NewManager(configPath)
	promptManager := storage.NewPromptManager(promptsDir)
	chatManager := storage.NewChatManager(chatsDir)
	userManager := storage.NewUserManager(userAboutDir)
	os.MkdirAll(cachePhotoDir, 0755)

	log.Printf("数据存储目录: %s", baseDir)
	log.Printf("  配置文件: %s", configPath)
	log.Printf("  提示词目录: %s", promptsDir)
	log.Printf("  聊天记录目录: %s", chatsDir)
	log.Printf("  用户信息目录: %s", userAboutDir)
	log.Printf("  图片缓存目录: %s", cachePhotoDir)

	// 创建路由
	mux := http.NewServeMux()

	// 注册API处理器
	handler := api.NewHandler(configManager, promptManager, chatManager, userManager, cachePhotoDir)
	handler.RegisterRoutes(mux)

	// 启动服务
	addr := fmt.Sprintf(":%d", *port)
	log.Printf("AI客户端后端启动在 http://localhost%s", addr)
	log.Printf("API端点:")
	log.Printf("  POST   /api/chat                    - 发送聊天消息")
	log.Printf("  GET    /management/config           - 获取配置")
	log.Printf("  PUT    /management/config           - 更新配置")
	log.Printf("  GET    /management/providers        - 获取供应商列表")
	log.Printf("  POST   /management/providers        - 创建供应商")
	log.Printf("  GET    /management/providers/{id}   - 获取单个供应商")
	log.Printf("  PUT    /management/providers/{id}   - 更新供应商")
	log.Printf("  DELETE /management/providers/{id}   - 删除供应商")
	log.Printf("  GET    /management/providers/active - 获取激活供应商")
	log.Printf("  PUT    /management/providers/active - 设置激活供应商")
	log.Printf("  GET    /management/prompts          - 获取提示词列表")
	log.Printf("  POST   /management/prompts          - 创建提示词")
	log.Printf("  GET    /management/prompts/{id}     - 获取单个提示词")
	log.Printf("  PUT    /management/prompts/{id}     - 更新提示词")
	log.Printf("  DELETE /management/prompts/{id}     - 删除提示词")
	log.Printf("  GET    /management/prompts-avatar/{id} - 获取提示词头像")
	log.Printf("  POST   /management/prompts-avatar/{id} - 上传提示词头像")
	log.Printf("  DELETE /management/prompts-avatar/{id} - 删除提示词头像")
	log.Printf("  GET    /management/sessions         - 获取会话列表")
	log.Printf("  POST   /management/sessions         - 创建会话")
	log.Printf("  GET    /management/sessions/{id}    - 获取会话详情(含聊天记录)")
	log.Printf("  PUT    /management/sessions/{id}    - 更新会话标题")
	log.Printf("  DELETE /management/sessions/{id}    - 删除会话")
	log.Printf("  GET    /management/user             - 获取用户信息")
	log.Printf("  PUT    /management/user             - 更新用户信息")
	log.Printf("  GET    /management/user/avatar      - 获取用户头像")
	log.Printf("  POST   /management/user/avatar      - 上传用户头像")
	log.Printf("  DELETE /management/user/avatar      - 删除用户头像")
	log.Printf("  POST   /management/cache-photo      - 上传聊天图片")
	log.Printf("  GET    /management/cache-photo/{name} - 获取聊天图片")
	log.Printf("  GET    /management/health           - 健康检查")

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20, // 1MB
	}
	if err := server.ListenAndServe(); err != nil {
		log.Fatal("服务启动失败:", err)
	}
}
