package routes

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
)

type FriendStatus string

const (
	StatusPending  FriendStatus = "pending"
	StatusAccepted FriendStatus = "accepted"
)

type Friend struct {
	UserId   string       `json:"user_id"`
	FriendId string       `json:"friend_id"`
	Status   FriendStatus `json:"status"`
}

type FriendRequest struct {
	UserId   string `json:"user_id"`
	FriendId string `json:"friend_id"`
}

type AcceptedFriendRequest struct {
	Friend_id string `json:"friend_id"`
}

type FriendResponse struct {
	Name   string       `json:"name"`
	Status FriendStatus `json:"status"`
}

// @Summary      Создание друга
// @Tags         friends
// @Accept       json
// @Produce      json
// @Router       /auth/friends [post]
// @Param data body routes.FriendRequest true "Данные"
// @Security BearerAuth
// @Success  200  {object} routes.FriendStatus
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
			err = h.DB.QueryRow(`INSERT INTO friendship (user_id, friend_id, status) VALUES ($1, $2, $3) RETURNING status`, req.UserId, req.FriendId, StatusPending).Scan(&friend.Status)
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

// @Summary      Получение друзей
// @Tags         friends
// @Accept       json
// @Produce      json
// @Router       /auth/friends [get]
// @Security BearerAuth
func (h *Handler) GetFriends(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value("user_id").(string)
	if !ok {
		log.Printf("User ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	status := r.URL.Query().Get("status")

	if status == "" {
		log.Printf("Status query parameter is missing")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	rows, err := h.DB.Query(`SELECT u.name FROM friendship f JOIN users u ON f.friend_id::text = u.id::text WHERE f.user_id = $1 AND f.status = $2`, userID, status)
	if err != nil {
		log.Printf("Error querying friendships: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	var friends []FriendResponse
	for rows.Next() {
		var friend FriendResponse
		err := rows.Scan(&friend.Name, &friend.Status)
		if err != nil {
			log.Printf("Error scanning friendships: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		friends = append(friends, friend)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(friends); err != nil {
		log.Printf("Error encoding response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// @Summary      Принять друга
// @Tags         friends
// @Accept       json
// @Produce      json
// @Router       /auth/friends/accepted [post]
// @Param data body routes.AcceptedFriendRequest true "Принять друга"
// @Security BearerAuth
func (h *Handler) AcceptedFriend(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value("user_id").(string)
	if !ok {
		log.Printf("User ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req AcceptedFriendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Error decoding request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Friend_id == "" {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	result, err := h.DB.Exec(`UPDATE friendship SET status = $1 WHERE user_id = $2 AND friend_id = $3`, StatusAccepted, userID, req.Friend_id)
	if err != nil {
		log.Printf("Error updating friend: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	rows, err := result.RowsAffected()
	if err != nil {
		log.Printf("Error updating friend: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if rows == 0 {
		http.Error(w, "Friend Not Found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err = json.NewEncoder(w).Encode(rows); err != nil {
		log.Printf("Error encoding response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}
