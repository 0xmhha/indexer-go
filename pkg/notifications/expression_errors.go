package notifications

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/events"
)

// maxExpressionErrors is how many consecutive evaluation errors disable a
// setting.
const maxExpressionErrors = 10

// StreamError tells a setting's owner that its expressions failed on an
// event (StreamMessage.SettingID, Block, Error); Disabled is set on the
// error that disabled the setting.
const StreamError = "error"

var (
	expressionEvaluations = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "indexer_notification_expression_evaluations_total",
		Help: "Evaluations of notification setting expressions by result: notify, skip (condition false) or error",
	}, []string{"result"})
	expressionSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "indexer_notification_expression_seconds",
		Help:    "Time to evaluate one setting's expressions for one event",
		Buckets: []float64{1e-6, 5e-6, 1e-5, 5e-5, 1e-4, 5e-4, 1e-3, 1e-2},
	})
	settingsDisabledByErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "indexer_notification_settings_disabled_total",
		Help: "Notification settings disabled after consecutive expression errors",
	})
)

// SetChainID sets the indexed chain's id: notifications carry it
// (payload.chain_id) and expressions read it (chain.id).
func (s *NotificationService) SetChainID(id uint64) { s.chainID.Store(id) }

// applyExpressions evaluates a setting's condition and payload for an
// event: whether to notify, with the payload's value set on n. An
// evaluation error notifies nothing and is reported (expressionFailed);
// it is not returned, so one setting's expression never stops the others
// or holds back the change stream.
func (s *NotificationService) applyExpressions(ctx context.Context, setting *NotificationSetting, event events.Event, n *Notification) (bool, error) {
	if setting.Condition == "" && setting.Payload == "" {
		return true, nil
	}
	if _, reorg := event.(*events.ReorgEvent); reorg {
		return true, nil // a reorganization is notified as it is
	}
	start := time.Now()
	notify, result, err := s.evaluateExpressions(setting, event, n)
	expressionSeconds.Observe(time.Since(start).Seconds())
	if err != nil {
		expressionEvaluations.WithLabelValues("error").Inc()
		s.expressionFailed(ctx, setting, n.Payload.BlockNumber, err)
		return false, nil
	}
	s.exprMu.Lock()
	delete(s.exprErrors, setting.ID)
	s.exprMu.Unlock()
	if !notify {
		expressionEvaluations.WithLabelValues("skip").Inc()
		return false, nil
	}
	expressionEvaluations.WithLabelValues("notify").Inc()
	n.Payload.Result = result
	return true, nil
}

func (s *NotificationService) evaluateExpressions(setting *NotificationSetting, event events.Event, n *Notification) (bool, []byte, error) {
	x, err := compileExpressions(setting)
	if err != nil || x == nil {
		return x == nil && err == nil, nil, err
	}
	return x.evaluate(x.activation(s.chainID.Load(), event, n.Payload.Decoded))
}

// expressionFailed counts a setting's evaluation error, tells its owner
// over the stream, and disables the setting after maxExpressionErrors
// consecutive errors.
func (s *NotificationService) expressionFailed(ctx context.Context, setting *NotificationSetting, block uint64, evalErr error) {
	s.exprMu.Lock()
	s.exprErrors[setting.ID]++
	count := s.exprErrors[setting.ID]
	disable := count >= maxExpressionErrors
	if disable {
		delete(s.exprErrors, setting.ID)
	}
	s.exprMu.Unlock()

	s.logger.Warn("notification expression failed",
		zap.String("setting_id", setting.ID), zap.Uint64("block", block), zap.Int("consecutive", count), zap.Error(evalErr))
	if disable {
		if err := s.disableSetting(ctx, setting.ID); err != nil {
			s.logger.Error("failed to disable a setting after expression errors", zap.String("setting_id", setting.ID), zap.Error(err))
			disable = false
		} else {
			settingsDisabledByErrors.Inc()
			s.logger.Warn("notification setting disabled after consecutive expression errors",
				zap.String("setting_id", setting.ID), zap.Int("errors", maxExpressionErrors))
		}
	}
	if setting.Owner != "" {
		_, _ = s.streams.sendJSON(setting.Owner, &StreamMessage{Type: StreamError, SettingID: setting.ID, Block: block,
			Error: evalErr.Error(), Disabled: disable})
	}
}

// disableSetting stores a setting as disabled.
func (s *NotificationService) disableSetting(ctx context.Context, id string) error {
	st, err := s.storage.GetSetting(ctx, id)
	if err != nil {
		return err
	}
	if st == nil {
		return nil
	}
	st.Enabled = false
	st.UpdatedAt = time.Now()
	if err := s.storage.SaveSetting(ctx, st); err != nil {
		return err
	}
	s.mu.Lock()
	s.settings[id] = st
	s.countFast()
	s.mu.Unlock()
	return nil
}
