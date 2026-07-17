package config

import (
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	Mailer MailerConfig
}

// MailerConfig selects the outbound mail provider. Only "noop" is supported
// today; real providers plug in here without touching callers.
type MailerConfig struct {
	Provider string `json:",default=noop,options=noop"`
}
