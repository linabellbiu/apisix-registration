package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	apisix "github.com/linabellbiu/apisix-registration"
)

func main() {
	// 创建自定义HTTP服务器
	router := gin.Default()

	// 添加一些自定义路由
	router.GET("/api/hello", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "Hello from custom server",
			"time":    time.Now().Format(time.RFC3339),
		})
	})

	// 创建HTTP服务器
	httpServer := &http.Server{
		Addr:    ":8888",
		Handler: router,
	}

	// 创建服务配置 - 高级示例
	cfg := apisix.Config{
		// 基本配置（必填项）
		Name: "advanced-service",
		Host: "localhost",
		Port: 8888,

		// 自定义上游ID - 便于与现有系统集成
		Upstream: apisix.Upstream{
			Id: "advanced-custom-upstream",
		},

		// API配置
		Path:     "/api/v1",
		AdminAPI: "http://apisix-admin:9180/apisix/admin",
		APIKey:   os.Getenv("APISIX_API_KEY"),

		// 健康检查配置
		HealthCfg: apisix.HealthCheckConfig{
			Enabled:       true,          // 启用健康检查
			Timeout:       5,             // 自定义超时时间
			MaxFails:      2,             // 自定义最大失败次数
			Method:        "GET",         // HTTP方法
			Path:          "/api/health", // 自定义健康检查路由
			HealthyCode:   200,           // 健康状态码
			UnhealthyCode: 503,           // 自定义不健康状态码
		},

		// 使用自定义HTTP服务器
		HTTPServer: httpServer,
	}

	// 创建服务实例
	service, err := apisix.New(cfg)
	if err != nil {
		log.Fatalf("创建服务失败: %v", err)
	}

	// 本示例展示如何手动控制各个步骤，并集成自定义HTTP服务器

	// 1. 手动注册到APISIX
	log.Println("手动注册服务到APISIX...")
	if err := service.Register(cfg.AdminAPI, cfg.APIKey); err != nil {
		log.Fatalf("注册服务失败: %v", err)
	}

	// 2. 手动启动健康检查 - 将集成到自定义服务器
	log.Println("启动健康检查服务...")
	if err := service.StartHealthCheck(); err != nil {
		// 如果健康检查启动失败，注销服务
		_ = service.Deregister(cfg.AdminAPI, cfg.APIKey)
		log.Fatalf("启动健康检查失败: %v", err)
	}

	// 3. 启动HTTP服务器（在后台）
	go func() {
		log.Printf("启动HTTP服务器在 http://localhost:%s ...", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP服务器启动失败: %v", err)
		}
	}()

	// 这里可以添加其他业务逻辑...
	log.Println("服务已完全启动")
	fmt.Println("您可以访问以下端点测试服务:")
	fmt.Println("- 自定义API: http://localhost:8888/api/hello")
	fmt.Println("- 健康检查: http://localhost:8888/api/health")
	fmt.Println("\n按Ctrl+C终止服务...")

	// 4. 等待终止信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	// 5. 手动注销和关闭
	log.Println("收到终止信号，开始关闭...")
	if err := service.Deregister(cfg.AdminAPI, cfg.APIKey); err != nil {
		log.Printf("注销服务失败: %v", err)
	}

	// 注：也可以使用以下异步方法自动处理整个生命周期：
	// if err := service.StartWithGracefulShutdown(cfg.AdminAPI, cfg.APIKey); err != nil {
	//    log.Fatalf("服务启动失败: %v", err)
	// }
	// // StartWithGracefulShutdown是异步的，可以继续执行其他逻辑
	// // ...其他业务逻辑...
}
