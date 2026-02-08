package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Build-Flow-Labs/merge-queue/internal/github"
	"github.com/Build-Flow-Labs/merge-queue/internal/queue"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"
)

func main() {
	log.Println("Starting Merge Queue Service")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(envInt("DB_MAX_OPEN_CONNS", 10))
	db.SetMaxIdleConns(envInt("DB_MAX_IDLE_CONNS", 5))
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	log.Println("Connected to database")

	runMigrations(dsn)

	// GitHub App configuration
	ghConfig := &github.AppConfig{
		AppID:          envInt64("GITHUB_APP_ID", 0),
		PrivateKey:     os.Getenv("GITHUB_PRIVATE_KEY"),
		PrivateKeyPath: os.Getenv("GITHUB_PRIVATE_KEY_PATH"),
		WebhookSecret:  os.Getenv("GITHUB_WEBHOOK_SECRET"),
	}
	if ghConfig.AppID == 0 {
		log.Fatal("GITHUB_APP_ID environment variable is required")
	}
	if ghConfig.PrivateKey == "" && ghConfig.PrivateKeyPath == "" {
		log.Fatal("GITHUB_PRIVATE_KEY or GITHUB_PRIVATE_KEY_PATH is required")
	}

	// Initialize queue processor
	processor := queue.NewProcessor(db, ghConfig)
	processor.Start()
	defer processor.Stop()

	// Initialize HTTP handlers
	handlers := &Handlers{
		db:        db,
		ghConfig:  ghConfig,
		processor: processor,
	}

	mux := http.NewServeMux()
	registerRoutes(mux, handlers)

	// Middleware
	handler := withCORS(withLogging(withRecover(mux)))

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Merge Queue Service listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-done
	log.Println("Shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("server forced shutdown: %v", err)
	}
	log.Println("Server stopped")
}

func runMigrations(dsn string) {
	m, err := migrate.New("file://./migrations", dsn)
	if err != nil {
		log.Printf("migrations: failed to initialize: %v", err)
		return
	}
	defer m.Close()

	if err := m.Up(); err != nil {
		if err == migrate.ErrNoChange {
			log.Println("migrations: up to date")
			return
		}
		log.Fatalf("migrations: failed to apply: %v", err)
	}
	log.Println("migrations: applied successfully")
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func envInt64(key string, fallback int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

// Middleware

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

func withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("panic: %v", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Routes

func registerRoutes(mux *http.ServeMux, h *Handlers) {
	// Dashboard
	mux.HandleFunc("GET /", h.Dashboard)

	// Health
	mux.HandleFunc("GET /healthz", h.Healthz)

	// GitHub webhooks
	mux.HandleFunc("POST /webhooks/github", h.GitHubWebhook)

	// API - Queue management
	mux.HandleFunc("GET /api/v1/queue", h.GetQueue)
	mux.HandleFunc("POST /api/v1/queue", h.AddToQueue)
	mux.HandleFunc("DELETE /api/v1/queue/{id}", h.RemoveFromQueue)
	mux.HandleFunc("POST /api/v1/queue/{id}/retry", h.RetryQueueItem)
	mux.HandleFunc("POST /api/v1/queue/{id}/pause", h.PauseQueueItem)

	// API - Settings
	mux.HandleFunc("GET /api/v1/settings", h.GetSettings)
	mux.HandleFunc("PUT /api/v1/settings", h.UpdateSettings)

	// API - Events
	mux.HandleFunc("GET /api/v1/events", h.GetEvents)

	// API - Installations
	mux.HandleFunc("GET /api/v1/installations", h.ListInstallations)

	// API - Repos
	mux.HandleFunc("GET /api/v1/repos", h.ListRepos)
}

// Handlers

type Handlers struct {
	db        *sql.DB
	ghConfig  *github.AppConfig
	processor *queue.Processor
}

func (h *Handlers) Healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, "%v", toJSON(data))
}

func toJSON(v interface{}) string {
	switch val := v.(type) {
	case map[string]string:
		result := "{"
		first := true
		for k, v := range val {
			if !first {
				result += ","
			}
			result += fmt.Sprintf(`"%s":"%s"`, k, v)
			first = false
		}
		return result + "}"
	default:
		return fmt.Sprintf("%v", val)
	}
}
