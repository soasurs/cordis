package config

import "github.com/zeromicro/go-zero/zrpc"

type Config struct {
	Name     string
	ListenOn string
	Services ServiceConfig
}

type ServiceConfig struct {
	Authenticator zrpc.RpcClientConf
}
