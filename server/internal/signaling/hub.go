package signaling

import "encoding/json"

type Hub struct {
	// Registered clients.
	clients map[*Client]bool

	// Map clients by ID for direct messaging
	clientsById map[string]*Client

	// Inbound messages from the clients.
	broadcast chan []byte

	// Direct messages between clients
	directMessage chan Message

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client
}

type Message struct {
	Type string          `json:"type"`
	From string          `json:"from"`
	To   string          `json:"to"`
	Data json.RawMessage `json:"data"`
}

func NewHub() *Hub {
	return &Hub{
		broadcast:     make(chan []byte),
		directMessage: make(chan Message),
		register:      make(chan *Client),
		unregister:    make(chan *Client),
		clients:       make(map[*Client]bool),
		clientsById:   make(map[string]*Client),
	}
}
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			if client.id != "" {
				h.clientsById[client.id] = client
			}
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				delete(h.clientsById, client.id)
				close(client.send)
			}
		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
		case msg := <-h.directMessage:
			if client, ok := h.clientsById[msg.To]; ok {
				msgBytes, _ := json.Marshal(msg)
				select {
				case client.send <- msgBytes:
				default:
					close(client.send)
					delete(h.clients, client)
					delete(h.clientsById, client.id)
				}
			}
		}
	}
}
