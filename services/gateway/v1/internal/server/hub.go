package server

import "sync"

type hub struct {
	mu       sync.RWMutex
	channels map[int64]map[*client]struct{}
	users    map[int64]map[*client]struct{}
	sessions map[string]*client
}

func newHub() *hub {
	return &hub{
		channels: make(map[int64]map[*client]struct{}),
		users:    make(map[int64]map[*client]struct{}),
		sessions: make(map[string]*client),
	}
}

func (h *hub) add(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	sessions := h.users[c.userID]
	if sessions == nil {
		sessions = make(map[*client]struct{})
		h.users[c.userID] = sessions
	}
	sessions[c] = struct{}{}
	h.sessions[c.gatewaySessionID] = c
}

func (h *hub) subscribe(c *client, channelIDs []int64) []int64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	newlyActive := make([]int64, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		if channelID == 0 {
			continue
		}
		subscribers := h.channels[channelID]
		if subscribers == nil {
			subscribers = make(map[*client]struct{})
			h.channels[channelID] = subscribers
			newlyActive = append(newlyActive, channelID)
		}
		subscribers[c] = struct{}{}
		c.channels[channelID] = struct{}{}
	}
	return newlyActive
}

func (h *hub) unsubscribe(c *client, channelID int64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, subscribed := c.channels[channelID]; !subscribed {
		return false
	}
	delete(c.channels, channelID)
	subscribers := h.channels[channelID]
	delete(subscribers, c)
	if len(subscribers) == 0 {
		delete(h.channels, channelID)
		return true
	}
	return false
}

type channelSubscription struct {
	client    *client
	channelID int64
}

func (h *hub) channelSubscriptions() []channelSubscription {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var subscriptions []channelSubscription
	for channelID, clients := range h.channels {
		for c := range clients {
			subscriptions = append(subscriptions, channelSubscription{client: c, channelID: channelID})
		}
	}
	return subscriptions
}

func (h *hub) remove(c *client) []int64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	if sessions := h.users[c.userID]; sessions != nil {
		delete(sessions, c)
		if len(sessions) == 0 {
			delete(h.users, c.userID)
		}
	}
	delete(h.sessions, c.gatewaySessionID)

	inactive := make([]int64, 0, len(c.channels))
	for channelID := range c.channels {
		subscribers := h.channels[channelID]
		delete(subscribers, c)
		if len(subscribers) == 0 {
			delete(h.channels, channelID)
			inactive = append(inactive, channelID)
		}
	}
	return inactive
}

func (h *hub) activeChannels() []int64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	channels := make([]int64, 0, len(h.channels))
	for channelID := range h.channels {
		channels = append(channels, channelID)
	}
	return channels
}

func (h *hub) channelClients(channelID int64) []*client {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return clientsFromSet(h.channels[channelID])
}

func (h *hub) userClients(userID int64) []*client {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return clientsFromSet(h.users[userID])
}

func (h *hub) sessionClient(sessionID string) *client {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return h.sessions[sessionID]
}

func clientsFromSet(set map[*client]struct{}) []*client {
	clients := make([]*client, 0, len(set))
	for c := range set {
		clients = append(clients, c)
	}
	return clients
}
