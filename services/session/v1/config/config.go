package config

import (
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/trace"
	"github.com/zeromicro/go-zero/zrpc"

	"github.com/soasurs/cordis/pkg/sessionregistry"
)

type Config struct {
	Name            string
	ListenOn        string
	Timeout         int64 `json:",default=0"`
	Log             logx.LogConf
	DevServer       service.DevServerConfig `json:",optional"`
	Telemetry       trace.Config            `json:",optional"`
	Middlewares     zrpc.ServerMiddlewaresConf
	Node            NodeConfig
	Redis           redis.RedisConf
	SessionRegistry sessionregistry.Config
	Services        ServiceConfig
}

type NodeConfig struct {
	ID                   string
	AdvertiseAddress     string
	HeartbeatSeconds     int `json:",default=15"`
	NodeTTLSeconds       int `json:",default=30"`
	SessionResumeSeconds int `json:",default=120"`
	MaxReplayEvents      int `json:",default=2048"`
	RouteRefreshSeconds  int `json:",default=15"`
	RouteTTLSeconds      int `json:",default=30"`
	BindingQueueSize     int `json:",default=4096"`
	DrainSeconds         int `json:",default=30"`
}

type ServiceConfig struct {
	Authenticator zrpc.RpcClientConf
	Presence      zrpc.RpcClientConf
	Guild         zrpc.RpcClientConf
	Message       zrpc.RpcClientConf
}

func (c Config) RPCConfig() zrpc.RpcServerConf {
	return zrpc.RpcServerConf{
		ServiceConf: service.ServiceConf{
			Name: c.Name, Log: c.Log, DevServer: c.DevServer, Telemetry: c.Telemetry,
		},
		ListenOn: c.ListenOn, Timeout: c.Timeout, Middlewares: c.Middlewares,
	}
}

func (c NodeConfig) HeartbeatInterval() time.Duration {
	if c.HeartbeatSeconds <= 0 {
		return 15 * time.Second
	}
	return time.Duration(c.HeartbeatSeconds) * time.Second
}

func (c NodeConfig) NodeTTL() time.Duration {
	if c.NodeTTLSeconds <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.NodeTTLSeconds) * time.Second
}

func (c NodeConfig) ResumeTTL() time.Duration {
	if c.SessionResumeSeconds <= 0 {
		return 2 * time.Minute
	}
	return time.Duration(c.SessionResumeSeconds) * time.Second
}

func (c NodeConfig) ReplayLimit() int {
	if c.MaxReplayEvents <= 0 {
		return 2048
	}
	return c.MaxReplayEvents
}

func (c NodeConfig) RouteRefreshInterval() time.Duration {
	if c.RouteRefreshSeconds <= 0 {
		return 15 * time.Second
	}
	return time.Duration(c.RouteRefreshSeconds) * time.Second
}

func (c NodeConfig) RouteTTL() time.Duration {
	if c.RouteTTLSeconds <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.RouteTTLSeconds) * time.Second
}

func (c NodeConfig) QueueSize() int {
	if c.BindingQueueSize <= 0 {
		return 4096
	}
	return c.BindingQueueSize
}

func (c NodeConfig) DrainWindow() time.Duration {
	if c.DrainSeconds <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.DrainSeconds) * time.Second
}
