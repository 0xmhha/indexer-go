package notifications

import (
	"net/url"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// Deliveries to one destination are capped (Config.DestinationRateLimit
// per second, 0 for no cap; Config.DestinationBurst at once), so a setting cannot turn
// the indexer into a source of floods against a server it names: every
// webhook to the same host, and every Slack message to the same incoming
// webhook URL (Slack's own limit is per URL; the host is Slack's for
// everyone), share one limit. A delivery over the limit is not attempted:
// it is put back for when the limit allows it, without counting as an
// attempt, so a busy destination delays its own notifications only.

// DefaultDestinationRateLimit and DefaultDestinationBurst are the
// defaults (DefaultConfig); a burst that is not positive is the default.
const (
	DefaultDestinationRateLimit = 10
	DefaultDestinationBurst     = 20
)

// limiterIdle is how long an unused destination's limiter is kept.
const limiterIdle = 10 * time.Minute

type destinationLimiter struct {
	lim      *rate.Limiter
	lastUsed time.Time
}

// destinationLimits holds one limiter per destination; nil when deliveries
// are not limited.
type destinationLimits struct {
	mu      sync.Mutex
	every   rate.Limit
	burst   int
	byKey   map[string]*destinationLimiter
	cleaned time.Time
}

func newDestinationLimits(perSecond float64, burst int) *destinationLimits {
	if perSecond <= 0 {
		return nil
	}
	if burst <= 0 {
		burst = DefaultDestinationBurst
	}
	return &destinationLimits{every: rate.Limit(perSecond), burst: burst, byKey: map[string]*destinationLimiter{}}
}

// destinationKey is the destination a setting's notifications share a
// limit with, "" for one that is not limited.
func destinationKey(t NotificationType, d Destination) string {
	switch t {
	case NotificationTypeWebhook:
		u, err := url.Parse(d.WebhookURL)
		if err != nil || u.Host == "" {
			return ""
		}
		return "webhook/" + strings.ToLower(u.Host)
	case NotificationTypeSlack:
		if d.SlackWebhookURL == "" {
			return ""
		}
		return "slack/" + d.SlackWebhookURL
	}
	return ""
}

// wait reports how long a delivery to key must wait; 0 takes a token now.
func (d *destinationLimits) wait(key string, now time.Time) time.Duration {
	if d == nil || key == "" {
		return 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if now.Sub(d.cleaned) > limiterIdle {
		for k, l := range d.byKey {
			if now.Sub(l.lastUsed) > limiterIdle {
				delete(d.byKey, k)
			}
		}
		d.cleaned = now
	}
	l := d.byKey[key]
	if l == nil {
		l = &destinationLimiter{lim: rate.NewLimiter(d.every, d.burst)}
		d.byKey[key] = l
	}
	l.lastUsed = now
	r := l.lim.ReserveN(now, 1)
	if delay := r.DelayFrom(now); delay > 0 {
		r.CancelAt(now)
		return delay
	}
	return 0
}

// deferDelivery puts a notification back for after wait without counting
// an attempt: the retry processor queues it again when it is due.
func (s *NotificationService) deferDelivery(n *Notification, wait time.Duration) {
	next := time.Now().Add(wait)
	n.NextRetry = &next
	n.Status = DeliveryStatusRetrying
	if err := s.storage.UpdateNotification(s.workCtx, n); err != nil {
		s.logger.Warn("failed to defer a rate-limited notification", zap.String("notification_id", n.ID), zap.Error(err))
	}
	metricDeferred.WithLabelValues(string(n.Type)).Inc()
	s.logger.Debug("notification deferred by its destination's rate limit",
		zap.String("notification_id", n.ID), zap.Duration("wait", wait))
}
