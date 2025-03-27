package apisix_registration

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// HealthHandler 定义健康检查处理程序接口
type HealthHandler interface {
	// RegisterHealthCheck 向HTTP服务注册健康检查路由
	RegisterHealthCheck(path string, handler http.HandlerFunc) error
}

// 默认健康检查响应
func defaultHealthResponse(serviceName string) []byte {
	responseJSON := fmt.Sprintf(`{"status":"ok","service":"%s","time":"%s"}`,
		serviceName,
		time.Now().Format(time.RFC3339))
	return []byte(responseJSON)
}

// StandardHealthHandler 标准http.Handler适配器
type StandardHealthHandler struct {
	Server *http.Server
}

// RegisterHealthCheck 实现HealthHandler接口
func (h *StandardHealthHandler) RegisterHealthCheck(path string, handler http.HandlerFunc) error {
	if h.Server == nil {
		return fmt.Errorf("HTTP服务器为空")
	}

	existingHandler := h.Server.Handler
	if existingHandler == nil {
		return fmt.Errorf("服务器处理器为空")
	}

	// 创建一个包装处理器
	h.Server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 匹配健康检查路径
		if r.URL.Path == path {
			handler(w, r)
			return
		}
		// 其他请求交由原有处理器处理
		existingHandler.ServeHTTP(w, r)
	})

	return nil
}

// GinHealthHandler gin框架适配器
type GinHealthHandler struct {
	Engine *gin.Engine
}

// RegisterHealthCheck 实现HealthHandler接口
func (h *GinHealthHandler) RegisterHealthCheck(path string, handler http.HandlerFunc) error {
	if h.Engine == nil {
		return fmt.Errorf("Gin引擎为空")
	}

	// 为Gin添加健康检查路由
	h.Engine.GET(path, func(c *gin.Context) {
		resp := defaultHealthResponse(c.GetHeader("X-Service-Name"))
		c.Data(http.StatusOK, "application/json", resp)
	})

	return nil
}

// GoZeroHealthHandler go-zero框架的适配器
type GoZeroHealthHandler struct {
	// 可以持有go-zero服务器引用
	// 这里使用通用接口，用户需要实现这个接口
	RegisterRoute func(path string, handler http.HandlerFunc) error
}

// RegisterHealthCheck 实现HealthHandler接口
func (h *GoZeroHealthHandler) RegisterHealthCheck(path string, handler http.HandlerFunc) error {
	if h.RegisterRoute == nil {
		return fmt.Errorf("注册路由函数为空")
	}
	return h.RegisterRoute(path, handler)
}

// healthService 提供健康检查服务
type healthService struct {
	serviceName   string
	port          int
	server        *http.Server
	customHandler HealthHandler
	healthPath    string
	logger        *zap.Logger
}

// newHealthService 创建健康检查服务
func newHealthService(serviceName string, port int, logger *zap.Logger) *healthService {
	return &healthService{
		serviceName: serviceName,
		port:        port,
		healthPath:  "/health", // 默认健康检查路径
		logger:      logger,
	}
}

// setCustomHandler 设置自定义健康检查处理器
func (h *healthService) setCustomHandler(handler HealthHandler, healthPath string) {
	h.customHandler = handler
	if healthPath != "" {
		h.healthPath = healthPath
	}
	h.logger.Info("已设置自定义健康检查处理器",
		zap.String("health_path", h.healthPath))
}

// setCustomServer 设置自定义HTTP服务器（兼容旧版本）
func (h *healthService) setCustomServer(server *http.Server, healthPath string) {
	if server != nil {
		h.customHandler = &StandardHealthHandler{Server: server}
		if healthPath != "" {
			h.healthPath = healthPath
		}
		h.logger.Info("已设置自定义HTTP服务器",
			zap.String("health_path", h.healthPath))
	} else {
		h.logger.Warn("提供的HTTP服务器为空")
	}
}

// 健康检查处理函数
func (h *healthService) healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(defaultHealthResponse(h.serviceName))
}

// start 启动健康检查服务
func (h *healthService) start() error {
	// 如果有自定义处理器，使用它来注册健康检查
	if h.customHandler != nil {
		return h.registerHealthCheck()
	}

	// 否则创建新的服务器
	return h.startNewServer()
}

// registerHealthCheck 向自定义处理器注册健康检查
func (h *healthService) registerHealthCheck() error {
	err := h.customHandler.RegisterHealthCheck(h.healthPath, h.healthCheckHandler)
	if err != nil {
		return fmt.Errorf("注册健康检查路由失败: %w", err)
	}

	h.logger.Info("已向自定义服务器添加健康检查路由",
		zap.String("service", h.serviceName),
		zap.String("health_path", h.healthPath))

	return nil
}

// startNewServer 启动新的健康检查服务器
func (h *healthService) startNewServer() error {
	router := gin.Default()

	// 添加健康检查路由
	router.GET(h.healthPath, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"service": h.serviceName,
			"time":    time.Now().Format(time.RFC3339),
		})
	})

	h.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", h.port),
		Handler: router,
	}

	// 启动HTTP服务器
	go func() {
		if err := h.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			h.logger.Error("健康检查服务启动失败", zap.Error(err))
		}
	}()

	h.logger.Info("健康检查服务已启动",
		zap.String("service", h.serviceName),
		zap.Int("port", h.port),
		zap.String("health_path", h.healthPath),
	)

	return nil
}

// shutdown 关闭健康检查服务
func (h *healthService) shutdown(ctx context.Context) error {
	// 如果使用的是自定义处理器，不需要关闭（由外部管理）
	if h.customHandler != nil {
		h.logger.Info("自定义健康检查服务由外部管理，跳过关闭")
		return nil
	}

	// 关闭内部创建的服务器
	if h.server == nil {
		return nil
	}

	if err := h.server.Shutdown(ctx); err != nil {
		h.logger.Error("关闭健康检查服务失败", zap.Error(err))
		return err
	}

	h.logger.Info("健康检查服务已关闭")
	return nil
}
