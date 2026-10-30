package email

import (
	"context"
	"strings"
	"testing"

	"moitoring/internal/config"
	"moitoring/internal/models"
)

func TestSendRejectsSubjectHeaderInjectionBeforeConnecting(t *testing.T) {
	sender := NewSender(config.SMTPConfig{From: "monitor@example.local"})
	err := sender.Send(context.Background(), models.Notification{
		Recipients: []string{"ops@example.local"},
		Subject:    "alert\r\nBcc: attacker@example.local",
	})
	if err == nil || !strings.Contains(err.Error(), "line break") {
		t.Fatalf("Send() error = %v, want subject line-break rejection", err)
	}
}

func TestSendRejectsInvalidRecipientBeforeConnecting(t *testing.T) {
	sender := NewSender(config.SMTPConfig{From: "monitor@example.local"})
	err := sender.Send(context.Background(), models.Notification{
		Recipients: []string{"invalid recipient"},
		Subject:    "RabbitMQ is down",
	})
	if err == nil || !strings.Contains(err.Error(), "recipient") {
		t.Fatalf("Send() error = %v, want recipient validation error", err)
	}
}
