package signaling

import (
	"database/sql"
	"log"
	"sync"
)

// Менеджер всех комнат
type RoomManager struct {
	rooms map[string]*Hub // roomID -> Hub
	mutex sync.RWMutex
}

func NewRoomManager() *RoomManager {
	return &RoomManager{
		rooms: make(map[string]*Hub),
	}
}

// Получить или создать комнату
func (rm *RoomManager) GetOrCreateRoom(roomID string, db *sql.DB) *Hub {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	// Если комната уже существует, возвращаем её
	if hub, exists := rm.rooms[roomID]; exists {
		return hub
	}

	// Создаем новую комнату
	log.Printf("Creating new room: %s", roomID)
	hub := NewHub(db)
	rm.rooms[roomID] = hub

	// Запускаем Hub для этой комнаты
	go hub.Run()

	return hub
}

// Удалить пустую комнату
func (rm *RoomManager) RemoveRoomIfEmpty(roomID string) {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	if hub, exists := rm.rooms[roomID]; exists {
		if len(hub.clients) == 0 {
			log.Printf("Removing empty room: %s", roomID)
			delete(rm.rooms, roomID)
		}
	}
}
