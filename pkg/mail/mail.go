// Package mail defines the shared vocabulary between mail-sending services
// and the mailer service: template identifiers and their variable keys.
package mail

const (
	// TemplatePasswordReset delivers a password reset link.
	TemplatePasswordReset = "password_reset"
	// TemplateEmailVerification delivers an email ownership confirmation link.
	TemplateEmailVerification = "email_verification"
)

// VariableToken carries the single-use recovery token. Its value is a secret
// and must never be logged.
const VariableToken = "token"
