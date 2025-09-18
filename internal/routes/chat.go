package routes

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/dgrijalva/jwt-go"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"log"
	"math/rand"
	"net/http"
	"strconv"
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
type Message struct {
	ID          int       `json:"id"`
	SenderID    string    `json:"sender_id"`
	Text        string    `json:"message_text"`
	MessageType string    `json:"message_type"`
	IsRead      bool      `json:"is_read"`
	CreatedAt   time.Time `json:"created_at"`
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
	send   chan *MessageChat
	hub    *ClientHub
	name   string
}

type MessageChat struct {
	Message  string `json:"message"`
	Name     string `json:"name"`
	ChatId   string `json:"chat_id"`
	SenderId string `json:"sender_id"`
}

type ClientHub struct {
	clientsChat map[*ClientChat]bool
	register    chan *ClientChat
	unregister  chan *ClientChat
	broadcast   chan *MessageChat
	db          *sql.DB
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// GetChat Получение переписки из чата
// @Summary Получение чата
// @Description Получение чата
// @Tags chats
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} CreateChatResponse "Чат успешно создан или пользователь присоединен к существующему"
// @Router /auth/chats [post]
func (h *Handler) GetChat(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value("user_id").(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	chatID := r.URL.Query().Get("chat_id")
	if chatID == "" {
		http.Error(w, "Missing chat_id parameter", http.StatusBadRequest)
		return
	}
	limit := 50 // По умолчанию 50 сообщений
	offset := 0

	if limitParam := r.URL.Query().Get("limit"); limitParam != "" {
		if parsedLimit, err := strconv.Atoi(limitParam); err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	if offsetParam := r.URL.Query().Get("offset"); offsetParam != "" {
		if parsedOffset, err := strconv.Atoi(offsetParam); err == nil && parsedOffset >= 0 {
			offset = parsedOffset
		}
	}

	if !h.userHasAccessToChat(userID, chatID) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	query := `
    SELECT id, sender_id, message_text, "message_type ", is_read
    FROM messages
    WHERE chat_id = $1
    ORDER BY id ASC  
    LIMIT $2 OFFSET $3
`

	rows, err := h.DB.Query(query, chatID, limit, offset)
	if err != nil {
		log.Printf("Database error: %v", err)
		http.Error(w, "Failed to get chat history", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	messages := []Message{}
	for rows.Next() {
		var msg Message

		err := rows.Scan(&msg.ID, &msg.SenderID, &msg.Text, &msg.MessageType, &msg.IsRead)
		if err != nil {
			log.Printf("Error scanning message row: %v", err)
			continue
		}

		messages = append(messages, msg)

		if err = rows.Err(); err != nil {
			log.Printf("Error iterating through results: %v", err)
			http.Error(w, "Error processing chat history", http.StatusInternalServerError)
			return
		}

	}

	response := struct {
		ChatID   string    `json:"chat_id"`
		Messages []Message `json:"messages"`
		Total    int       `json:"total_messages"`
	}{
		ChatID:   chatID,
		Messages: messages,
		Total:    len(messages),
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding response: %v", err)
		http.Error(w, "Error encoding response", http.StatusInternalServerError)
		return
	}
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
			WHERE ((user_id = $1 AND friend_id = $2) 
			   OR (user_id = $2 AND friend_id = $1))
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

	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Invalid token", http.StatusUnauthorized)
		return
	}

	parsedToken, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return jwtSecret, nil
	})

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("CreateConnectChat: upgrader error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	claims, ok := parsedToken.Claims.(jwt.MapClaims)
	if !ok {
		log.Println("Invalid token claims")
		http.Error(w, "Invalid token", http.StatusUnauthorized)
		return
	}
	userId, ok := claims["user_id"].(string)
	if !ok {
		log.Println("User Name not found in context")
		conn.Close()
		return
	}

	userName, ok := claims["user_name"].(string)
	if !ok {
		log.Println("User Name not found in context")
		conn.Close()
		return
	}

	var exists bool
	err = h.DB.QueryRow(`
    SELECT EXISTS(
        SELECT 1 FROM chat_participants
        WHERE chat_id = $1 AND user_id = $2
    )
`, chatID, userId).Scan(&exists)

	if err != nil {
		log.Printf("CreateConnectChat: chat participant check error: %v", err)
		conn.Close()
		return
	}
	if !exists {
		errMsg := map[string]string{
			"name":    "error",
			"message": "Вы не участник этого чата",
		}
		data, _ := json.Marshal(errMsg)
		conn.WriteMessage(websocket.TextMessage, data)
		conn.Close()
		return
	}

	newChatHub := getOrCreateChatHub(h.DB, chatID)

	client := &ClientChat{
		conn:   conn,
		chatId: chatID,
		send:   make(chan *MessageChat, 256),
		hub:    newChatHub,
		name:   userName,
		userId: userId,
	}

	client.hub.register <- client

	go client.writePump()
	go client.readPump(client.hub)
}

func getOrCreateChatHub(db *sql.DB, chatId string) *ClientHub {
	hubMutex.RLock()
	hub, exists := chatHubs[chatId]
	hubMutex.RUnlock()
	if exists {
		return hub
	}

	hubMutex.Lock()
	defer hubMutex.Unlock()
	hub, exists = chatHubs[chatId]
	if exists {
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
			h.clientsChat[client] = true
			println("Client registered", client)
			message := &MessageChat{
				Message:  "Is online",
				Name:     client.name,
				ChatId:   client.chatId,
				SenderId: client.userId,
			}
			client.hub.broadcast <- message

		case client := <-h.unregister:
			if _, ok := h.clientsChat[client]; ok {
				message := &MessageChat{
					Message:  "Left from chat",
					Name:     client.name,
					ChatId:   client.chatId,
					SenderId: client.userId,
				}
				client.hub.broadcast <- message
				delete(h.clientsChat, client)
				println("Client unregistered", client)
				close(client.send)
			}

		case msg := <-h.broadcast:
			if msg.Message != "Is online" && msg.Message != "Left from chat" {
				query := `
		INSERT INTO messages (chat_id, sender_id, message_text, "message_type ", is_read )
		VALUES ($1, $2, $3, $4, true)
		RETURNING id
	`
				var messageID int
				err := h.db.QueryRow(
					query,
					msg.ChatId,
					msg.SenderId,
					msg.Message,
					"type",
				).Scan(&messageID)

				if err != nil {
					log.Printf("Run: insert message error: %v", err)
					return
				}
			}

			for client := range h.clientsChat {
				select {
				case client.send <- msg:
				default:
					close(client.send)
					delete(h.clientsChat, client)
				}
			}
		}
	}
}

func (c *ClientChat) readPump(hub *ClientHub) {
	defer func() {
		hub.unregister <- c
		c.conn.Close()
	}()

	for {
		_, text, err := c.conn.ReadMessage()
		if err != nil {
			break
		}

		msg := &MessageChat{
			Message:  string(text),
			Name:     c.name,
			ChatId:   c.chatId,
			SenderId: c.userId,
		}
		hub.broadcast <- msg
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

			// Сериализация структуры в JSON
			data, err := json.Marshal(message)
			if err != nil {
				log.Println("marshal error:", err)
				continue
			}

			c.conn.WriteMessage(websocket.TextMessage, data)
		}
	}
}
func NewClientHub(db *sql.DB) *ClientHub {
	return &ClientHub{
		clientsChat: make(map[*ClientChat]bool),
		broadcast:   make(chan *MessageChat, 256),
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

func (h *Handler) userHasAccessToChat(userID, chatID string) bool {
	// Здесь должна быть логика проверки прав доступа
	// Например, проверка, является ли пользователь участником чата

	query := `
		SELECT COUNT(*) 
		FROM chat_participants 
		WHERE chat_id = $1 AND user_id = $2
	`

	var count int
	err := h.DB.QueryRow(query, chatID, userID).Scan(&count)
	if err != nil {
		log.Printf("Error checking chat access: %v", err)
		return false
	}

	return count > 0
}
