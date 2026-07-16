package config

import (
	"time"

	"github.com/zeromicro/go-zero/zrpc"

	"github.com/soasurs/cordis/pkg/database"
)

type Config struct {
	zrpc.RpcServerConf
	Database database.Config
	Tokens   TokenConfig
	Sessions SessionConfig
	Services ServiceConfig
}

type TokenConfig struct {
	Issuer  string `json:",default=cordis.authenticator.v1"`
	Access  TokenKindConfig
	Refresh TokenKindConfig
}

type TokenKindConfig struct {
	Secret string
	TTL    time.Duration
}

type SessionConfig struct {
	TTL time.Duration
}

type ServiceConfig struct {
	User zrpc.RpcClientConf
}
