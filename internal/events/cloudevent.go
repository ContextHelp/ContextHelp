package events

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Event represents a CloudEvents v1.0 specification event.
type Event struct {
	ID              string          `json:"id"`
	Source          string          `json:"source"`
	SpecVersion     string          `json:"specversion"`
	Type            string          `json:"type"`
	DataContentType string          `json:"datacontenttype,omitempty"`
	Time            time.Time       `json:"time"`
	Data            json.RawMessage `json:"data,omitempty"`
}

// NewEvent creates a new CloudEvent.
func NewEvent(source, eventType string, data any) (Event, error) {
	var rawData json.RawMessage
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return Event{}, err
		}
		rawData = b
	}

	return Event{
		ID:              uuid.New().String(),
		Source:          source,
		SpecVersion:     "1.0",
		Type:            eventType,
		DataContentType: "application/json",
		Time:            time.Now().UTC(),
		Data:            rawData,
	}, nil
}
