package security

import "github.com/ideacrafterslabs/ctxt/internal/config"

// ConfigFromAlerts maps the user-facing security.alerts config onto the
// emitter Config, applying emitter defaults for unset thresholds so a
// zero-valued config still alerts sensibly.
func ConfigFromAlerts(a config.SecurityAlertsConfig) Config {
	c := DefaultConfig()
	if a.AuthFailureThreshold > 0 {
		c.AuthFailureThreshold = a.AuthFailureThreshold
	}
	if a.ACLDenialThreshold > 0 {
		c.ACLDenialThreshold = a.ACLDenialThreshold
	}
	c.WebhookURL = a.WebhookURL
	c.SMTP = SMTPConfig{
		Host: a.SMTP.Host,
		Port: a.SMTP.Port,
		From: a.SMTP.From,
		To:   a.SMTP.To,
	}
	return c
}
