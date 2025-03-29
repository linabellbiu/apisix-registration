package apisix_registration

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"go.uber.org/zap"
)

// 健康检查相关常量
const (
	DefaultHost              = "127.0.0.1"
	DefaultHealthCheckMethod = "GET"
	DefaultHealthCheckRoute  = "/health"
	DefaultAdminApi          = "http://192.168.3.71:9180/apisix/admin"
)

type Service struct {
	name        string
	host        string
	port        int
	path        string
	adminApi    string // APISIX Admin API 地址
	apiKey      string // APISIX Admin API 密钥
	upstreamID  string
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
	Id            string `json:",optional"` // Id 自定义上游ID，如果为空则自动生成
	UpstreamTypes string `json:",optional"` // UpstreamTypes 指定上游服务的类型。
}

// HealthCheckConfig 健康检查的配置
type HealthCheckConfig struct {
	Enabled bool   `json:",optional"` // 是否启用健康检查
	Path    string `json:",optional"` // 健康检查路由
}

// Config 是服务配置
type Config struct {
	Enabled   bool              `json:",optional"`
	Name      string            // 服务名称
	Port      int               // 服务端口
	Host      string            `json:",optional"` // 服务主机名
	Upstream  Upstream          `json:",optional"`
	AdminApi  string            `json:",optional"` // APISIX Admin API 地址
	ApiKey    string            `json:",optional"` // APISIX Admin API 密钥
	HealthCfg HealthCheckConfig `json:",optional"` // 健康检查配置

	// 可以使用以下两种方式之一来集成自定义HTTP服务：
	// 1. 使用标准HTTP服务器
	httpServer *http.Server
	// 2. 使用自定义健康检查处理器（支持不同框架）
	healthHandler HealthHandler
}

type Option func(Config)

func OptionsWithHttpServer(server *http.Server) Option {
	return func(config Config) {
		config.httpServer = server
	}
}

func OptionsWithHealthHandler(health HealthHandler) Option {
	return func(config Config) {
		config.healthHandler = health
	}
}

// New 创建一个新的服务实例
func New(cfg Config, o ...Option) (*Service, error) {
	if !cfg.Enabled {
		log.Println("注册服务未开启")
		return &Service{}, nil
	}
	logger, _ := zap.NewProduction()

	if cfg.AdminApi == "" {
		cfg.AdminApi = DefaultAdminApi
	}

	if cfg.Name == "" {
		return nil, fmt.Errorf("%w: 服务名称不能为空", ErrInvalidConfig)
	}

	if cfg.Host == "" {
		cfg.Host = DefaultHost
	}

	if cfg.Port <= 0 {
		return nil, ErrInvalidPort
	}

	// 生成或使用上游ID
	upstreamID := cfg.Upstream.Id
	if upstreamID == "" {
		upstreamID = fmt.Sprintf("%s_%s_%d", cfg.Name, cfg.Host, cfg.Port)
		logger.Info("未指定上游ID，自动生成", zap.String("upstream_id", upstreamID))
	}

	// 处理健康检查配置
	healthCheck := cfg.HealthCfg.Enabled
	if cfg.HealthCfg.Enabled {
		// 健康检查开启时，设置默认值
		if cfg.HealthCfg.Path == "" {
			cfg.HealthCfg.Path = DefaultHealthCheckRoute
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	apiClient := newAPIClient(logger)
	healthSvc := newHealthService(cfg.Name, cfg.Port, logger)

	for _, f := range o {
		f(cfg)
	}

	// 设置健康检查服务
	if cfg.healthHandler != nil {
		// 优先使用HealthHandler接口
		healthSvc.setCustomHandler(cfg.healthHandler, cfg.HealthCfg.Path)
	} else if cfg.httpServer != nil {
		// 兼容旧版本的HTTPServer方式
		healthSvc.setCustomServer(cfg.httpServer, cfg.HealthCfg.Path)
	}

	return &Service{
		apiKey:      cfg.ApiKey,
		adminApi:    cfg.AdminApi,
		name:        cfg.Name,
		host:        cfg.Host,
		port:        cfg.Port,
		upstreamID:  upstreamID,
		healthCheck: healthCheck,
		apiClient:   apiClient,
		healthSvc:   healthSvc,
		logger:      logger,
		ctx:         ctx,
		cancel:      cancel,
	}, nil
}

// Register 注册服务到APISIX
func (s *Service) Register() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.adminApi == "" {
		return ErrEmptyAdminAPI
	}

	if s.apiKey == "" {
		s.logger.Warn("未提供API密钥，这可能会导致认证失败")
	}

	err := s.apiClient.createUpstream(
		s.adminApi,
		s.apiKey,
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

// Start 启动服务
func (s *Service) Start() error {
	if err := s.Register(); err != nil {
		return err
	}

	// 启动健康检查服务
	if err := s.StartHealthCheck(); err != nil {
		s.logger.Error("启动健康检查失败")
		return err
	}

	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

		<-quit

		s.logger.Info("关闭信号已接收，开始注册服务关闭")
		s.cancel()

		// 从APISIX注销
		if err := s.Deregister(); err != nil {
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

	s.logger.Info("注册服务已启动")
	return nil
}

// Deregister 从APISIX注销服务
func (s *Service) Deregister() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.adminApi == "" {
		return ErrEmptyAdminAPI
	}

	nodeKey := fmt.Sprintf("%s:%d", s.host, s.port)
	if s.upstreamID != "" {
		err := s.apiClient.deleteNode(s.adminApi, s.apiKey, s.upstreamID, nodeKey)
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
