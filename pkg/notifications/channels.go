package notifications

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// A setting sends its notifications to its Type and to each of its further
// Channels (the subscriptions design's channels: [stream, webhook]), all
// from one filter, condition and payload. The notification of the first
// channel keeps the id it would have alone; another channel's is derived
// from it and the channel, so each is created once per channel.

// channels are the setting's channels, its Type first.
func (st *NotificationSetting) channels() []NotificationType {
	out := make([]NotificationType, 0, 1+len(st.Channels))
	out = append(out, st.Type)
	for _, c := range st.Channels {
		if c != st.Type {
			out = append(out, c)
		}
	}
	return out
}

// hasChannel reports whether the setting sends to channel t.
func (st *NotificationSetting) hasChannel(t NotificationType) bool {
	for _, c := range st.channels() {
		if c == t {
			return true
		}
	}
	return false
}

// validateSetting checks a setting being created or updated: every channel
// is known and accepts the setting (its destination), each named once, and
// the filter, delivery and expressions are well-formed.
func (s *NotificationService) validateSetting(setting *NotificationSetting) error {
	seen := map[NotificationType]bool{}
	for _, c := range append([]NotificationType{setting.Type}, setting.Channels...) {
		if seen[c] {
			return fmt.Errorf("invalid setting: channel %s is named twice", c)
		}
		seen[c] = true
		handler, ok := s.handlers[c]
		if !ok {
			return fmt.Errorf("unsupported notification type: %s", c)
		}
		if err := handler.Validate(setting); err != nil {
			return fmt.Errorf("invalid setting: %s: %w", c, err)
		}
	}
	if err := validateFilter(setting.Filter); err != nil {
		return fmt.Errorf("invalid setting: %w", err)
	}
	if err := validateDelivery(setting.Delivery); err != nil {
		return fmt.Errorf("invalid setting: %w", err)
	}
	if err := validateExpressions(setting); err != nil {
		return fmt.Errorf("invalid setting: %w", err)
	}
	return nil
}

// channelID is the id of a notification's copy for another channel.
func channelID(id string, channel NotificationType) string {
	return uuid.NewSHA1(notificationIDSpace, []byte(id+"/channel/"+string(channel))).String()
}

// sendChannels sends a created notification to each of the setting's
// channels: a stream is sent to the owner's connections; other channels
// are stored (unless stored already, when the event is evaluated again)
// and queued.
func (s *NotificationService) sendChannels(ctx context.Context, setting *NotificationSetting, n *Notification, dedupe bool) error {
	var errs []error
	channels := setting.channels()
	// A stream first: storing for the other channels must not delay it.
	order := make([]int, 0, len(channels))
	for i, channel := range channels {
		if channel == NotificationTypeStream {
			order = append([]int{i}, order...)
		} else {
			order = append(order, i)
		}
	}
	for _, i := range order {
		channel := channels[i]
		c := *n
		c.Type = channel
		if i > 0 {
			c.ID = channelID(n.ID, channel)
		}
		if channel == NotificationTypeStream {
			// Not stored: sent to the owner's connections now or never.
			c.Status = DeliveryStatusSent
			if _, err := s.streams.sendJSON(setting.Owner, &StreamMessage{Type: StreamNotification, Notification: &c}); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		if dedupe {
			existing, err := s.storage.GetNotification(ctx, c.ID)
			if err != nil {
				return err
			}
			if existing != nil {
				continue
			}
		}
		if err := s.storage.SaveNotification(ctx, &c); err != nil {
			return err
		}
		s.enqueueNotification(&c)
	}
	return errors.Join(errs...)
}
