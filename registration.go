package apisix_registration

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"go.uber.org/zap"
)

// 健康检查相关常量
const (
	DefaultHealthCheckTimeout       = 3
	DefaultHealthCheckMaxFails      = 3
	DefaultHealthCheckMethod        = "GET"
	DefaultHealthCheckRoute         = "/health"
	DefaultHealthCheckHealthyCode   = 200
	DefaultHealthCheckUnhealthyCode = 500
)

type Service struct {
	name        string
	host        string
	port        int
	path        string
	upstreamID  string
	routeID     string
	healthCheck bool
	interval    int

	apiClient *apisixClient
	healthSvc *healthService
	logger    *zap.Logger

	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
}

type Upstream struct {
	Id            string // Id 自定义上游ID，如果为空则自动生成
	UpstreamTypes string // UpstreamTypes 指定上游服务的类型。
}

// HealthCheckConfig 健康检查的配置
type HealthCheckConfig struct {
	Enabled       bool   // 是否启用健康检查
	Timeout       int    // 超时时间（秒）
	MaxFails      int    // 最大失败次数
	Method        string // HTTP 方法
	Route         string // 健康检查路由
	HealthyCode   int    // 健康状态码
	UnhealthyCode int    // 不健康状态码
}

// Config 是服务配置
type Config struct {
	Name      string // 服务名称
	Host      string // 服务主机名
	Port      int    // 服务端口
	Path      string // 服务路径
	Upstream  Upstream
	AdminAPI  string            // APISIX Admin API 地址
	APIKey    string            // APISIX Admin API 密钥
	HealthCfg HealthCheckConfig // 健康检查配置
	// 可以使用以下两种方式之一来集成自定义HTTP服务：

	// 1. 使用标准HTTP服务器
	HTTPServer *http.Server

	// 2. 使用自定义健康检查处理器（支持不同框架）
	HealthHandler HealthHandler
}

// New 创建一个新的服务实例
func New(cfg Config) (*Service, error) {
	logger, _ := zap.NewProduction()

	if cfg.Name == "" {
		return nil, fmt.Errorf("%w: 服务名称不能为空", ErrInvalidConfig)
	}

	if cfg.Host == "" {
		return nil, ErrEmptyHost
	}

	if cfg.Port <= 0 {
		return nil, ErrInvalidPort
	}

	// 设置默认值
	if cfg.Path == "" {
		cfg.Path = "/"
		logger.Info("未指定Path，使用默认值", zap.String("path", cfg.Path))
	}

	// 生成或使用上游ID
	upstreamID := cfg.Upstream.Id
	if upstreamID == "" {
		upstreamID = fmt.Sprintf("%s_%s_%d", cfg.Name, cfg.Host, cfg.Port)
		logger.Info("未指定上游ID，自动生成", zap.String("upstream_id", upstreamID))
	}

	routeID := fmt.Sprintf("%s_route", cfg.Name)

	// 处理健康检查配置
	healthCheck := cfg.HealthCfg.Enabled
	var interval int
	if healthCheck {
		// 健康检查开启时，设置默认值
		if cfg.HealthCfg.Timeout <= 0 {
			cfg.HealthCfg.Timeout = DefaultHealthCheckTimeout
			logger.Info("未指定健康检查超时时间，使用默认值", zap.Int("timeout", cfg.HealthCfg.Timeout))
		}

		if cfg.HealthCfg.MaxFails <= 0 {
			cfg.HealthCfg.MaxFails = DefaultHealthCheckMaxFails
			logger.Info("未指定健康检查最大失败次数，使用默认值", zap.Int("max_fails", cfg.HealthCfg.MaxFails))
		}

		if cfg.HealthCfg.Method == "" {
			cfg.HealthCfg.Method = DefaultHealthCheckMethod
			logger.Info("未指定健康检查HTTP方法，使用默认值", zap.String("method", cfg.HealthCfg.Method))
		}

		if cfg.HealthCfg.Route == "" {
			cfg.HealthCfg.Route = DefaultHealthCheckRoute
			logger.Info("未指定健康检查路由，使用默认值", zap.String("route", cfg.HealthCfg.Route))
		}

		if cfg.HealthCfg.HealthyCode <= 0 {
			cfg.HealthCfg.HealthyCode = DefaultHealthCheckHealthyCode
			logger.Info("未指定健康状态码，使用默认值", zap.Int("healthy_code", cfg.HealthCfg.HealthyCode))
		}

		if cfg.HealthCfg.UnhealthyCode <= 0 {
			cfg.HealthCfg.UnhealthyCode = DefaultHealthCheckUnhealthyCode
			logger.Info("未指定不健康状态码，使用默认值", zap.Int("unhealthy_code", cfg.HealthCfg.UnhealthyCode))
		}

		interval = DefaultHealthCheckInterval
	} else {
		logger.Info("健康检查已禁用")
	}

	ctx, cancel := context.WithCancel(context.Background())
	apiClient := newAPIClient(logger)
	healthSvc := newHealthService(cfg.Name, cfg.Port, logger)

	// 设置健康检查服务
	if cfg.HealthHandler != nil {
		// 优先使用HealthHandler接口
		healthSvc.setCustomHandler(cfg.HealthHandler, cfg.HealthCfg.Route)
		logger.Info("使用自定义健康检查处理器")
	} else if cfg.HTTPServer != nil {
		// 兼容旧版本的HTTPServer方式
		healthSvc.setCustomServer(cfg.HTTPServer, cfg.HealthCfg.Route)
		logger.Info("使用标准HTTP服务器")
	}

	return &Service{
		name:        cfg.Name,
		host:        cfg.Host,
		port:        cfg.Port,
		path:        cfg.Path,
		upstreamID:  upstreamID,
		routeID:     routeID,
		healthCheck: healthCheck,
		interval:    interval,
		apiClient:   apiClient,
		healthSvc:   healthSvc,
		logger:      logger,
		ctx:         ctx,
		cancel:      cancel,
	}, nil
}

// Register 注册服务到APISIX
func (s *Service) Register(adminAPI, apiKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if adminAPI == "" {
		return ErrEmptyAdminAPI
	}

	if apiKey == "" {
		s.logger.Warn("未提供API密钥，这可能会导致认证失败")
	}

	err := s.apiClient.createUpstream(
		adminAPI,
		apiKey,
		s.upstreamID,
		s.name,
		s.host,
		s.port,
	)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCreateUpstream, err)
	}

	s.logger.Info("服务已成功注册到APISIX",
		zap.String("service", s.name),
		zap.String("host", s.host),
		zap.Int("port", s.port),
		zap.String("upstream_id", s.upstreamID),
	)

	return nil
}

// StartHealthCheck 启动健康检查服务
func (s *Service) StartHealthCheck() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.healthCheck {
		return nil
	}

	if err := s.healthSvc.start(); err != nil {
		return fmt.Errorf("%w: %v", ErrStartHealthCheck, err)
	}

	return nil
}

// StartWithGracefulShutdown 启动服务并处理优雅关闭（异步）
func (s *Service) StartWithGracefulShutdown(adminAPI, apiKey string) error {
	if err := s.Register(adminAPI, apiKey); err != nil {
		return err
	}

	// 启动健康检查服务
	if err := s.StartHealthCheck(); err != nil {
		// 发生错误时注销服务
		_ = s.Deregister(adminAPI, apiKey)
		return err
	}

	// 异步处理关闭逻辑
	go func() {
		// 监听信号
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

		// 等待信号
		<-quit

		s.logger.Info("关闭信号已接收，开始优雅关闭")

		// 取消上下文
		s.cancel()

		// 从APISIX注销
		if err := s.Deregister(adminAPI, apiKey); err != nil {
			s.logger.Error("从APISIX注销失败", zap.Error(err))
		}

		// 关闭健康检查服务
		ctx, cancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
		defer cancel()

		if err := s.Shutdown(ctx); err != nil {
			s.logger.Error("关闭服务失败", zap.Error(err))
		}

		s.logger.Info("服务已完全关闭")
	}()

	s.logger.Info("服务已启动，将在接收到关闭信号后自动关闭")
	return nil
}

// Deregister 从APISIX注销服务
func (s *Service) Deregister(adminAPI, apiKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if adminAPI == "" {
		return ErrEmptyAdminAPI
	}

	// 构建节点标识符
	nodeKey := fmt.Sprintf("%s:%d", s.host, s.port)

	// 只删除特定节点，而不是整个上游
	if s.upstreamID != "" {
		err := s.apiClient.deleteNode(adminAPI, apiKey, s.upstreamID, nodeKey)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrDeleteNode, err)
		}
	}

	s.logger.Info("服务节点已从APISIX注销",
		zap.String("service", s.name),
		zap.String("upstream_id", s.upstreamID),
		zap.String("node", nodeKey),
	)

	return nil
}

// Shutdown 关闭服务
func (s *Service) Shutdown(ctx context.Context) error {
	if s.healthCheck {
		if err := s.healthSvc.shutdown(ctx); err != nil {
			return fmt.Errorf("%w: %v", ErrShutdownServer, err)
		}
	}
	return nil
}

// SetHealthHandler 设置自定义健康检查处理器
func (s *Service) SetHealthHandler(handler HealthHandler, healthPath string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if handler == nil {
		s.logger.Warn("提供的健康检查处理器为nil，将使用默认健康检查服务")
		return
	}

	// 将自定义处理器传递给健康检查服务
	s.healthSvc.setCustomHandler(handler, healthPath)
	s.logger.Info("已设置自定义健康检查处理器",
		zap.String("health_path", healthPath))
}

// SetHTTPServer 设置自定义HTTP服务器（兼容旧版本）
func (s *Service) SetHTTPServer(server *http.Server, healthPath string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if server == nil {
		s.logger.Warn("提供的HTTP服务器为nil，将使用默认健康检查服务")
		return
	}

	// 将自定义服务器传递给健康检查服务
	s.healthSvc.setCustomServer(server, healthPath)
	s.logger.Info("已设置自定义HTTP服务器",
		zap.String("health_path", healthPath))
}
