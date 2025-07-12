package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
	httpSwagger "github.com/swaggo/http-swagger"
	"log"
	"net/http"
	"os"
	"os/signal"
	_ "server/docs"
	"server/internal/routes"
	"server/internal/signaling"
	"syscall"
	"time"
)

var db *sql.DB

// @title Swagger Example API
// @version 1.0
// @description This is a sample server Petstore server.
// @termsOfService http://swagger.io/terms/
// @contact.name API Support
// @contact.url http://www.swagger.io/support
// @contact.email support@swagger.io
// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html
// @host localhost:8080
// @BasePath /
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Введите ваш токен
func main() {
	var addr = flag.String("addr", "0.0.0.0:8080", "address to listen on")

	flag.Parse()

	if err := initDB(); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	roomManager := signaling.NewRoomManager()

	router := mux.NewRouter()
	router.Use(loggingMiddleware)
	router.Use(corsMiddleware)
	router.Methods("OPTIONS").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	router.PathPrefix("/swagger/").Handler(httpSwagger.Handler(
		httpSwagger.URL("/swagger/doc.json"),
		httpSwagger.DeepLinking(true),
	))

	handler := routes.NewHandler(db)
	router.HandleFunc("/users/register", handler.RegisterUser).Methods("POST")
	router.HandleFunc("/users/login", handler.LoginUser).Methods("POST")

	protectedRouter := router.PathPrefix("/auth").Subrouter()
	protectedRouter.Use(routes.AuthMiddleware)

	// users
	protectedRouter.HandleFunc("/users", handler.GetUsers).Methods("GET")
	protectedRouter.HandleFunc("/users/{name}", handler.GetUser).Methods("GET")

	// friends
	protectedRouter.HandleFunc("/friends", handler.CreateFriendship).Methods("POST")
	protectedRouter.HandleFunc("/friends", handler.GetFriends).Methods("GET")
	protectedRouter.HandleFunc("/friends/accepted", handler.AcceptedFriend).Methods("POST")
	// chats
	protectedRouter.HandleFunc("/chats", handler.CreateChat).Methods("POST")
	// roms
	protectedRouter.HandleFunc("/rooms", handler.CreateRoom).Methods("POST")
	protectedRouter.HandleFunc("/rooms", handler.GetRooms).Methods("GET")
	// profile
	protectedRouter.HandleFunc("/profile", handler.GetProfile).Methods("GET")
	// ws
	protectedRouter.HandleFunc("/ws/chats/{chatId}/connect", handler.CreateConnectChat)
	protectedRouter.HandleFunc("/ws/{roomId}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		roomID := vars["roomId"]

		if roomID == "" {
			http.Error(w, "Room ID is required", http.StatusBadRequest)
			return
		}

		// Получаем или создаем комнату
		hub := roomManager.GetOrCreateRoom(roomID, db)
		signaling.ServerWs(hub, w, r)
	})

	srv := &http.Server{
		Addr:         *addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("Starting server on %s", *addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("ListenAndServe: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown failed: %v", err)
	}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf(
			"%s %s %s %s",
			r.Method,
			r.RequestURI,
			r.RemoteAddr,
			time.Since(start),
		)
	})
}

func initDB() error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgresql://postgres:lOQUudUlZXRXnLcwMatSSioGevydrLnL@mainline.proxy.rlwy.net:57379/railway"
	}

	var err error
	db, err = sql.Open("postgres", databaseURL)
	if err != nil {
		return fmt.Errorf("sql.Open: %v", err)
	}

	if err := db.Ping(); err != nil {
		return fmt.Errorf("db.Ping: %v", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	return nil
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("CORS middleware: %s %s", r.Method, r.URL.Path)

		// Устанавливаем заголовки CORS для всех запросов
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		// Обрабатываем предварительные запросы OPTIONS
		if r.Method == "OPTIONS" {
			log.Printf("Responding to OPTIONS request for %s", r.URL.Path)
			w.WriteHeader(http.StatusOK)
			return // Важно: останавливаем обработку здесь
		}

		// Для других методов продолжаем обработку
		next.ServeHTTP(w, r)
	})
}
