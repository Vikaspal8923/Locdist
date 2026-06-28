package app

import (
	"encoding/json"
	"sync"
	"time"
)

type Event struct {
	Type string    `json:"type"`
	Time time.Time `json:"time"`
	Data any       `json:"data,omitempty"`
}

type EventHub struct {
	mu          sync.Mutex
	nextID      int
	subscribers map[int]chan Event
}

func NewEventHub() *EventHub { return &EventHub{subscribers: make(map[int]chan Event)} }

func (h *EventHub) Publish(eventType string, data any) {
	event := Event{Type: eventType, Time: time.Now(), Data: data}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, subscriber := range h.subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
}

func (h *EventHub) Subscribe() (<-chan Event, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id := h.nextID
	h.nextID++
	channel := make(chan Event, 32)
	h.subscribers[id] = channel
	return channel, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if existing, ok := h.subscribers[id]; ok {
			delete(h.subscribers, id)
			close(existing)
		}
	}
}

func eventJSON(event Event) []byte { data, _ := json.Marshal(event); return data }
