# APISIX 服务注册

这个Go语言包提供了一个简洁优雅的方式来将服务注册到Apache APISIX中，不需要额外的注册发现服务,同时包含健康检查。

## 安装

```bash
go get github.com/linabellbiu/apisix-registration
```

## 使用方法

以下是基本用法示例：

```go
package main

import (
	"log"
	"os"

	apisix "github.com/linabellbiu/apisix-registration"
)

func main() {
	// 创建服务配置
	cfg := apisix.Config{
		Name:     "your-service",
		Host:     "localhost",           // 服务主机名
		Port:     8080,                  // 服务端口
		Upstream: apisix.Upstream{
			Id: "custom-upstream-id",    // 自定义上游ID（可选）
		},
		AdminApi: "http://apisix-admin:9180/apisix/admin",  // APISIX Admin API 地址
		ApiKey:   os.Getenv("APISIX_API_KEY"),              // APISIX Admin API 密钥
		HealthCfg: apisix.HealthCheckConfig{
			Enabled:       true,         // 启用健康检查
            Path:         "/health",    // 健康检查路由
		},
	}

	// 创建服务实例
	service, err := apisix.New(cfg)
	if err != nil {
		log.Fatalf("创建服务失败: %v", err)
	}

	// 启动服务并处理优雅关闭
	if err := service.Start(); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
```

## 上游管理

当服务注册到APISIX时，包会执行以下操作：

1. 检查上游ID是否存在
   - 如果不存在，创建新的上游
   - 如果已存在，将当前服务节点添加到现有上游

2. 当服务注销时：
   - 只删除特定的服务节点，而非整个上游

这种设计允许同一上游ID注册多个不同节点，从而支持负载均衡和高可用性。

## 关于健康检查

健康检查功能只有在配置中设置 `HealthCfg.Enabled: true` 时才会启用。启用健康检查时，包会：

1. 在服务的端口上提供一个健康检查路由（默认为 `/health`）
2. 返回包含服务状态的JSON响应

### 使用自定义HTTP服务器

您可以选择将健康检查集成到您已有的HTTP服务器中，而不是让包创建新的服务器。有多种方式可以实现这一点：

### 1. 使用标准HTTP服务器

```go
// 创建自己的HTTP服务器
myServer := &http.Server{
    Addr:    ":8080",
    Handler: myHandler,
}

// 在配置中传入
cfg := apisix.Config{
    // ...其他配置...
    HealthCfg: apisix.HealthCheckConfig{
        Enabled: true,
        Route:   "/api/health",  // 自定义健康检查路径
    },
}

service, err := apisix.New(cfg,apisix.OptionsWithHttpServer(myServer))
```

### 2. 使用自定义健康检查处理器（任意HTTP框架）

我们提供了通用接口`HealthHandler`，可以适配任何HTTP框架：

```go
// 定义一个自定义的健康检查处理器
customHandler := &apisix.HealthHandler{
    RegisterHealthCheck: func(path string, handler http.HandlerFunc) error {
        // 在您的框架中注册健康检查路由
        // 例如: router.GET(path, handler)
        return nil
    },
}

// 在配置中传入
cfg := apisix.Config{
    // ...其他配置...
    HealthHandler: customHandler,
}
```

### 3. 集成Gin框架

内置支持Gin框架：

```go
// 创建Gin引擎
engine := gin.Default()

// 创建Gin适配器
ginHandler := &apisix.GinHealthHandler{
    Engine: engine,
}

// 在配置中传入
cfg := apisix.Config{
    // ...其他配置...
}

service, err := apisix.New(cfg,apisix.OptionsWithHealthHandler(engine))

```

### 4. 集成go-zero框架

内置支持go-zero框架：

```go
// 创建go-zero服务器
server := rest.MustNewServer(c.RestConf)

// 创建go-zero适配器
goZeroHandler := &apisix.GoZeroHealthHandler{
    RegisterRoute: func(path string, handler http.HandlerFunc) error {
        // 注册健康检查路由
        server.AddRoute([]rest.Route{
            {
                Method:  http.MethodGet,
                Path:    path,
                Handler: handler,
            },
        })
        return nil
    },
}

// 在配置中传入
cfg := apisix.Config{
    // ...其他配置...
}

service, err := apisix.New(cfg,apisix.OptionsWithHealthHandler(goZeroHandler))

```

### 5. 通过方法设置（在创建服务实例后）

```go
// 创建服务实例
service, _ := apisix.New(cfg)

// 设置自定义健康检查处理器
service.SetHealthHandler(myHandler, "/custom/health")

// 或使用标准HTTP服务器（向后兼容）
service.SetHTTPServer(myServer, "/custom/health")

// 启动健康检查
service.StartHealthCheck()
```

使用自定义HTTP服务器时，包会在您的处理器逻辑中添加健康检查路由，而不会创建新的HTTP服务器。

## 手动注册和注销

如果您希望手动控制注册和注销过程：

```go
service, err := apisix.New(cfg)
// 注册服务（包含检查上游是否存在的逻辑）
err := service.Register()

// 启动健康检查
err := service.StartHealthCheck()

// 注销服务（只删除特定节点，不删除整个上游）
err := service.Deregister()
```

## 优雅关闭

服务会监听 SIGINT 和 SIGTERM 信号，当接收到这些信号时：

1. 从APISIX中注销服务（只删除特定节点）
2. 关闭健康检查服务
3. 完成所有清理工作

## 注意:
开启`apisix.HealthCheckConfig.Enabled=true`代码健康检查路由后,还要在apisix开启`主动检查`功能然后重启apisix服务,才能生效