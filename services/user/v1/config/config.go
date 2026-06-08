package config

import (
	"github.com/soasurs/cordis/pkg/database"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	Database database.Config
}
