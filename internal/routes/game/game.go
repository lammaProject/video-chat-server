package game

import (
	"encoding/json"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
	"sync"
	"time"
)

var (
	gameHubs = make(map[string]*Hub)
	hubMutex sync.RWMutex
)

type Message struct {
	IsStart bool  `json:"is_start"`
	Ball    *Move `json:"ball"`
	Player1 *Move `json:"player1"`
	Player2 *Move `json:"player2"`
}

type PlayerMove struct {
	Id     int   `json:"id"`
	Player *Move `json:"player"`
}

type BulletsMove struct {
	Id     int    `json:"id"`
	Bullet Bullet `json:"bullet"`
}

type Bullet struct {
	Id       int `json:"id"`
	X        int `json:"x"`
	Y        int `json:"y"`
	PlayerId int `json:"playerId"`
}

type Move struct {
	Y int `json:"y"`
	X int `json:"x"`
}

type Hub struct {
	register        chan *Client
	gamePole        chan *Message
	unregister      chan *Client
	players         map[*Client]bool
	bullets         map[int]*Bullet
	ball            *Move
	player1         *Move
	player2         *Move
	playerMove      chan *PlayerMove
	bulletMove      chan *Bullet
	bulletIdCounter int
	bulletMutex     sync.Mutex
}
type Client struct {
	conn   *websocket.Conn
	gameId string
	send   chan interface{}
	bullet chan *Bullet
	hub    *Hub
}

type HitMessage struct {
	Type     string `json:"type"`
	PlayerId int    `json:"playerId"`
	BulletId int    `json:"bulletId"`
}

// Добавим структуру для удаления пули
type BulletRemoveMessage struct {
	Type     string `json:"type"`
	BulletId int    `json:"bulletId"`
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func getOrCreateChatHub(gameId string) *Hub {
	hubMutex.RLock()
	hub, exists := gameHubs[gameId]
	hubMutex.RUnlock()
	if exists {
		return hub
	}

	hubMutex.Lock()
	defer hubMutex.Unlock()
	hub, exists = gameHubs[gameId]
	if exists {
		return hub
	}

	ball := &Move{
		X: 50,
		Y: 250,
	}
	player1 := &Move{X: 100, Y: 500}
	player2 := &Move{X: 100, Y: 50}

	newHub := &Hub{
		register:        make(chan *Client),
		gamePole:        make(chan *Message),
		unregister:      make(chan *Client),
		players:         make(map[*Client]bool),
		bullets:         make(map[int]*Bullet),
		ball:            ball,
		player1:         player1,
		player2:         player2,
		playerMove:      make(chan *PlayerMove),
		bulletMove:      make(chan *Bullet),
		bulletIdCounter: 0,
	}

	go newHub.Run()
	gameHubs[gameId] = newHub
	return newHub
}

func CreateConnectGame(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	gameId := vars["gameId"]
	if gameId == "" {
		http.Error(w, "Game ID is required", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		http.Error(w, "Failed to upgrade to websocket", http.StatusInternalServerError)
		return
	}

	newGameHub := getOrCreateChatHub(gameId)

	player := &Client{
		conn:   conn,
		gameId: gameId,
		send:   make(chan interface{}, 256),
		bullet: make(chan *Bullet, 256),
		hub:    newGameHub,
	}

	player.hub.register <- player
	go player.writePump()
	go player.readPump(player.hub)
}
func (h *Hub) Run() {
	go h.processBullets()

	for {
		select {
		case client := <-h.register:
			h.players[client] = true

			clientId := 1
			if len(h.players) > 1 {
				clientId = 2
			}

			// Формируем сообщение и кладём в канал
			msg := &Message{
				IsStart: len(h.players) == 2,
				Ball:    h.ball,
				Player1: h.player1,
				Player2: h.player2,
			}
			initMsg := map[string]interface{}{
				"type":      "init",
				"id":        clientId,
				"gameState": msg,
			}
			client.send <- initMsg // ✅ вместо прямого WriteMessage

		case client := <-h.unregister:
			if _, ok := h.players[client]; ok {
				delete(h.players, client)
				close(client.send)
				close(client.bullet) // закрываем и bullet-канал
				log.Printf("Client unregistered, players left: %d\n", len(h.players))
			}

		case msg := <-h.playerMove:
			player := msg.Id
			move := msg.Player

			if player == 1 {
				h.player1 = move
			} else {
				h.player2 = move
			}

			gamePole := &Message{
				IsStart: len(h.players) == 2,
				Ball:    h.ball,
				Player1: h.player1,
				Player2: h.player2,
			}

			// Рассылаем всем игрокам через их send-канал
			for c := range h.players {
				select {
				case c.send <- gamePole:
				default:
					// если канал переполнен — отключаем клиента
					close(c.send)
					delete(h.players, c)
				}
			}

		case bullet := <-h.bulletMove:
			h.bulletMutex.Lock()
			h.bulletIdCounter++
			bullet.Id = h.bulletIdCounter
			h.bullets[bullet.Id] = bullet
			h.bulletMutex.Unlock()

			// Формируем событие "новая пуля"
			bulletMsg := map[string]interface{}{
				"type":   "newBullet",
				"bullet": bullet,
			}

			// Рассылаем пулю всем игрокам через send
			for c := range h.players {
				select {
				case c.send <- bulletMsg:
				default:
					close(c.send)
					delete(h.players, c)
				}
			}
		}
	}
}

func (c *Client) readPump(hub *Hub) {
	defer func() {
		hub.unregister <- c
		c.conn.Close()
	}()

	// Увеличиваем таймауты
	c.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		return nil
	})

	// Устанавливаем максимальный размер сообщения
	c.conn.SetReadLimit(512)

	for {
		_, text, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("websocket error: %v", err)
			}
			break
		}

		// Обновляем deadline при каждом сообщении
		c.conn.SetReadDeadline(time.Now().Add(90 * time.Second))

		var raw map[string]interface{}
		if err := json.Unmarshal(text, &raw); err != nil {
			log.Printf("Ошибка парсинга JSON: %v\n", err)
			continue // Используем continue вместо return
		}
		log.Printf("Bullet received from Player %d: X=%d, Y=%d", raw)

		if _, ok := raw["bullet"]; ok {
			var wrapper struct {
				Id     int `json:"id"`
				Bullet struct {
					X float64 `json:"x"`
					Y float64 `json:"y"`
				} `json:"bullet"`
			}
			if err := json.Unmarshal(text, &wrapper); err != nil {
				log.Printf("Ошибка парсинга Bullet: %v\n", err)
				continue
			}
			bullet := &Bullet{
				X:        int(wrapper.Bullet.X),
				Y:        int(wrapper.Bullet.Y),
				PlayerId: wrapper.Id,
			}
			hub.bulletMove <- bullet
		} else {
			var msg PlayerMove
			if err := json.Unmarshal(text, &msg); err != nil {
				log.Printf("Ошибка парсинга PlayerMove: %v\n", err)
				continue
			}
			hub.playerMove <- &msg
		}
	}
}
func (c *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				// канал закрыт → закрываем сокет
				return
			}

			data, err := json.Marshal(msg)
			if err != nil {
				log.Println("marshal error:", err)
				continue
			}

			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}

		case <-ticker.C:
			// ping keepalive
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (h *Hub) processBullets() {
	ticker := time.NewTicker(30 * time.Millisecond)
	defer ticker.Stop()

	for {
		<-ticker.C
		h.bulletMutex.Lock()

		bulletsToRemove := []int{}

		for id, bullet := range h.bullets {
			if bullet.PlayerId == 1 {
				bullet.Y -= 5
				if bullet.Y < 50 {
					bulletsToRemove = append(bulletsToRemove, id)
					continue
				}
				if checkCollision(bullet, h.player2) {
					h.sendHitNotification(2, id)
					bulletsToRemove = append(bulletsToRemove, id)
					continue
				}
			} else if bullet.PlayerId == 2 {
				bullet.Y += 5
				if bullet.Y > 500 {
					bulletsToRemove = append(bulletsToRemove, id)
					continue
				}
				if checkCollision(bullet, h.player1) {
					h.sendHitNotification(1, id)
					bulletsToRemove = append(bulletsToRemove, id)
					continue
				}
			}

			// Отправляем обновление позиции
			for c := range h.players {
				select {
				case c.send <- map[string]interface{}{
					"type":   "bulletUpdate",
					"bullet": bullet,
				}:
				default:
					close(c.send)
					delete(h.players, c)
				}
			}
		}

		// Удаляем "мёртвые" пули
		for _, id := range bulletsToRemove {
			delete(h.bullets, id)
			h.sendBulletRemoval(id)
		}

		h.bulletMutex.Unlock()
	}
}

func (h *Hub) sendHitNotification(playerId int, bulletId int) {
	msg := map[string]interface{}{
		"type":     "hit",
		"playerId": playerId,
		"bulletId": bulletId,
	}

	for c := range h.players {
		select {
		case c.send <- msg:
		default:
			close(c.send)
			delete(h.players, c)
		}
	}
}

func (h *Hub) sendBulletRemoval(bulletId int) {
	msg := map[string]interface{}{
		"type":     "bulletRemove",
		"bulletId": bulletId,
	}

	for c := range h.players {
		select {
		case c.send <- msg:
		default:
			// Если клиент завис и канал забит → отключаем
			close(c.send)
			delete(h.players, c)
		}
	}
}

func checkCollision(bullet *Bullet, player *Move) bool {
	return abs(bullet.X-player.X) < 50 && abs(bullet.Y-player.Y) < 50
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
