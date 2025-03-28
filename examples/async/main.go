package main

import (
	"fmt"
	"log"
	"os"
	"time"

	apisix "github.com/linabellbiu/apisix-registration"
)

func main() {
	// 创建服务配置
	cfg := apisix.Config{
		Name: "async-service",
		Host: "localhost",
		Port: 8080,
		Path: "/api",
		Upstream: apisix.Upstream{
			Id: "async-upstream",
		},
		AdminAPI: "http://apisix-admin:9180/apisix/admin",
		APIKey:   os.Getenv("APISIX_API_KEY"),
		HealthCfg: apisix.HealthCheckConfig{
			Enabled: true,
		},
	}

	// 创建服务实例
	service, err := apisix.New(cfg)
	if err != nil {
		log.Fatalf("创建服务失败: %v", err)
	}

	// 异步启动服务
	log.Println("异步启动服务...")
	if err := service.StartWithGracefulShutdown(cfg.AdminAPI, cfg.APIKey); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}

	// StartWithGracefulShutdown是异步的，所以这里会立即执行
	log.Println("服务已启动，现在可以继续执行其他业务逻辑")

	// 模拟一些其他业务逻辑
	for i := 1; i <= 10; i++ {
		log.Printf("执行业务逻辑... (%d/10)", i)
		time.Sleep(1 * time.Second)
	}

	// 主程序可以继续运行，服务会在收到终止信号后自动关闭
	log.Println("业务逻辑执行完毕，但服务仍在运行")
	log.Println("您可以按Ctrl+C终止程序，服务将自动注销并关闭")
	fmt.Println("健康检查端点: http://localhost:8080/health")

	// 阻止主程序退出
	select {}
}
