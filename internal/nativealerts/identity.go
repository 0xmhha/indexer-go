package nativealerts

import (
	"crypto/sha256"
	"encoding/json"
)

func digest(v any) ID {
	data, err := json.Marshal(v)
	if err != nil {
		return ID{}
	}
	return ID(sha256.Sum256(data))
}
func ruleID(r Rule) ID { return digest(r) }

func transitionID(s State) ID {
	return digest(struct {
		Key         Key
		Incarnation Incarnation
		Revision    uint64
		Level       Level
	}{s.Key, s.Incarnation, s.Revision, s.Active})
}
func eventFor(s State) Event {
	t := transitionID(s)
	return Event{ID: digest(struct {
		Transition ID
		Reminder   uint64
	}{t, s.Reminder}), Transition: t, Key: s.Key, Level: s.Active, Revision: s.Revision, Reminder: s.Reminder, OccurredAt: s.TransitionAt, NoticeAt: s.LastNoticeAt}
}
