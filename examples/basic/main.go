package main

import (
	apisix "github.com/linabellbiu/apisix-registration"
	"log"
)

func main() {
	// 创建服务配置
	cfg := apisix.Config{
		Name: "example-service", // 服务名称（必填）
		Port: 8080,              // 服务端口（必填）
		Host: "localhost",       // 服务主机名（必填）
		Upstream: apisix.Upstream{
			Id: "custom-example-upstream", // 自定义上游ID（可选，不指定则自动生成）
		},
		AdminApi: "",
		ApiKey:   "",
		HealthCfg: apisix.HealthCheckConfig{
			Enabled: true,      // 启用健康检查
			Path:    "/health", // 健康检查路由
		},
	}

	// 创建服务实例
	service, err := apisix.New(cfg)
	if err != nil {
		log.Fatalf("创建服务失败: %v", err)
	}

	log.Println("服务已创建，准备注册到APISIX...")

	// 启动服务并处理优雅关闭
	if err := service.Start(); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
