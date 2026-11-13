package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	"time"
)

// Sender delivers an event through one kind of channel.
type Sender interface {
	// RequiredConfig lists the config keys the channel must provide.
	RequiredConfig() []string
	Send(ctx context.Context, config map[string]string, event Event) error
}

func defaultSenders(client *http.Client) map[Kind]Sender {
	return map[Kind]Sender{
		KindDiscord:  discordSender{client},
		KindSlack:    slackSender{client},
		KindTelegram: telegramSender{client},
		KindWebhook:  webhookSender{client},
		KindEmail:    emailSender{},
	}
}

type discordSender struct{ client *http.Client }

func (discordSender) RequiredConfig() []string { return []string{"webhook_url"} }

func (s discordSender) Send(ctx context.Context, config map[string]string, event Event) error {
	color := 0x2FBF71
	if event.Failure() {
		color = 0xE5484D
	}
	payload := map[string]any{"embeds": []map[string]any{{
		"title": event.Title, "description": event.Message, "color": color,
		"timestamp": event.OccurredAt.UTC().Format(time.RFC3339),
	}}}
	return postJSON(ctx, s.client, config["webhook_url"], payload)
}

type slackSender struct{ client *http.Client }

func (slackSender) RequiredConfig() []string { return []string{"webhook_url"} }

func (s slackSender) Send(ctx context.Context, config map[string]string, event Event) error {
	payload := map[string]any{"text": "*" + escapeSlack(event.Title) + "*\n" + escapeSlack(event.Message)}
	return postJSON(ctx, s.client, config["webhook_url"], payload)
}

type telegramSender struct{ client *http.Client }

func (telegramSender) RequiredConfig() []string { return []string{"bot_token", "chat_id"} }

func (s telegramSender) Send(ctx context.Context, config map[string]string, event Event) error {
	endpoint := "https://api.telegram.org/bot" + url.PathEscape(config["bot_token"]) + "/sendMessage"
	payload := map[string]any{"chat_id": config["chat_id"], "text": event.Title + "\n\n" + event.Message}
	return postJSON(ctx, s.client, endpoint, payload)
}

type webhookSender struct{ client *http.Client }

func (webhookSender) RequiredConfig() []string { return []string{"url"} }

func (s webhookSender) Send(ctx context.Context, config map[string]string, event Event) error {
	return postJSON(ctx, s.client, config["url"], event)
}

type emailSender struct{}

func (emailSender) RequiredConfig() []string { return []string{"host", "port", "from", "to"} }

func (emailSender) Send(ctx context.Context, config map[string]string, event Event) error {
	address := net.JoinHostPort(config["host"], config["port"])
	var auth smtp.Auth
	if config["username"] != "" {
		auth = smtp.PlainAuth("", config["username"], config["password"], config["host"])
	}
	recipients := strings.Split(config["to"], ",")
	for i := range recipients {
		recipients[i] = strings.TrimSpace(recipients[i])
	}
	if hasLineBreak(config["from"]) || hasLineBreak(strings.Join(recipients, ",")) {
		return fmt.Errorf("e-mail addresses must not contain line breaks")
	}
	message := "From: " + sanitizeHeader(config["from"]) + "\r\n" +
		"To: " + sanitizeHeader(strings.Join(recipients, ", ")) + "\r\n" +
		"Subject: " + sanitizeHeader(event.Title) + "\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" +
		event.Message + "\r\n"
	if err := sendMail(ctx, address, config["host"], auth, config["from"], recipients, []byte(message), config["require_tls"] == "true"); err != nil {
		return fmt.Errorf("send e-mail via %s: %w", address, err)
	}
	return nil
}

// sendMail does what smtp.SendMail does (STARTTLS when offered, then auth)
// but within ctx: smtp.SendMail has no deadline, so a server that stops
// answering would hold the delivery, and every channel after it, forever.
func sendMail(ctx context.Context, address, host string, auth smtp.Auth, from string, to []string, message []byte, requireTLS ...bool) error {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()
	supported, _ := client.Extension("STARTTLS")
	if len(requireTLS) > 0 && requireTLS[0] && !supported {
		return fmt.Errorf("SMTP server does not offer required STARTTLS")
	}
	if supported {
		if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return err
		}
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(message); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func hasLineBreak(value string) bool { return strings.ContainsAny(value, "\r\n") }

func sanitizeHeader(value string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(value)
}

func postJSON(ctx context.Context, client *http.Client, endpoint string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "pier-notifier")
	response, err := client.Do(request)
	if err != nil {
		// The URL may embed a secret token, so the transport error is not echoed verbatim.
		return fmt.Errorf("request failed: %w", redactURL(err))
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("destination answered %d: %s", response.StatusCode, strings.TrimSpace(string(detail)))
	}
	return nil
}

func redactURL(err error) error {
	if urlErr, ok := err.(*url.Error); ok {
		return urlErr.Err
	}
	return err
}

func escapeSlack(value string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(value)
}
