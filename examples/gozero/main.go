package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	apisix "github.com/linabellbiu/apisix-registration"

	"github.com/zeromicro/go-zero/rest"
)

// 简化版的go-zero配置
type Config struct {
	RestConf rest.RestConf
}

func main() {
	var c Config
	c.RestConf = rest.RestConf{
		Host:    "0.0.0.0",
		Port:    8886,
		Verbose: false,
	}

	// 创建go-zero服务器
	server := rest.MustNewServer(c.RestConf)
	defer server.Stop()

	// 添加一些示例路由
	server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/a",
		Handler: helloHandler,
	})

	// 2. 创建go-zero健康检查处理器适配器
	goZeroHandler := &apisix.GoZeroHealthHandler{
		RegisterRoute: func(path string, handler http.HandlerFunc) error {
			// 在go-zero中注册健康检查路由
			server.AddRoute(rest.Route{
				Method:  http.MethodGet,
				Path:    path,
				Handler: handler,
			})
			log.Printf("已在go-zero中注册健康检查路由: %s", path)
			return nil
		},
	}

	// 3. 创建APISIX服务配置
	cfg := apisix.Config{
		Name: "gozero-service",
		Host: "192.168.3.71",
		Port: 8886,
		Path: "/api",
		Upstream: apisix.Upstream{
			Id: "gozero-upstream",
		},
		AdminAPI: "http://192.168.3.71:9180/apisix/admin",
		APIKey:   "edd1c9f034335f136f87ad84b625c8f1",
		HealthCfg: apisix.HealthCheckConfig{
			Enabled: true,
			Path:    "/api/health",
		},
		// 使用go-zero的健康检查处理器
		HealthHandler: goZeroHandler,
	}

	// 4. 创建APISIX服务实例
	service, err := apisix.New(cfg)
	if err != nil {
		log.Fatalf("创建APISIX服务实例失败: %v", err)
	}

	if err := service.StartWithGracefulShutdown(cfg.AdminAPI, cfg.APIKey); err != nil {
		log.Fatalf("注册到APISIX失败: %v", err)
	}

	fmt.Println("启动go-zero服务器...")
	fmt.Println("服务已完全启动")
	fmt.Println("您可以访问以下端点测试服务:")
	fmt.Println("- 示例API: http://localhost:8889/api/hello")
	fmt.Println("- 健康检查: http://localhost:8889/api/health")
	fmt.Println("\n按Ctrl+C终止服务...")

	server.Start()
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
