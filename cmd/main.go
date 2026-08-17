package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"awesomeProject/internal/database"
	ws "awesomeProject/internal/websocket"
)

func main() {
	dsn := getenv("DATABASE_URL", "postgres://root:root@localhost:5432/dice_api?sslmode=disable")
	addr := getenv("HTTP_ADDR", ":8080")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	repo, err := database.NewPostgresRepository(ctx, dsn)
	cancel()
	if err != nil {
		log.Fatalf("main: connect to postgres: %v", err)
	}

	handler := ws.NewHandler(repo)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", handler.ServeWS)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 15 * time.Second,
	}

	go func() {
		log.Printf("main: dice game listening on %s (ws://localhost%s/ws)", addr, addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("main: http server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("main: shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("main: graceful shutdown failed: %v", err)
	}
}

func getenv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}
