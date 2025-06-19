package main

import "sync"

// Broadcaster struct manages subscribers and broadcasting events to them.
type Broadcaster struct {
	subscribers map[chan string]bool
	mu          sync.Mutex
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		subscribers: make(map[chan string]bool),
	}
}

type Subscriber chan string

// AddSubscriber adds a new subscriber channel to the broadcaster.
func (b *Broadcaster) AddSubscriber() Subscriber {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan string, 1) // Buffered channel for non-blocking
	b.subscribers[ch] = true
	return ch
}

// Remove removes a subscriber channel from the broadcaster.
func (b *Broadcaster) Remove(ch Subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.subscribers, ch)
	close(ch)
}

// Broadcast sends the message to all active subscribers.
func (b *Broadcaster) Broadcast(message string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subscribers {
		select {
		case ch <- message:
		default:
			// if the subscriber channel is blocked, skip it
		}
	}
}
