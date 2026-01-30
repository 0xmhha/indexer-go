package notifications

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html/template"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// EmailHandler handles email notification delivery.
type EmailHandler struct {
	config    *EmailConfig
	logger    *zap.Logger
	templates map[string]*template.Template
	mu        sync.RWMutex

	// Rate limiting
	rateLimiter *rateLimiter
}

// rateLimiter implements a simple token bucket rate limiter.
type rateLimiter struct {
	mu        sync.Mutex
	tokens    int
	maxTokens int
	lastTime  time.Time
	interval  time.Duration
}

func newRateLimiter(ratePerMinute int) *rateLimiter {
	return &rateLimiter{
		tokens:    ratePerMinute,
		maxTokens: ratePerMinute,
		lastTime:  time.Now(),
		interval:  time.Minute,
	}
}

func (r *rateLimiter) allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(r.lastTime)

	// Refill tokens based on elapsed time
	if elapsed >= r.interval {
		r.tokens = r.maxTokens
		r.lastTime = now
	} else {
		refill := int(float64(r.maxTokens) * elapsed.Seconds() / r.interval.Seconds())
		r.tokens += refill
		if r.tokens > r.maxTokens {
			r.tokens = r.maxTokens
		}
		r.lastTime = now
	}

	if r.tokens > 0 {
		r.tokens--
		return true
	}
	return false
}

// NewEmailHandler creates a new email handler.
func NewEmailHandler(config *EmailConfig, logger *zap.Logger) *EmailHandler {
	if config == nil {
		config = &EmailConfig{
			Enabled:            false,
			SMTPPort:           587,
			UseTLS:             true,
			MaxRecipients:      10,
			RateLimitPerMinute: 60,
		}
	}

	rateLimit := config.RateLimitPerMinute
	if rateLimit <= 0 {
		rateLimit = 60
	}

	return &EmailHandler{
		config:      config,
		logger:      logger.Named("email"),
		templates:   make(map[string]*template.Template),
		rateLimiter: newRateLimiter(rateLimit),
	}
}

// Type returns the notification type.
func (h *EmailHandler) Type() NotificationType {
	return NotificationTypeEmail
}

// Validate validates an email notification setting.
func (h *EmailHandler) Validate(setting *NotificationSetting) error {
	if len(setting.Destination.EmailTo) == 0 {
		return fmt.Errorf("at least one email recipient is required")
	}

	totalRecipients := len(setting.Destination.EmailTo) + len(setting.Destination.EmailCC)
	if totalRecipients > h.config.MaxRecipients {
		return fmt.Errorf("too many recipients: %d (max: %d)", totalRecipients, h.config.MaxRecipients)
	}

	// Validate email format
	for _, email := range setting.Destination.EmailTo {
		if !isValidEmail(email) {
			return fmt.Errorf("invalid email address: %s", email)
		}
	}
	for _, email := range setting.Destination.EmailCC {
		if !isValidEmail(email) {
			return fmt.Errorf("invalid CC email address: %s", email)
		}
	}

	return nil
}

// isValidEmail performs basic email validation.
func isValidEmail(email string) bool {
	// Basic validation: contains @ and has content before and after
	at := strings.Index(email, "@")
	if at < 1 {
		return false
	}
	dot := strings.LastIndex(email[at:], ".")
	if dot < 1 || dot >= len(email[at:])-1 {
		return false
	}
	return true
}

// Deliver delivers an email notification.
func (h *EmailHandler) Deliver(ctx context.Context, notification *Notification, setting *NotificationSetting) (*DeliveryResult, error) {
	start := time.Now()
	result := &DeliveryResult{
		DeliveredAt: start,
	}

	// Check rate limit
	if !h.rateLimiter.allow() {
		result.Success = false
		result.Error = "rate limit exceeded"
		result.Duration = time.Since(start).Milliseconds()
		return result, fmt.Errorf("rate limit exceeded")
	}

	// Build email message
	subject := h.buildSubject(notification, setting)
	body, err := h.buildBody(notification, setting)
	if err != nil {
		result.Success = false
		result.Error = fmt.Sprintf("failed to build email body: %v", err)
		result.Duration = time.Since(start).Milliseconds()
		return result, err
	}

	// Prepare email
	msg := h.buildMessage(setting.Destination.EmailTo, setting.Destination.EmailCC, subject, body)

	// Send email
	err = h.sendMail(ctx, setting.Destination.EmailTo, setting.Destination.EmailCC, msg)
	result.Duration = time.Since(start).Milliseconds()

	if err != nil {
		result.Success = false
		result.Error = fmt.Sprintf("failed to send email: %v", err)
		h.logger.Warn("email delivery failed",
			zap.String("notification_id", notification.ID),
			zap.Error(err))
		return result, err
	}

	result.Success = true
	h.logger.Debug("email delivered successfully",
		zap.String("notification_id", notification.ID),
		zap.Int64("duration_ms", result.Duration))

	return result, nil
}

// buildSubject builds the email subject.
func (h *EmailHandler) buildSubject(notification *Notification, setting *NotificationSetting) string {
	if setting.Destination.EmailSubject != "" {
		return setting.Destination.EmailSubject
	}

	switch notification.EventType {
	case EventTypeBlock:
		return fmt.Sprintf("[Indexer] New Block #%d", notification.Payload.BlockNumber)
	case EventTypeTransaction:
		return fmt.Sprintf("[Indexer] New Transaction in Block #%d", notification.Payload.BlockNumber)
	case EventTypeLog:
		return fmt.Sprintf("[Indexer] Event Log in Block #%d", notification.Payload.BlockNumber)
	default:
		return fmt.Sprintf("[Indexer] %s Notification", notification.EventType)
	}
}

// buildBody builds the email body.
func (h *EmailHandler) buildBody(notification *Notification, setting *NotificationSetting) (string, error) {
	// Try to get a template for this event type
	h.mu.RLock()
	tmpl, ok := h.templates[string(notification.EventType)]
	h.mu.RUnlock()

	if ok {
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, notification); err != nil {
			return "", err
		}
		return buf.String(), nil
	}

	// Use default template
	return h.buildDefaultBody(notification)
}

// buildDefaultBody builds the default email body.
func (h *EmailHandler) buildDefaultBody(notification *Notification) (string, error) {
	var buf bytes.Buffer

	buf.WriteString("<!DOCTYPE html>\n<html>\n<head>\n")
	buf.WriteString("<style>\n")
	buf.WriteString("body { font-family: Arial, sans-serif; margin: 20px; }\n")
	buf.WriteString(".header { background: #2563eb; color: white; padding: 20px; border-radius: 8px 8px 0 0; }\n")
	buf.WriteString(".content { background: #f3f4f6; padding: 20px; border-radius: 0 0 8px 8px; }\n")
	buf.WriteString(".data { background: white; padding: 15px; margin-top: 15px; border-radius: 4px; }\n")
	buf.WriteString("pre { background: #1f2937; color: #f9fafb; padding: 15px; border-radius: 4px; overflow-x: auto; }\n")
	buf.WriteString(".footer { margin-top: 20px; font-size: 12px; color: #6b7280; }\n")
	buf.WriteString("</style>\n</head>\n<body>\n")

	buf.WriteString("<div class=\"header\">\n")
	buf.WriteString(fmt.Sprintf("<h2>%s Event</h2>\n", notification.EventType))
	buf.WriteString("</div>\n")

	buf.WriteString("<div class=\"content\">\n")
	buf.WriteString(fmt.Sprintf("<p><strong>Block Number:</strong> %d</p>\n", notification.Payload.BlockNumber))
	buf.WriteString(fmt.Sprintf("<p><strong>Block Hash:</strong> %s</p>\n", notification.Payload.BlockHash.Hex()))
	buf.WriteString(fmt.Sprintf("<p><strong>Timestamp:</strong> %s</p>\n", notification.Payload.Timestamp.UTC().Format(time.RFC3339)))

	buf.WriteString("<div class=\"data\">\n")
	buf.WriteString("<h3>Event Data</h3>\n")

	// Pretty print JSON data
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, notification.Payload.Data, "", "  "); err == nil {
		buf.WriteString("<pre>")
		buf.WriteString(template.HTMLEscapeString(prettyJSON.String()))
		buf.WriteString("</pre>\n")
	} else {
		buf.WriteString("<pre>")
		buf.WriteString(template.HTMLEscapeString(string(notification.Payload.Data)))
		buf.WriteString("</pre>\n")
	}

	buf.WriteString("</div>\n")
	buf.WriteString("</div>\n")

	buf.WriteString("<div class=\"footer\">\n")
	buf.WriteString(fmt.Sprintf("<p>Notification ID: %s</p>\n", notification.ID))
	buf.WriteString("<p>Generated by Indexer Notification Service</p>\n")
	buf.WriteString("</div>\n")

	buf.WriteString("</body>\n</html>")

	return buf.String(), nil
}

// buildMessage builds the MIME email message.
func (h *EmailHandler) buildMessage(to, cc []string, subject, body string) []byte {
	var msg bytes.Buffer

	fromName := h.config.FromName
	if fromName == "" {
		fromName = "Indexer"
	}

	// Headers
	msg.WriteString(fmt.Sprintf("From: %s <%s>\r\n", fromName, h.config.FromAddress))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(to, ", ")))
	if len(cc) > 0 {
		msg.WriteString(fmt.Sprintf("Cc: %s\r\n", strings.Join(cc, ", ")))
	}
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
	msg.WriteString("\r\n")

	// Body
	msg.WriteString(body)

	return msg.Bytes()
}

// sendMail sends the email via SMTP.
func (h *EmailHandler) sendMail(ctx context.Context, to, cc []string, msg []byte) error {
	addr := fmt.Sprintf("%s:%d", h.config.SMTPHost, h.config.SMTPPort)

	// Combine all recipients
	recipients := make([]string, 0, len(to)+len(cc))
	recipients = append(recipients, to...)
	recipients = append(recipients, cc...)

	var auth smtp.Auth
	if h.config.SMTPUsername != "" {
		auth = smtp.PlainAuth("", h.config.SMTPUsername, h.config.SMTPPassword, h.config.SMTPHost)
	}

	if h.config.UseTLS {
		return h.sendMailTLS(addr, auth, recipients, msg)
	}

	return smtp.SendMail(addr, auth, h.config.FromAddress, recipients, msg)
}

// sendMailTLS sends email using TLS.
func (h *EmailHandler) sendMailTLS(addr string, auth smtp.Auth, recipients []string, msg []byte) error {
	host := h.config.SMTPHost

	// Connect to SMTP server
	conn, err := tls.Dial("tcp", addr, &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	defer client.Close()

	// Authenticate
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
	}

	// Set sender
	if err := client.Mail(h.config.FromAddress); err != nil {
		return fmt.Errorf("failed to set sender: %w", err)
	}

	// Add recipients
	for _, rcpt := range recipients {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("failed to add recipient %s: %w", rcpt, err)
		}
	}

	// Send message body
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("failed to get data writer: %w", err)
	}

	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("failed to close data writer: %w", err)
	}

	return client.Quit()
}

// LoadTemplate loads an email template from string.
func (h *EmailHandler) LoadTemplate(name, templateStr string) error {
	tmpl, err := template.New(name).Parse(templateStr)
	if err != nil {
		return err
	}

	h.mu.Lock()
	h.templates[name] = tmpl
	h.mu.Unlock()

	return nil
}
