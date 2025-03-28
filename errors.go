package apisix_registration

import (
	"errors"
	"time"
)

// 基本常量和默认配置
const (
	// DefaultHealthCheckInterval 默认健康检查间隔(秒)
	DefaultHealthCheckInterval = 5

	// DefaultShutdownTimeout 默认关闭超时时间(秒)
	DefaultShutdownTimeout = 3 * time.Second
)

// 错误定义
var (
	// ErrEmptyHost 主机名不能为空
	ErrEmptyHost = errors.New("主机不能为空")

	// ErrInvalidPort 端口号必须大于0
	ErrInvalidPort = errors.New("端口必须大于0")

	// ErrEmptyAdminAPI APISIX Admin API地址不能为空
	ErrEmptyAdminAPI = errors.New("APISIX Admin API 地址不能为空")

	// ErrCreateUpstream 创建上游失败
	ErrCreateUpstream = errors.New("创建上游失败")

	// ErrCreateRoute 创建路由失败
	ErrCreateRoute = errors.New("创建路由失败")

	// ErrDeleteUpstream 删除上游失败
	ErrDeleteUpstream = errors.New("删除上游失败")

	// ErrDeleteRoute 删除路由失败
	ErrDeleteRoute = errors.New("删除路由失败")

	// ErrDeleteNode 从上游删除节点失败
	ErrDeleteNode = errors.New("从上游删除节点失败")

	// ErrStartHealthCheck 启动健康检查服务失败
	ErrStartHealthCheck = errors.New("启动健康检查服务失败")

	// ErrShutdownServer 关闭服务失败
	ErrShutdownServer = errors.New("关闭服务失败")

	// ErrInvalidConfig 配置验证失败
	ErrInvalidConfig = errors.New("配置验证失败")
)
