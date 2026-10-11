package notifications

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// With WebhookConfig.VerifyDestinations, a webhook URL is accepted only
// when it answers a challenge when the setting is created, or when an
// update changes the URL: the indexer POSTs
// {"type":"url_verification","challenge":"<random>"} (signed like any
// delivery when the setting has a secret, header X-Event-Type:
// url_verification) and the URL must answer 2xx with the challenge, as the
// body or as {"challenge":"<random>"}. A server that never asked for
// notifications does not echo the challenge, so a setting cannot name it
// to have the indexer send it requests.

// destinationVerifier is a handler that checks a setting's destination
// with the destination itself before the setting is stored.
type destinationVerifier interface {
	VerifyDestination(ctx context.Context, setting *NotificationSetting) error
}

// challengePayload is the body of a challenge request.
type challengePayload struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
}

// VerifyDestination implements destinationVerifier.
func (h *WebhookHandler) VerifyDestination(ctx context.Context, setting *NotificationSetting) error {
	if !h.config.VerifyDestinations {
		return nil
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	challenge := hex.EncodeToString(nonce)
	body, err := json.Marshal(challengePayload{Type: "url_verification", Challenge: challenge})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, setting.Destination.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook URL: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Indexer-Webhook/1.0")
	req.Header.Set("X-Event-Type", "url_verification")
	for key, value := range setting.Destination.WebhookHeaders {
		req.Header.Set(key, value)
	}
	if setting.Destination.WebhookSecret != "" {
		header := h.config.SignatureHeader
		if header == "" {
			header = "X-Signature-256"
		}
		req.Header.Set(header, "sha256="+h.computeSignature(body, setting.Destination.WebhookSecret))
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook URL did not answer the challenge: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook URL answered the challenge with status %d", resp.StatusCode)
	}
	if strings.TrimSpace(string(answer)) == challenge {
		return nil
	}
	var echoed challengePayload
	if json.Unmarshal(answer, &echoed) == nil && echoed.Challenge == challenge {
		return nil
	}
	return fmt.Errorf("webhook URL did not echo the challenge (answer it with the challenge value; see docs/CONFIG.md)")
}

// verifyDestinations checks each channel's destination that is new: all
// of them for a new setting, and for an update those whose destination
// changed (existing is the stored setting).
func (s *NotificationService) verifyDestinations(ctx context.Context, setting, existing *NotificationSetting) error {
	for _, channel := range setting.channels() {
		v, ok := s.handlers[channel].(destinationVerifier)
		if !ok {
			continue
		}
		if existing != nil && existing.hasChannel(channel) && sameDestination(channel, existing.Destination, setting.Destination) {
			continue
		}
		if err := v.VerifyDestination(ctx, setting); err != nil {
			return fmt.Errorf("invalid setting: %s: %w", channel, err)
		}
	}
	return nil
}

// sameDestination reports whether a channel's destination is unchanged.
func sameDestination(channel NotificationType, a, b Destination) bool {
	switch channel {
	case NotificationTypeWebhook:
		return a.WebhookURL == b.WebhookURL
	case NotificationTypeSlack:
		return a.SlackWebhookURL == b.SlackWebhookURL
	}
	return true
}
