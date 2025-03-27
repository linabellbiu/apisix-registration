package apisix_registration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-resty/resty/v2"
	"go.uber.org/zap"
)

// 客户端配置默认值
const (
	defaultTimeout          = 5 * time.Second
	defaultRetryCount       = 3
	defaultRetryWaitTime    = 500 * time.Millisecond
	defaultRetryMaxWaitTime = 2 * time.Second
)

// apisixClient 是与 APISIX Admin API 交互的客户端
type apisixClient struct {
	client *resty.Client
	logger *zap.Logger
}

// newAPIClient 创建一个新的 APISIX 客户端
func newAPIClient(logger *zap.Logger) *apisixClient {
	client := resty.New().
		SetTimeout(defaultTimeout).
		SetRetryCount(defaultRetryCount).
		SetRetryWaitTime(defaultRetryWaitTime).
		SetRetryMaxWaitTime(defaultRetryMaxWaitTime)

	return &apisixClient{
		client: client,
		logger: logger,
	}
}

// checkUpstreamExists 检查上游是否存在
func (c *apisixClient) checkUpstreamExists(adminAPI, apiKey, upstreamID string) (bool, error) {
	url := fmt.Sprintf("%s/upstreams/%s", adminAPI, upstreamID)

	resp, err := c.client.R().
		SetHeader("X-API-KEY", apiKey).
		Get(url)

	if err != nil {
		return false, fmt.Errorf("检查上游请求失败: %w", err)
	}

	// 如果状态码是404，表示上游不存在
	if resp.StatusCode() == http.StatusNotFound {
		return false, nil
	}

	// 如果状态码是200，表示上游存在
	if resp.StatusCode() == http.StatusOK {
		c.logger.Info("上游已存在", zap.String("upstream_id", upstreamID))
		return true, nil
	}

	// 其他状态码视为错误
	return false, fmt.Errorf("检查上游失败，状态码: %d, 响应: %s", resp.StatusCode(), resp.String())
}

// createUpstream 创建上游，如果上游已存在则添加节点
func (c *apisixClient) createUpstream(adminAPI, apiKey, upstreamID, name, host string, port int) error {
	nodeKey := fmt.Sprintf("%s:%d", host, port)

	// 首先检查上游是否存在
	exists, err := c.checkUpstreamExists(adminAPI, apiKey, upstreamID)
	if err != nil {
		return err
	}

	// 如果上游已存在，添加节点
	if exists {
		c.logger.Info("上游已存在，准备添加节点",
			zap.String("upstream_id", upstreamID),
			zap.String("node", nodeKey))

		return c.addNodeToUpstream(adminAPI, apiKey, upstreamID, host, port)
	}

	// 上游不存在，创建新的上游
	url := fmt.Sprintf("%s/upstreams/%s", adminAPI, upstreamID)

	data := map[string]interface{}{
		"name": name,
		"type": "roundrobin",
		"nodes": map[string]int{
			nodeKey: 1,
		},
	}

	resp, err := c.client.R().
		SetHeader("Content-Type", "application/json").
		SetHeader("X-API-KEY", apiKey).
		SetBody(data).
		Put(url)

	if err != nil {
		return fmt.Errorf("创建上游请求失败: %w", err)
	}

	if resp.StatusCode() != http.StatusCreated && resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("创建上游失败，状态码: %d, 响应: %s", resp.StatusCode(), resp.String())
	}

	c.logger.Info("成功创建上游",
		zap.String("upstream_id", upstreamID),
		zap.String("name", name),
		zap.String("host", host),
		zap.Int("port", port),
	)

	return nil
}

// addNodeToUpstream 向现有上游添加节点
func (c *apisixClient) addNodeToUpstream(adminAPI, apiKey, upstreamID, host string, port int) error {
	// 获取当前上游信息
	url := fmt.Sprintf("%s/upstreams/%s", adminAPI, upstreamID)
	nodeKey := fmt.Sprintf("%s:%d", host, port)

	resp, err := c.client.R().
		SetHeader("X-API-KEY", apiKey).
		Get(url)

	if err != nil {
		return fmt.Errorf("获取上游信息失败: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("获取上游信息失败，状态码: %d, 响应: %s", resp.StatusCode(), resp.String())
	}

	// 解析上游信息
	var upstreamData map[string]interface{}
	if err := json.Unmarshal(resp.Body(), &upstreamData); err != nil {
		return fmt.Errorf("解析上游信息失败: %w", err)
	}

	// 获取节点信息
	upstreamValue, ok := upstreamData["value"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("上游数据格式错误")
	}

	nodes, ok := upstreamValue["nodes"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("上游节点数据格式错误")
	}

	// 检查节点是否已存在
	if _, exists := nodes[nodeKey]; exists {
		c.logger.Info("节点已存在，无需添加",
			zap.String("upstream_id", upstreamID),
			zap.String("node", nodeKey))
		return nil
	}

	// 添加新节点
	nodes[nodeKey] = float64(1)

	// 更新上游信息
	updateResp, err := c.client.R().
		SetHeader("Content-Type", "application/json").
		SetHeader("X-API-KEY", apiKey).
		SetBody(upstreamValue).
		Put(url)

	if err != nil {
		return fmt.Errorf("更新上游请求失败: %w", err)
	}

	if updateResp.StatusCode() != http.StatusOK && updateResp.StatusCode() != http.StatusCreated {
		return fmt.Errorf("更新上游失败，状态码: %d, 响应: %s", updateResp.StatusCode(), updateResp.String())
	}

	c.logger.Info("成功添加节点到上游",
		zap.String("upstream_id", upstreamID),
		zap.String("node", nodeKey))

	return nil
}

// createRoute 创建路由
func (c *apisixClient) createRoute(adminAPI, apiKey, routeID, name, path, upstreamID string) error {
	url := fmt.Sprintf("%s/routes/%s", adminAPI, routeID)

	data := map[string]interface{}{
		"name":        name,
		"uri":         path,
		"upstream_id": upstreamID,
	}

	resp, err := c.client.R().
		SetHeader("Content-Type", "application/json").
		SetHeader("X-API-KEY", apiKey).
		SetBody(data).
		Put(url)

	if err != nil {
		return fmt.Errorf("创建路由请求失败: %w", err)
	}

	if resp.StatusCode() != http.StatusCreated && resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("创建路由失败，状态码: %d, 响应: %s", resp.StatusCode(), resp.String())
	}

	c.logger.Info("成功创建路由",
		zap.String("route_id", routeID),
		zap.String("name", name),
		zap.String("path", path),
		zap.String("upstream_id", upstreamID),
	)

	return nil
}

// deleteUpstream 删除上游
func (c *apisixClient) deleteUpstream(adminAPI, apiKey, upstreamID string) error {
	url := fmt.Sprintf("%s/upstreams/%s", adminAPI, upstreamID)

	resp, err := c.client.R().
		SetHeader("X-API-KEY", apiKey).
		Delete(url)

	if err != nil {
		return fmt.Errorf("删除上游请求失败: %w", err)
	}

	if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusNoContent {
		return fmt.Errorf("删除上游失败，状态码: %d, 响应: %s", resp.StatusCode(), resp.String())
	}

	c.logger.Info("成功删除上游", zap.String("upstream_id", upstreamID))
	return nil
}

// deleteRoute 删除路由
func (c *apisixClient) deleteRoute(adminAPI, apiKey, routeID string) error {
	url := fmt.Sprintf("%s/routes/%s", adminAPI, routeID)

	resp, err := c.client.R().
		SetHeader("X-API-KEY", apiKey).
		Delete(url)

	if err != nil {
		return fmt.Errorf("删除路由请求失败: %w", err)
	}

	if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusNoContent {
		return fmt.Errorf("删除路由失败，状态码: %d, 响应: %s", resp.StatusCode(), resp.String())
	}

	c.logger.Info("成功删除路由", zap.String("route_id", routeID))
	return nil
}

// 添加删除节点方法
func (c *apisixClient) deleteNode(adminAPI, apiKey, upstreamID, node string) error {
	// 首先获取当前上游信息
	url := fmt.Sprintf("%s/upstreams/%s", adminAPI, upstreamID)

	resp, err := c.client.R().
		SetHeader("X-API-KEY", apiKey).
		Get(url)

	if err != nil {
		return fmt.Errorf("获取上游信息失败: %w", err)
	}

	if resp.StatusCode() == http.StatusNotFound {
		c.logger.Info("上游不存在，无需删除节点", zap.String("upstream_id", upstreamID))
		return nil
	}

	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("获取上游信息失败，状态码: %d, 响应: %s", resp.StatusCode(), resp.String())
	}

	// 解析上游信息
	var upstreamData map[string]interface{}
	if err := json.Unmarshal(resp.Body(), &upstreamData); err != nil {
		return fmt.Errorf("解析上游信息失败: %w", err)
	}

	// 获取节点信息
	nodes, ok := upstreamData["value"].(map[string]interface{})["nodes"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("上游节点数据格式错误")
	}

	// 检查节点是否存在
	if _, exists := nodes[node]; !exists {
		c.logger.Info("节点不存在，无需删除",
			zap.String("upstream_id", upstreamID),
			zap.String("node", node))
		return nil
	}

	// 从节点列表中删除指定节点
	nodes[node] = nil

	// 如果删除后节点列表为空，整个上游也不用保留了
	// 考虑是否需要这段逻辑,大概率不应该删除整个上游吧
	//if len(nodes) == 0 {
	//	c.logger.Info("删除最后一个节点，将删除整个上游",
	//		zap.String("upstream_id", upstreamID))
	//	return c.deleteUpstream(adminAPI, apiKey, upstreamID)
	//}

	// 更新上游信息
	upstreamData["value"].(map[string]interface{})["nodes"] = nodes

	// 更新上游
	updateResp, err := c.client.R().
		SetHeader("Content-Type", "application/json").
		SetHeader("X-API-KEY", apiKey).
		SetBody(upstreamData["value"]).
		Patch(url)

	if err != nil {
		return fmt.Errorf("更新上游请求失败: %w", err)
	}

	if updateResp.StatusCode() != http.StatusCreated && updateResp.StatusCode() != http.StatusOK {
		return fmt.Errorf("更新上游失败，状态码: %d, 响应: %s", updateResp.StatusCode(), updateResp.String())
	}

	c.logger.Info("服务下线,踢出节点",
		zap.String("upstream_id", upstreamID),
		zap.String("node", node))

	return nil
}
