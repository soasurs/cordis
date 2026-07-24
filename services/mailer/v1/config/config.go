package config

import (
	"time"

	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	Mailer MailerConfig
}

// MailerConfig selects the outbound mail provider and configures links used by
// transactional email templates.
type MailerConfig struct {
	Provider             string     `json:",default=noop,options=noop|smtp"`
	From                 string     `json:",optional"`
	PasswordResetURL     string     `json:",optional"`
	EmailVerificationURL string     `json:",optional"`
	SMTP                 SMTPConfig `json:",optional"`
}

// SMTPConfig controls SMTP transport security and optional authentication.
type SMTPConfig struct {
	Address    string        `json:",optional"`
	Username   string        `json:",optional"`
	Password   string        `json:",optional"`
	RequireTLS bool          `json:",default=true"`
	Timeout    time.Duration `json:",default=10s"`
}
