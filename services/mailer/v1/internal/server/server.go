package server

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mailerv1 "github.com/soasurs/cordis/gen/mailer/v1"
	"github.com/soasurs/cordis/pkg/mail"
	"github.com/soasurs/cordis/services/mailer/v1/internal/svc"
)

type mailerServer struct {
	svcCtx *svc.ServiceContext
}

func New(svcCtx *svc.ServiceContext) mailerv1.MailerServiceServer {
	return &mailerServer{svcCtx: svcCtx}
}

var knownTemplates = map[string]bool{
	mail.TemplatePasswordReset:     true,
	mail.TemplateEmailVerification: true,
}

func (s *mailerServer) SendEmail(ctx context.Context, req *mailerv1.SendEmailRequest) (*mailerv1.SendEmailResponse, error) {
	to := strings.TrimSpace(req.GetTo())
	if to == "" {
		return nil, status.Error(codes.InvalidArgument, "recipient is required")
	}
	if !isValidEmail(to) {
		return nil, status.Error(codes.InvalidArgument, "invalid recipient email format")
	}
	if !knownTemplates[req.GetTemplate()] {
		return nil, status.Error(codes.InvalidArgument, "unknown template")
	}

	if err := s.svcCtx.Provider.Send(ctx, to, req.GetTemplate(), req.GetVariables()); err != nil {
		return nil, status.Error(codes.Internal, "mail delivery failed")
	}

	resp := new(mailerv1.SendEmailResponse)
	resp.SetOk(true)
	return resp, nil
}

func isValidEmail(email string) bool {
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" || domain == "" {
		return false
	}
	return strings.Contains(domain, ".")
}
