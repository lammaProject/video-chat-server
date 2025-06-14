package routes

import (
	"encoding/json"
	"log"
	"net/http"
)

func (h *Handler) CreateRoom(w http.ResponseWriter, r *http.Request) {
	var req Room
	userID, ok := r.Context().Value("user_id").(string)
	if !ok {
		log.Printf("User ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Error decoding register request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "Name and password are required", http.StatusBadRequest)
		return
	}

	var room Room

	err := h.DB.QueryRow(
		"INSERT INTO rooms (name, created_by) VALUES ($1, $2) RETURNING id, name, created_by",
		req.Name, userID,
	).Scan(&room.ID, &room.Name, &room.CreatedBy)

	if err != nil {
		log.Printf("Error creating room: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(room); err != nil {
		log.Printf("Error encoding room: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

func (h *Handler) GetRooms(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value("user_id").(string)
	if !ok {
		log.Printf("User ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	query := `SELECT id, name, created_by FROM rooms WHERE created_by = $1 ORDER BY id DESC`
	rows, err := h.DB.Query(query, userID)
	if err != nil {
		log.Printf("Error fetching rooms: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var rooms []Room
	for rows.Next() {
		var room Room
		if err := rows.Scan(&room.ID, &room.Name, &room.CreatedBy); err != nil {
			log.Printf("Error scanning room: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		rooms = append(rooms, room)
	}

	if err := rows.Err(); err != nil {
		log.Printf("Error fetching rooms: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rooms); err != nil {
		log.Printf("Error encoding rooms: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}
