package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	apisix "github.com/linabellbiu/apisix-registration"

	"github.com/zeromicro/go-zero/rest"
)

// 简化版的go-zero配置
type Config struct {
	RestConf rest.RestConf
}

func main() {
	// 1. 创建和配置go-zero服务器
	var c Config
	// 这里简化为直接配置，实际应用中通常通过文件加载
	c.RestConf = rest.RestConf{
		Host: "0.0.0.0",
		Port: 8889,
	}

	// 创建go-zero服务器
	server := rest.MustNewServer(c.RestConf)
	defer server.Stop()

	// 添加一些示例路由
	server.AddRoute([]rest.Route{
		{
			Method:  http.MethodGet,
			Path:    "/api/hello",
			Handler: helloHandler,
		},
	})

	// 2. 创建go-zero健康检查处理器适配器
	goZeroHandler := &apisix.GoZeroHealthHandler{
		RegisterRoute: func(path string, handler http.HandlerFunc) error {
			// 在go-zero中注册健康检查路由
			server.AddRoute([]rest.Route{
				{
					Method:  http.MethodGet,
					Path:    path,
					Handler: handler,
				},
			})
			log.Printf("已在go-zero中注册健康检查路由: %s", path)
			return nil
		},
	}

	// 3. 创建APISIX服务配置
	cfg := apisix.Config{
		Name: "gozero-service",
		Host: "localhost",
		Port: 8889,
		Path: "/api",
		Upstream: apisix.Upstream{
			Id: "gozero-upstream",
		},
		AdminAPI: "http://apisix-admin:9180/apisix/admin",
		APIKey:   os.Getenv("APISIX_API_KEY"),
		HealthCfg: apisix.HealthCheckConfig{
			Enabled: true,
			Route:   "/api/health",
		},
		// 使用go-zero的健康检查处理器
		HealthHandler: goZeroHandler,
	}

	// 4. 创建APISIX服务实例
	service, err := apisix.New(cfg)
	if err != nil {
		log.Fatalf("创建APISIX服务实例失败: %v", err)
	}

	// 5. 启动服务
	// 5.1 启动go-zero服务
	go func() {
		fmt.Println("启动go-zero服务器...")
		server.Start()
	}()

	// 5.2 注册到APISIX
	if err := service.Register(cfg.AdminAPI, cfg.APIKey); err != nil {
		log.Fatalf("注册到APISIX失败: %v", err)
	}

	// 5.3 启动健康检查（这会调用上面定义的RegisterRoute函数）
	if err := service.StartHealthCheck(); err != nil {
		_ = service.Deregister(cfg.AdminAPI, cfg.APIKey)
		log.Fatalf("启动健康检查失败: %v", err)
	}

	fmt.Println("服务已完全启动")
	fmt.Println("您可以访问以下端点测试服务:")
	fmt.Println("- 示例API: http://localhost:8889/api/hello")
	fmt.Println("- 健康检查: http://localhost:8889/api/health")
	fmt.Println("\n按Ctrl+C终止服务...")

	// 6. 等待终止信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	// 7. 优雅关闭
	fmt.Println("正在关闭服务...")

	// 从APISIX注销
	if err := service.Deregister(cfg.AdminAPI, cfg.APIKey); err != nil {
		log.Printf("从APISIX注销失败: %v", err)
	}

	// go-zero服务会在程序退出时通过defer自动关闭
}

// 示例处理函数
func helloHandler(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"message": "Hello from go-zero server!",
		"time":    time.Now().Format(time.RFC3339),
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"message":"%s","time":"%s"}`, response["message"], response["time"])
}
