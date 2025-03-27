package main

import (
	"log"
	"os"

	apisix "github.com/linabellbiu/apisix-registration"
)

func main() {
	// 创建服务配置
	cfg := apisix.Config{
		Name: "example-service", // 服务名称（必填）
		Host: "localhost",       // 服务主机名（必填）
		Port: 8080,              // 服务端口（必填）
		Path: "/api",            // 服务路径（可选，默认为"/"）
		Upstream: apisix.Upstream{
			Id: "custom-example-upstream", // 自定义上游ID（可选，不指定则自动生成）
		},
		AdminAPI: "http://apisix-admin:9180/apisix/admin", // APISIX Admin API 地址（必填）
		APIKey:   os.Getenv("APISIX_API_KEY"),             // APISIX Admin API 密钥
		HealthCfg: apisix.HealthCheckConfig{
			Enabled:       true,      // 启用健康检查
			Timeout:       3,         // 健康检查超时时间(秒)
			MaxFails:      3,         // 最大失败次数
			Method:        "GET",     // HTTP方法
			Route:         "/health", // 健康检查路由
			HealthyCode:   200,       // 健康状态码
			UnhealthyCode: 500,       // 不健康状态码
		},
	}

	// 创建服务实例
	service, err := apisix.New(cfg)
	if err != nil {
		log.Fatalf("创建服务失败: %v", err)
	}

	log.Println("服务已创建，准备注册到APISIX...")

	// 启动服务并处理优雅关闭
	if err := service.StartWithGracefulShutdown(cfg.AdminAPI, cfg.APIKey); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
