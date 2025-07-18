package routes

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

var (
	chatHubs = make(map[string]*ClientHub)
	hubMutex sync.RWMutex
)

type TypeChat string

const (
	TypeChatPrivate TypeChat = "private"
	TypeChatGroup   TypeChat = "group"
)

type CreateChatRequest struct {
	FriendId string   `json:"friend_id"`
	TypeChat TypeChat `json:"type_chat"`
	Name     string   `json:"name"`
}

type Chat struct {
	Id   string   `json:"id"`
	Type TypeChat `json:"type"`
	Name string   `json:"name"`
}

type ChatParticipants struct {
	ChatId   string `json:"chat_id"`
	UserId   string `json:"user_id"`
	JoinedAt string `json:"joined_at"`
}

// CreateChatResponse — что мы возвращаем клиенту
type CreateChatResponse struct {
	ChatID  string `json:"chat_id"`
	Created bool   `json:"created"`
	Message string `json:"message,omitempty"`
}

type ClientChat struct {
	conn   *websocket.Conn
	chatId string
	userId string
	send   chan []byte
	hub    *ClientHub
}

type ClientHub struct {
	clientsChat map[*ClientChat]bool
	register    chan *ClientChat
	unregister  chan *ClientChat
	broadcast   chan []byte
	db          *sql.DB
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// CreateChat создает новый чат или присоединяет к существующему
// @Summary Создание чата
// @Description Создает новый чат (приватный или групповой) или присоединяет пользователя к существующему приватному чату
// @Tags chats
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CreateChatRequest true "Данные для создания чата"
// @Success 200 {object} CreateChatResponse "Чат успешно создан или пользователь присоединен к существующему"
// @Router /auth/chats [post]
func (h *Handler) CreateChat(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value("user_id").(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req CreateChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// Валидация
	if req.TypeChat == "" || req.Name == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// Дополнительная валидация для приватного чата
	if req.TypeChat == "private" && req.FriendId == "" {
		http.Error(w, "Friend ID is required for private chat", http.StatusBadRequest)
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		log.Printf("CreateChat: tx begin error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// Логика для приватного чата
	if req.TypeChat == "private" {
		// 1. Проверяем дружбу между пользователями
		var status string
		err = tx.QueryRow(`
			SELECT status FROM friendship 
			WHERE (user_id = $1 AND friend_id = $2) 
			   OR (user_id = $2 AND friend_id = $1)
			AND status = 'accepted'
		`, userID, req.FriendId).Scan(&status)

		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, "Friendship not found or not accepted", http.StatusBadRequest)
				return
			}
			log.Printf("CreateChat: friendship check error: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// 2. Проверяем существование приватного чата между этими пользователями
		var existingChatID string
		err = tx.QueryRow(`
			SELECT c.id FROM chats c
			JOIN chat_participants cp1 ON c.id = cp1.chat_id
			JOIN chat_participants cp2 ON c.id = cp2.chat_id
			WHERE c.type = 'private' 
			AND cp1.user_id = $1 
			AND cp2.user_id = $2
			AND cp1.user_id != cp2.user_id
		`, userID, req.FriendId).Scan(&existingChatID)

		if err != nil && err != sql.ErrNoRows {
			log.Printf("CreateChat: existing chat check error: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// 3. Если чат существует, проверяем участие текущего пользователя
		if existingChatID != "" {
			var participantExists bool
			err = tx.QueryRow(`
				SELECT EXISTS(SELECT 1 FROM chat_participants WHERE chat_id = $1 AND user_id = $2)
			`, existingChatID, userID).Scan(&participantExists)

			if err != nil {
				log.Printf("CreateChat: participant check error: %v", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			if !participantExists {
				// Добавляем пользователя в существующий чат
				_, err = tx.Exec(`
					INSERT INTO chat_participants (chat_id, user_id, joined_at)
					VALUES ($1, $2, NOW())
				`, existingChatID, userID)

				if err != nil {
					log.Printf("CreateChat: add participant error: %v", err)
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
					return
				}
			}

			// Коммитим транзакцию
			if err = tx.Commit(); err != nil {
				log.Printf("CreateChat: commit error: %v", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			// Возвращаем существующий чат
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"chat_id": existingChatID,
				"message": "Joined existing chat",
			})
			return
		}
	}

	// 4. Создаем новый чат с сгенерированным ID
	chatID := generateChatID()

	_, err = tx.Exec(`
		INSERT INTO chats (id, type, name)
		VALUES ($1, $2, $3)
	`, chatID, req.TypeChat, req.Name)

	if err != nil {
		log.Printf("CreateChat: create chat error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// 5. Добавляем создателя чата в участники
	_, err = tx.Exec(`
		INSERT INTO chat_participants (chat_id, user_id, joined_at)
		VALUES ($1, $2, NOW())
	`, chatID, userID)

	if err != nil {
		log.Printf("CreateChat: add creator error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// 6. Для приватного чата добавляем друга
	if req.TypeChat == "private" {
		_, err = tx.Exec(`
			INSERT INTO chat_participants (chat_id, user_id, joined_at)
			VALUES ($1, $2, NOW())
		`, chatID, req.FriendId)

		if err != nil {
			log.Printf("CreateChat: add friend error: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
	}

	// Коммитим транзакцию
	if err = tx.Commit(); err != nil {
		log.Printf("CreateChat: commit error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Возвращаем успешный ответ
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"chat_id": chatID,
		"message": "Chat created successfully",
	})
}

func (h *Handler) CreateConnectChat(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	chatID := vars["chatId"]

	if chatID == "" {
		http.Error(w, "Missing chat ID", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("CreateConnectChat: upgrader error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	defer conn.Close()

	newChatHub := getOrCreateChatHub(h.DB, chatID)

	client := &ClientChat{
		conn:   conn,
		chatId: chatID,
		send:   make(chan []byte, 256),
		hub:    newChatHub,
	}

	client.hub.register <- client

	go client.writePump()
	go client.readPump(client.hub)
}

func getOrCreateChatHub(db *sql.DB, chatId string) *ClientHub {
	hubMutex.Lock()
	if hub, exists := chatHubs[chatId]; exists {
		hubMutex.RUnlock()
		return hub
	}
	hubMutex.RUnlock()
	hubMutex.Lock()
	defer hubMutex.Unlock()
	if hub, exists := chatHubs[chatId]; exists {
		return hub
	}

	newHub := NewClientHub(db)
	go newHub.Run()
	chatHubs[chatId] = newHub
	return newHub
}

func (h *ClientHub) Run() {
	for {
		select {
		case client := <-h.register:
			println("Client registered", client)
			client.conn.WriteMessage(websocket.TextMessage, []byte{})
		}
	}
}

func (c *ClientChat) readPump(hub *ClientHub) {
	defer func() {
		hub.unregister <- c
		c.conn.Close()
	}()

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}

		hub.broadcast <- message
	}
}

func (c *ClientChat) writePump() {
	defer c.conn.Close()

	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			c.conn.WriteMessage(websocket.TextMessage, message)
		}
	}
}
func NewClientHub(db *sql.DB) *ClientHub {
	return &ClientHub{
		clientsChat: make(map[*ClientChat]bool),
		broadcast:   make(chan []byte, 256),
		register:    make(chan *ClientChat),
		unregister:  make(chan *ClientChat),
		db:          db,
	}
}

// Функция для генерации уникального ID чата
func generateChatID() string {
	return fmt.Sprintf("chat_%d_%s", time.Now().Unix(), generateRandomString(8))
}

func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}
