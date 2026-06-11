package config

import (
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	Name     string
	ListenOn string
	Log      logx.LogConf
	RPC      zrpc.RpcServerConf
	Gateway  GatewayConfig `json:",optional"`
	Services ServiceConfig
}

type GatewayConfig struct {
	WebSocketPath          string `json:",default=/ws"`
	HeartbeatIntervalMs    int    `json:",default=30000"`
	IdentifyTimeoutSeconds int    `json:",default=10"`
	RouteRefreshSeconds    int    `json:",default=15"`
	GatewayRefreshSeconds  int    `json:",default=15"`
	MaxMessageBytes        int64  `json:",default=65536"`
}

type ServiceConfig struct {
	Authenticator zrpc.RpcClientConf
	Presence      zrpc.RpcClientConf
}

func (c GatewayConfig) WebSocketRoute() string {
	if c.WebSocketPath == "" {
		return "/ws"
	}
	return c.WebSocketPath
}

func (c GatewayConfig) HeartbeatInterval() time.Duration {
	if c.HeartbeatIntervalMs <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.HeartbeatIntervalMs) * time.Millisecond
}

func (c GatewayConfig) IdentifyTimeout() time.Duration {
	if c.IdentifyTimeoutSeconds <= 0 {
		return 10 * time.Second
	}
	return time.Duration(c.IdentifyTimeoutSeconds) * time.Second
}

func (c GatewayConfig) RouteRefreshInterval() time.Duration {
	if c.RouteRefreshSeconds <= 0 {
		return 15 * time.Second
	}
	return time.Duration(c.RouteRefreshSeconds) * time.Second
}

func (c GatewayConfig) GatewayRefreshInterval() time.Duration {
	if c.GatewayRefreshSeconds <= 0 {
		return 15 * time.Second
	}
	return time.Duration(c.GatewayRefreshSeconds) * time.Second
}

func (c GatewayConfig) MessageLimit() int64 {
	if c.MaxMessageBytes <= 0 {
		return 65536
	}
	return c.MaxMessageBytes
}
