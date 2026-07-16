package config

import (
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"

	"github.com/soasurs/cordis/services/api/v1/observability"
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
	Message       zrpc.RpcClientConf
}
