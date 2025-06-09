package signaling

import (
	"encoding/json"
	"fmt"
)

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
	Type string          `json:"type"` // "offer", "answer", "ice-candidate", "direct", etc.
	From string          `json:"from"`
	To   string          `json:"to"`
	Data json.RawMessage `json:"data"`
}

// Специализированные структуры для WebRTC сигналов
type RTCOffer struct {
	SDP string `json:"sdp"`
}

type RTCAnswer struct {
	SDP string `json:"sdp"`
}

type RTCIceCandidate struct {
	Candidate     string `json:"candidate"`
	SDPMid        string `json:"sdpMid"`
	SDPMLineIndex int    `json:"sdpMLineIndex"`
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
			h.clientsById[client.id] = client

			// Отправляем новому клиенту список всех пользователей
			userList := make([]string, 0, len(h.clientsById))
			for id := range h.clientsById {
				userList = append(userList, id)
			}

			userListMsg := Message{
				Type: "user-list",
				Data: json.RawMessage(fmt.Sprintf(`{"users":%s}`, marshalJSON(userList))),
			}

			msgBytes, _ := json.Marshal(userListMsg)
			client.send <- msgBytes

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
					delete(h.clientsById, client.id)
				}
			}

		case message := <-h.directMessage:
			// Обработка прямых сообщений, включая WebRTC сигналы
			if targetClient, ok := h.clientsById[message.To]; ok {
				messageBytes, err := json.Marshal(message)
				if err == nil {
					select {
					case targetClient.send <- messageBytes:
					default:
						close(targetClient.send)
						delete(h.clients, targetClient)
						delete(h.clientsById, targetClient.id)
					}
				}
			}
		}
	}
}

func marshalJSON(v interface{}) string {
	bytes, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(bytes)
}
