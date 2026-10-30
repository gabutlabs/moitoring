package email

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"moitoring/internal/config"
	"moitoring/internal/models"
)

type Sender struct {
	settings config.SMTPConfig
}

func NewSender(settings config.SMTPConfig) Sender {
	return Sender{settings: settings}
}

func (s Sender) Send(ctx context.Context, notification models.Notification) error {
	from, err := mail.ParseAddress(s.settings.From)
	if err != nil {
		return errors.New("invalid SMTP sender address")
	}
	if strings.ContainsAny(notification.Subject, "\r\n") {
		return errors.New("email subject contains a line break")
	}
	recipients := make([]string, 0, len(notification.Recipients))
	for _, value := range notification.Recipients {
		address, err := mail.ParseAddress(value)
		if err != nil {
			return errors.New("invalid alert recipient address")
		}
		recipients = append(recipients, address.Address)
	}
	if len(recipients) == 0 {
		return errors.New("alert email has no recipients")
	}

	address := net.JoinHostPort(s.settings.Host, strconv.Itoa(s.settings.Port))
	connection, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return errors.New("could not connect to SMTP server")
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return errors.New("could not set SMTP timeout")
	}
	stopClose := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stopClose()

	client, err := smtp.NewClient(connection, s.settings.Host)
	if err != nil {
		return errors.New("could not start SMTP session")
	}
	defer client.Close()

	if s.settings.StartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP server does not support STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{MinVersion: tls.VersionTLS12, ServerName: s.settings.Host}); err != nil {
			return errors.New("could not establish SMTP TLS")
		}
	}
	if s.settings.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.settings.Username, s.settings.Password, s.settings.Host)); err != nil {
			return errors.New("SMTP authentication failed")
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return errors.New("SMTP rejected the sender")
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return errors.New("SMTP rejected an alert recipient")
		}
	}
	writer, err := client.Data()
	if err != nil {
		return errors.New("SMTP did not accept the message")
	}
	message := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n",
		from.String(), strings.Join(recipients, ", "), mime.QEncoding.Encode("utf-8", notification.Subject), strings.ReplaceAll(notification.Body, "\n", "\r\n"))
	if _, err := writer.Write([]byte(message)); err != nil {
		_ = writer.Close()
		return errors.New("could not write alert email")
	}
	if err := writer.Close(); err != nil {
		return errors.New("SMTP did not finish accepting the email")
	}
	_ = client.Quit()
	return nil
}
