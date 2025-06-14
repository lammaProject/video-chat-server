package signaling

import (
	"log"
)

type Hub struct {
	roomID     string
	clients    map[string]*Client
	register   chan *Client
	unregister chan *Client
	message    chan *Message
	messages   []Message
	videochat  chan *VideoChatMessage
	manager    *RoomManager
}

type AnswerType struct {
	Messages []Message       `json:"messages,omitempty"`
	Type     MessageType     `json:"type"`
	Clients  map[string]bool `json:"clients,omitempty"`
}

type AnswerVideoChatType struct {
	Type MessageType      `json:"type"`
	Data VideoChatMessage `json:"data"`
}

func NewHub() *Hub {
	return &Hub{
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[string]*Client),
		messages:   make([]Message, 0),
		message:    make(chan *Message),
		videochat:  make(chan *VideoChatMessage),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			log.Printf("Client %s registered", client.id)
			h.clients[client.id] = client

			// Уведомляем нового клиента о существующих участниках
			existingClients := make(map[string]bool)
			for id := range h.clients {
				if id != client.id {
					existingClients[id] = true
				}
			}

			// Отправляем новому клиенту список существующих участников
			newAnswer := AnswerType{
				Type:     "register",
				Messages: h.messages,
				Clients:  existingClients,
			}
			client.send <- newAnswer

			// Уведомляем всех остальных о новом участнике
			newUserMessage := AnswerType{
				Type:    "new-user",
				Clients: map[string]bool{client.id: true},
			}
			for id, c := range h.clients {
				if id != client.id {
					select {
					case c.send <- newUserMessage:
					default:
						close(c.send)
						delete(h.clients, id)
					}
				}
			}

		case msg := <-h.message:
			h.messages = append(h.messages, *msg)
			newAnswer := AnswerType{
				Type:     "chat",
				Messages: h.messages,
				Clients:  h.getActiveClients(),
			}
			h.broadcast(newAnswer)

		case client := <-h.unregister:
			if _, ok := h.clients[client.id]; ok {
				log.Printf("Client %s unregistered", client.id)
				delete(h.clients, client.id)
				close(client.send)

				// Уведомляем остальных об отключении
				userLeftMessage := AnswerType{
					Type:    "user-left",
					Clients: map[string]bool{client.id: false},
				}
				h.broadcast(userLeftMessage)

				if len(h.clients) == 0 && h.manager != nil {
					h.manager.RemoveRoomIfEmpty(h.roomID)
				}
			}

		case videoMsg := <-h.videochat:
			log.Printf("Video message from %s to %s", videoMsg.From, videoMsg.To)

			// Находим получателя
			if targetClient, ok := h.clients[videoMsg.To]; ok {
				answer := AnswerVideoChatType{
					Type: "videochat",
					Data: *videoMsg,
				}

				select {
				case targetClient.send <- answer:
				default:
					close(targetClient.send)
					delete(h.clients, videoMsg.To)
				}
			} else {
				log.Printf("Target client %s not found", videoMsg.To)
			}
		}
	}
}

func (h *Hub) broadcast(message interface{}) {
	for id, client := range h.clients {
		select {
		case client.send <- message:
		default:
			close(client.send)
			delete(h.clients, id)
		}
	}
}

func (h *Hub) getActiveClients() map[string]bool {
	result := make(map[string]bool)
	for id := range h.clients {
		result[id] = true
	}
	return result
}
