package config

import (
	"github.com/zeromicro/go-zero/zrpc"

	"github.com/soasurs/cordis/pkg/database"
)

type Config struct {
	zrpc.RpcServerConf
	Database database.Config
}
