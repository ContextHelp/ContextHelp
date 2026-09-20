package security

import (
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

func TestConfigFromAlertsDefaults(t *testing.T) {
	c := ConfigFromAlerts(config.SecurityAlertsConfig{})
	if c.AuthFailureThreshold != 3 {
		t.Errorf("AuthFailureThreshold = %d, want default 3", c.AuthFailureThreshold)
	}
	if c.ACLDenialThreshold != 10 {
		t.Errorf("ACLDenialThreshold = %d, want default 10", c.ACLDenialThreshold)
	}
	if c.WindowDuration != 60*time.Second {
		t.Errorf("WindowDuration = %s, want 60s", c.WindowDuration)
	}
}

func TestConfigFromAlertsOverrides(t *testing.T) {
	c := ConfigFromAlerts(config.SecurityAlertsConfig{
		AuthFailureThreshold: 7,
		ACLDenialThreshold:   2,
		WebhookURL:           "https://alerts.example/hook",
		SMTP: config.SecuritySMTPConfig{
			Host: "smtp.example", Port: 587, From: "a@example", To: "b@example",
		},
	})
	if c.AuthFailureThreshold != 7 || c.ACLDenialThreshold != 2 {
		t.Errorf("thresholds = %d/%d, want 7/2", c.AuthFailureThreshold, c.ACLDenialThreshold)
	}
	if c.WebhookURL != "https://alerts.example/hook" {
		t.Errorf("WebhookURL = %q", c.WebhookURL)
	}
	if c.SMTP.Host != "smtp.example" || c.SMTP.To != "b@example" {
		t.Errorf("SMTP = %+v", c.SMTP)
	}
}
