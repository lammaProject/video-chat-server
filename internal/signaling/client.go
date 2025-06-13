package signaling

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 65536
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

type MessageType string

const (
	MessageTypeChat           MessageType = "chat"
	MessageTypeOffer          MessageType = "offer"
	MessageTypeAnswer         MessageType = "answer"
	MessageTypeIceCandidate   MessageType = "ice-candidate"
	MessageTypeVideoChatStart MessageType = "video-chat-start"
)

type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan interface{}
	id   string
}

type Message struct {
	Text string `json:"text"`
	From string `json:"from"`
	To   string `json:"to"`
}

type RTCSessionDescription struct {
	Type string `json:"type"`
	SDP  string `json:"sdp"`
}

type RTCIceCandidate struct {
	Candidate        string  `json:"candidate"`
	SdpMLineIndex    *uint16 `json:"sdpMLineIndex"`
	SdpMid           string  `json:"sdpMid"`
	UsernameFragment string  `json:"usernameFragment,omitempty"`
}

type VideoChatMessage struct {
	Type         string                 `json:"type"`
	Offer        *RTCSessionDescription `json:"offer,omitempty"`
	Answer       *RTCSessionDescription `json:"answer,omitempty"`
	IceCandidate *RTCIceCandidate       `json:"iceCandidate,omitempty"`
	From         string                 `json:"from"`
	To           string                 `json:"to"`
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}

		message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))

		var typeCheck struct {
			Type string `json:"type"`
		}

		if err := json.Unmarshal(message, &typeCheck); err != nil {
			log.Printf("Error parsing JSON: %v", err)
			continue
		}

		switch typeCheck.Type {
		case "chat":
			var msg Message
			if err := json.Unmarshal(message, &msg); err != nil {
				log.Printf("Error parsing chat message: %v", err)
				continue
			}
			c.hub.message <- &msg

		case "videochat":
			var msg VideoChatMessage
			if err := json.Unmarshal(message, &msg); err != nil {
				log.Printf("Error parsing videochat message: %v", err)
				continue
			}
			msg.From = c.id
			c.hub.videochat <- &msg
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteJSON(message); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func ServerWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	clientId := r.URL.Query().Get("id")
	if clientId == "" {
		conn.Close()
		return
	}

	client := &Client{
		hub:  hub,
		conn: conn,
		send: make(chan interface{}, 256),
		id:   clientId,
	}

	client.hub.register <- client

	go client.writePump()
	go client.readPump()
}
