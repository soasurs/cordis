package config

import (
	"github.com/soasurs/cordis/services/api/v1/observability"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	Name          string
	ListenOn      string
	Log           logx.LogConf
	Observability observability.Config
	Services      ServiceConfig
}

type ServiceConfig struct {
	Authenticator zrpc.RpcClientConf
	User          zrpc.RpcClientConf
}
