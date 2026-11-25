package event

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Type string

const (
	ViewStarted  Type = "VIEW_STARTED"
	ViewFinished Type = "VIEW_FINISHED"
	ViewPaused   Type = "VIEW_PAUSED"
	ViewResumed  Type = "VIEW_RESUMED"
	Liked        Type = "LIKED"
	Searched     Type = "SEARCHED"
)

type Device string

const (
	Mobile  Device = "MOBILE"
	Desktop Device = "DESKTOP"
	TV      Device = "TV"
	Tablet  Device = "TABLET"
)

type Event struct {
	EventID         string    `json:"event_id" avro:"event_id"`
	UserID          string    `json:"user_id" avro:"user_id"`
	MovieID         string    `json:"movie_id" avro:"movie_id"`
	EventType       Type      `json:"event_type" avro:"event_type"`
	Timestamp       time.Time `json:"timestamp" avro:"timestamp"`
	DeviceType      Device    `json:"device_type" avro:"device_type"`
	SessionID       string    `json:"session_id" avro:"session_id"`
	ProgressSeconds *int32    `json:"progress_seconds,omitempty" avro:"progress_seconds"`
}

func (e *Event) SetDefaults(now time.Time) {
	e.EventID = strings.TrimSpace(e.EventID)
	if e.EventID == "" {
		e.EventID = uuid.NewString()
	} else if id, err := uuid.Parse(e.EventID); err == nil {
		e.EventID = id.String()
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = now.UTC()
	} else {
		e.Timestamp = e.Timestamp.UTC()
	}
	e.UserID = strings.TrimSpace(e.UserID)
	e.MovieID = strings.TrimSpace(e.MovieID)
	e.SessionID = strings.TrimSpace(e.SessionID)
	e.EventType = Type(strings.ToUpper(strings.TrimSpace(string(e.EventType))))
	e.DeviceType = Device(strings.ToUpper(strings.TrimSpace(string(e.DeviceType))))
}

func (e Event) Validate() error {
	if _, err := uuid.Parse(e.EventID); err != nil {
		return errors.New("event_id must be a UUID")
	}
	for _, field := range []struct{ name, value string }{
		{"user_id", e.UserID}, {"movie_id", e.MovieID}, {"session_id", e.SessionID},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s is required", field.name)
		}
		if len(field.value) > 1024 {
			return fmt.Errorf("%s cannot exceed 1024 bytes", field.name)
		}
	}
	if !validType(e.EventType) {
		return fmt.Errorf("unsupported event_type %q", e.EventType)
	}
	if e.Timestamp.IsZero() {
		return errors.New("timestamp is required")
	}
	if year := e.Timestamp.UTC().Year(); year < 1970 || year >= 2100 {
		return errors.New("timestamp must be in [1970, 2100) for ClickHouse Date compatibility")
	}
	if !validDevice(e.DeviceType) {
		return fmt.Errorf("unsupported device_type %q", e.DeviceType)
	}
	if isViewingEvent(e.EventType) && e.ProgressSeconds == nil {
		return errors.New("progress_seconds is required for viewing events")
	}
	if e.ProgressSeconds != nil && *e.ProgressSeconds < 0 {
		return errors.New("progress_seconds cannot be negative")
	}
	return nil
}

func validType(value Type) bool {
	switch value {
	case ViewStarted, ViewFinished, ViewPaused, ViewResumed, Liked, Searched:
		return true
	default:
		return false
	}
}

func validDevice(value Device) bool {
	switch value {
	case Mobile, Desktop, TV, Tablet:
		return true
	default:
		return false
	}
}

func isViewingEvent(value Type) bool {
	switch value {
	case ViewStarted, ViewFinished, ViewPaused, ViewResumed:
		return true
	default:
		return false
	}
}
