package routes

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
)

// @Summary Получить имя, id пользователя
// @Description берется из токена
// @Tags profile
// @Router /auth/profile [get]
// @Security BearerAuth
func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value("user_id").(string)
	if !ok {
		log.Printf("User ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	query := `SELECT id, name FROM users WHERE id = $1`
	row := h.DB.QueryRow(query, userID)

	var user User
	if err := row.Scan(&user.ID, &user.Name); err != nil {
		if err == sql.ErrNoRows {
			log.Printf("User not found: %d", userID)
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}
		log.Printf("Error scanning user: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(user); err != nil {
		log.Printf("Error encoding user: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}
