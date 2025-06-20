package routes

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
)

type Friend struct {
	UserId   string `json:"user_id"`
	FriendId string `json:"friend_id"`
	Status   string `json:"status"`
}

type FriendRequest struct {
	UserId   string `json:"user_id"`
	FriendId string `json:"friend_id"`
}

// @Summary      Создание чата
// @Tags         friends
// @Accept       json
// @Produce      json
// @Router       /auth/friends [post]
// @Param data body routes.FriendRequest true "Данные"
// @Security BearerAuth
func (h *Handler) CreateFriendship(w http.ResponseWriter, r *http.Request) {
	var req FriendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Error decoding request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.FriendId == "" || req.UserId == "" {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var status string
	err := h.DB.QueryRow(`
    SELECT status FROM friendship 
    WHERE user_id = $1 AND friend_id = $2
`, req.UserId, req.FriendId).Scan(&status)
	if err != nil {
		if err == sql.ErrNoRows {
			var friend Friend
			err = h.DB.QueryRow(`INSERT INTO friendship (user_id, friend_id, status) VALUES ($1, $2, $3) RETURNING status`, req.UserId, req.FriendId, "pending").Scan(&friend.Status)
			if err != nil {
				log.Printf("Error inserting friend: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(friend.Status)
		} else {
			log.Printf("Error querying friendships: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	}
	if status == "pending" {
		http.Error(w, "This Friend Pending", http.StatusConflict)
		return
	}
	if status == "accepted" {
		http.Error(w, "This Friend Accepted", http.StatusConflict)
		return
	}
}
