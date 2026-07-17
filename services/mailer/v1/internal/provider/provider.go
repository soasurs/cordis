// Package provider defines the outbound email delivery boundary of the
// mailer service. Implementations must treat template variables as secrets
// and never log them verbatim.
package provider

import (
	"context"
	"strings"

	"github.com/zeromicro/go-zero/core/logx"
)

// Provider delivers one templated email.
type Provider interface {
	Send(ctx context.Context, to, template string, variables map[string]string) error
}

// Noop satisfies Provider without delivering anything. It logs a redacted
// record so local flows stay observable.
type Noop struct{}

func NewNoop() *Noop {
	return &Noop{}
}

func (p *Noop) Send(ctx context.Context, to, template string, _ map[string]string) error {
	logx.WithContext(ctx).Infow(
		"mailer noop delivery",
		logx.Field("to", MaskEmail(to)),
		logx.Field("template", template),
	)
	return nil
}

// MaskEmail hides most of the local part so addresses stay out of logs.
func MaskEmail(email string) string {
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" {
		return "***"
	}
	return local[:1] + "***@" + domain
}
