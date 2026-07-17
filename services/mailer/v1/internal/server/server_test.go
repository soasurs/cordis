package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mailerv1 "github.com/soasurs/cordis/gen/mailer/v1"
	"github.com/soasurs/cordis/pkg/mail"
	"github.com/soasurs/cordis/services/mailer/v1/config"
	"github.com/soasurs/cordis/services/mailer/v1/internal/provider"
	"github.com/soasurs/cordis/services/mailer/v1/internal/svc"
)

type recordedDelivery struct {
	to        string
	template  string
	variables map[string]string
}

type fakeProvider struct {
	deliveries []recordedDelivery
	err        error
}

func (p *fakeProvider) Send(_ context.Context, to, template string, variables map[string]string) error {
	p.deliveries = append(p.deliveries, recordedDelivery{to: to, template: template, variables: variables})
	return p.err
}

func newTestMailerServer(p provider.Provider) mailerv1.MailerServiceServer {
	return New(svc.NewServiceContextWithDependencies(config.Config{}, svc.Dependencies{Provider: p}))
}

func TestSendEmailDeliversThroughProvider(t *testing.T) {
	fake := new(fakeProvider)
	server := newTestMailerServer(fake)

	req := new(mailerv1.SendEmailRequest)
	req.SetTo("user@example.com")
	req.SetTemplate(mail.TemplatePasswordReset)
	req.SetVariables(map[string]string{mail.VariableToken: "raw-token"})
	resp, err := server.SendEmail(t.Context(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())

	require.Len(t, fake.deliveries, 1)
	require.Equal(t, "user@example.com", fake.deliveries[0].to)
	require.Equal(t, mail.TemplatePasswordReset, fake.deliveries[0].template)
	require.Equal(t, "raw-token", fake.deliveries[0].variables[mail.VariableToken])
}

func TestSendEmailValidation(t *testing.T) {
	server := newTestMailerServer(new(fakeProvider))

	req := new(mailerv1.SendEmailRequest)
	req.SetTemplate(mail.TemplatePasswordReset)
	_, err := server.SendEmail(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	req.SetTo("not-an-email")
	_, err = server.SendEmail(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	req.SetTo("user@example.com")
	req.SetTemplate("unknown_template")
	_, err = server.SendEmail(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestSendEmailProviderFailure(t *testing.T) {
	server := newTestMailerServer(&fakeProvider{err: errors.New("smtp down")})

	req := new(mailerv1.SendEmailRequest)
	req.SetTo("user@example.com")
	req.SetTemplate(mail.TemplateEmailVerification)
	_, err := server.SendEmail(t.Context(), req)
	require.Equal(t, codes.Internal, status.Code(err))
}

func TestMaskEmail(t *testing.T) {
	require.Equal(t, "u***@example.com", provider.MaskEmail("user@example.com"))
	require.Equal(t, "***", provider.MaskEmail("invalid"))
	require.Equal(t, "***", provider.MaskEmail("@example.com"))
}
