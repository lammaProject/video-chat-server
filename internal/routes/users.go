package routes

import (
	"database/sql"
	"encoding/json"
	"github.com/gorilla/mux"
	"golang.org/x/crypto/bcrypt"
	"log"
	"net/http"
	"strconv"
)

type RegisterRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}
type LoginRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

type AuthResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

// @Summary      Получить всех пользователей
// @Description  Получить список всех пользователей
// @Tags         users
// @Accept       json
// @Produce      json
// @Failure      500  {object}  map[string]string
// @Router       /users [get]
func (h *Handler) GetUsers(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value("user_id").(string)
	if !ok {
		log.Printf("User ID not found in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	userIDInt, err := strconv.Atoi(userID)
	if err != nil {
		log.Printf("Invalid user ID format: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	query := `
    SELECT u.id, u.name, 
           (
               SELECT f.status 
               FROM friendship f 
               WHERE ((f.user_id = $1 AND f.friend_id = u.id::text) OR 
                      (f.user_id = u.id::text AND f.friend_id = $2))
               LIMIT 1
           ) as is_friend
    FROM users u
    WHERE u.id != $3::integer
    ORDER BY u.id DESC
`

	rows, err := h.DB.Query(query, userIDInt, userIDInt, userIDInt)

	if err != nil {
		log.Printf("Error fetching users: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var user User
		if err := rows.Scan(&user.ID, &user.Name, &user.IsFriend); err != nil {
			log.Printf("Error scanning users: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		users = append(users, user)
	}

	if err := rows.Err(); err != nil {
		log.Printf("Error fetching users: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(users); err != nil {
		log.Printf("Error encoding users: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// @Summary Получить пользователя по имени
// @Description Получить данные конкретного пользователя
// @Tags users
// @Accept json
// @Produce json
// @Param name path string true "Имя пользователя"
// @Router /users/{name} [get]
func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]

	query := `SELECT id, name FROM users WHERE name = $1`
	row := h.DB.QueryRow(query, name)

	var user User
	if err := row.Scan(&user.ID, &user.Name); err != nil {
		if err == sql.ErrNoRows {
			log.Printf("User not found: %s", name)
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

// @Summary Регистрация пользователя
// @Description Зарегистрировать нового пользователя
// @Tags users
// @Accept json
// @Produce json
// @Router /users/register [post]
// @Param data body routes.RegisterRequest true "Данные"
func (h *Handler) RegisterUser(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Error decoding register request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.Password == "" {
		http.Error(w, "Name and password are required", http.StatusBadRequest)
		return
	}

	var count int
	err := h.DB.QueryRow("SELECT COUNT(*) FROM users WHERE name = $1", req.Name).Scan(&count)
	if err != nil {
		log.Printf("Error checking existing user: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if count > 0 {
		http.Error(w, "User with this name already exists", http.StatusConflict)
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("Error hashing password: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	var user User
	err = h.DB.QueryRow(
		"INSERT INTO users (name, password) VALUES ($1, $2) RETURNING id, name",
		req.Name, string(hashedPassword),
	).Scan(&user.ID, &user.Name)

	if err != nil {
		log.Printf("Error inserting user: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	token, err := createToken(user.ID)
	if err != nil {
		log.Printf("Error creating token: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(AuthResponse{
		Token: token, User: user,
	})
}

// @Summary Аутентификация
// @Tags users
// @Accept json
// @Produce json
// @Router /users/login [post]
// @Param data body routes.LoginRequest true "Данные"
func (h *Handler) LoginUser(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Error decoding login request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Проверка обязательных полей
	if req.Name == "" || req.Password == "" {
		http.Error(w, "Name and password are required", http.StatusBadRequest)
		return
	}

	// Поиск пользователя по имени
	var user User
	var hashedPassword string
	err := h.DB.QueryRow(
		"SELECT id, name, password FROM users WHERE name = $1",
		req.Name,
	).Scan(&user.ID, &user.Name, &hashedPassword)

	if err != nil {
		log.Printf("Error finding user: %v", err)
		http.Error(w, "Invalid name or password", http.StatusUnauthorized)
		return
	}

	// Проверка пароля
	err = bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(req.Password))
	if err != nil {
		log.Printf("Invalid password for user %s: %v", req.Name, err)
		http.Error(w, "Invalid name or password", http.StatusUnauthorized)
		return
	}

	// Создание JWT токена
	token, err := createToken(user.ID)
	if err != nil {
		log.Printf("Error creating token: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Отправка ответа
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(AuthResponse{
		Token: token,
		User:  user,
	})
}
