package email

import (
	"fmt"
	"net/smtp"
	"strings"
)

type Config struct {
	SMTPHost string
	SMTPPort int
	From     string
	// For Mailpit/dev, no auth needed. For production, add username/password.
}

type Service struct {
	cfg Config
}

func NewService(cfg Config) *Service {
	return &Service{cfg: cfg}
}

func (s *Service) SendVerification(to, name, verifyURL string) error {
	subject := "Verify your TunnelEdge account"
	body := buildVerificationEmail(name, verifyURL)
	return s.send(to, subject, body)
}

func (s *Service) send(to, subject, htmlBody string) error {
	addr := fmt.Sprintf("%s:%d", s.cfg.SMTPHost, s.cfg.SMTPPort)

	msg := strings.Join([]string{
		"From: " + s.cfg.From,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=UTF-8",
		"",
		htmlBody,
	}, "\r\n")

	// Mailpit / local dev: no auth
	return smtp.SendMail(addr, nil, s.cfg.From, []string{to}, []byte(msg))
}

func buildVerificationEmail(name, verifyURL string) string {
	return `<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<style>
  body { margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #0f172a; color: #e2e8f0; }
  .container { max-width: 480px; margin: 0 auto; padding: 40px 24px; }
  .card { background: #1e293b; border: 1px solid #334155; border-radius: 12px; padding: 32px; }
  .logo { font-size: 24px; font-weight: 700; color: #38bdf8; margin-bottom: 8px; }
  .subtitle { color: #94a3b8; font-size: 14px; margin-bottom: 24px; }
  h2 { font-size: 20px; margin: 0 0 12px; }
  p { color: #cbd5e1; line-height: 1.6; margin: 0 0 16px; }
  .btn { display: inline-block; padding: 12px 32px; background: #0ea5e9; color: #fff; text-decoration: none; border-radius: 8px; font-weight: 600; font-size: 14px; }
  .btn:hover { background: #0284c7; }
  .footer { margin-top: 24px; font-size: 12px; color: #64748b; text-align: center; }
  .url { word-break: break-all; color: #64748b; font-size: 12px; }
</style>
</head>
<body>
<div class="container">
  <div class="card">
    <div class="logo">⚡ TunnelEdge</div>
    <div class="subtitle">Tunnel Management Platform</div>
    <h2>Verify your email</h2>
    <p>Hi ` + name + `,</p>
    <p>Thanks for signing up! Please verify your email address to activate your account.</p>
    <p style="text-align:center; margin: 24px 0;">
      <a href="` + verifyURL + `" class="btn">Verify Email</a>
    </p>
    <p class="url">Or copy this link:<br>` + verifyURL + `</p>
    <p style="font-size:13px; color:#94a3b8;">This link expires in 24 hours.</p>
  </div>
  <div class="footer">
    TunnelEdge — Secure tunnel management<br>
    If you didn't create an account, you can ignore this email.
  </div>
</div>
</body>
</html>`
}
