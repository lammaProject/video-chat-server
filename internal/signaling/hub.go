package signaling

import (
	"fmt"
	"log"
)

type AnswerTypes string

const (
	AnswerTypeChat     MessageType = "chat"
	AnswerTypeRegister MessageType = "register"
)

type Hub struct {
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
	message    chan *Message
	messages   []Message
	answer     AnswerType
	videochat  chan *VideoChatMessage
}

type AnswerType struct {
	Messages []Message       `json:"messages"`
	Type     MessageType     `json:"type"`
	Clients  map[string]bool `json:"clients"`
}

type AnswerVideoChatType struct {
	Type MessageType      `json:"type"`
	Data VideoChatMessage `json:"data"`
}

func NewHub() *Hub {
	return &Hub{
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
		messages:   make([]Message, 0),
		message:    make(chan *Message),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			println("REGISTER", client.id)
			h.clients[client] = true

			newMessage := Message{
				Text: fmt.Sprintf("%s подключился", client.id),
				From: client.id,
				To:   "chat",
			}

			h.messages = append(h.messages, newMessage)

			newAnswer := AnswerType{
				Type:     "register",
				Messages: h.messages,
				Clients:  convertClients(h.clients),
			}

			for client := range h.clients {
				client.send <- newAnswer
			}
		case msg := <-h.message:
			h.messages = append(h.messages, *msg)

			newAnswer := AnswerType{
				Type:     "chat",
				Messages: h.messages,
				Clients:  convertClients(h.clients),
			}

			for client := range h.clients {
				select {
				case client.send <- newAnswer:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
		case unregister := <-h.unregister:
			newMessage := Message{
				Text: fmt.Sprintf("Покинул чат%s:", unregister.id),
				From: unregister.id,
				To:   "chat",
			}
			h.messages = append(h.messages, newMessage)
			delete(h.clients, unregister)
			newAnswer := AnswerType{
				Type:     "chat",
				Messages: h.messages,
				Clients:  convertClients(h.clients),
			}
			for client := range h.clients {
				select {
				case client.send <- newAnswer:
				}
			}
		case videoMsg := <-h.videochat:
			log.Printf("Получено сырое сообщение: %v", videoMsg)
			if videoMsg.Offer == nil && videoMsg.Answer == nil && videoMsg.IceCandidate == nil {
				log.Println("Invalid message received")
				continue
			}

			answer := AnswerVideoChatType{
				Type: "videochat", // установите тип сообщения
				Data: *videoMsg,   // используем существующее видео-сообщение как данные
			}

			for client := range h.clients {
				if client.id != videoMsg.UserId {
					if err := client.conn.WriteJSON(answer); err != nil {
						log.Printf("<UNK> <UNK> <UNK>: %v", err)
						client.conn.Close()
						delete(h.clients, client)
					}
				}
			}
		}
	}
}

func convertClients(clients map[*Client]bool) map[string]bool {
	result := make(map[string]bool)
	for client, active := range clients {
		result[client.id] = active
	}
	return result
}
