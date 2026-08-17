package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"awesomeProject/internal/database"
	"awesomeProject/internal/game"
	"awesomeProject/internal/models"

	"github.com/gorilla/websocket"
)

// events emitted back to the client.
const (
	EventWallet     = "wallet"
	EventPlayResult = "play_result"
	EventPlayClosed = "play_closed"
	EventError      = "error"
)

type Repository interface {
	GetPlayerBalance(ctx context.Context, clientID string) (float64, error)
	CreatePlay(ctx context.Context, clientID string, betAmount float64, betType string, drawnNumber int, result string, pendingWin float64) (*models.GameSession, error)
	ClosePlay(ctx context.Context, clientID string) (*models.GameSession, float64, error)
}

// Handler upgrades http connections to websocket and routes game actions.
type Handler struct {
	repo     Repository
	upgrader websocket.Upgrader
}

func NewHandler(repo Repository) *Handler {
	return &Handler{
		repo: repo,
		upgrader: websocket.Upgrader{
			// origin is unrestricted so the game can be tested with postman
			CheckOrigin: func(_ *http.Request) bool { return true },
		},
	}
}

// ServeWS() reads JSON until connection ends
func (h *Handler) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket: upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	for {
		var msg models.WSMessage
		if err := conn.ReadJSON(&msg); err != nil {
			var syntaxErr *json.SyntaxError
			if errors.As(err, &syntaxErr) {
				h.writeError(conn, "", "invalid JSON message")
				continue
			}
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("websocket: read error: %v", err)
			}
			return
		}

		h.handleMessage(r.Context(), conn, msg)
	}
}

func (h *Handler) handleMessage(ctx context.Context, conn *websocket.Conn, msg models.WSMessage) {
	clientID := strings.TrimSpace(msg.ClientID)
	if clientID == "" {
		h.writeError(conn, "", "clientId is required")
		return
	}

	switch msg.Action {
	case models.ActionWallet:
		h.handleWallet(ctx, conn, clientID)
	case models.ActionPlay:
		h.handlePlay(ctx, conn, clientID, msg)
	case models.ActionEndPlay:
		h.handleEndPlay(ctx, conn, clientID)
	default:
		h.writeError(conn, clientID, "unknown action")
	}
}

func (h *Handler) handleWallet(ctx context.Context, conn *websocket.Conn, clientID string) {
	balance, err := h.repo.GetPlayerBalance(ctx, clientID)
	if err != nil {
		log.Printf("websocket: wallet: %v", err)
		h.writeError(conn, clientID, "could not fetch balance")
		return
	}

	h.write(conn, models.WSMessage{
		Event:    EventWallet,
		ClientID: clientID,
		Balance:  &balance,
	})
}

func (h *Handler) handlePlay(ctx context.Context, conn *websocket.Conn, clientID string, msg models.WSMessage) {
	drawnNumber, result, pendingWin, err := game.Play(msg.BetAmount, msg.BetType)
	if err != nil {
		h.writeError(conn, clientID, err.Error())
		return
	}

	if _, err := h.repo.CreatePlay(ctx, clientID, msg.BetAmount, msg.BetType, drawnNumber, result, pendingWin); err != nil {
		h.writeError(conn, clientID, mapError(err))
		return
	}

	_, balance, err := h.repo.ClosePlay(ctx, clientID)
	if err != nil {
		h.writeError(conn, clientID, err.Error())
		return
	}

	h.write(conn, models.WSMessage{
		Event:       EventPlayResult,
		ClientID:    clientID,
		DrawnNumber: drawnNumber,
		Result:      result,
		Balance:     &balance,
	})
}

func (h *Handler) handleEndPlay(ctx context.Context, conn *websocket.Conn, clientID string) {
	_, balance, err := h.repo.ClosePlay(ctx, clientID)
	if err != nil {
		h.writeError(conn, clientID, mapError(err))
		return
	}

	h.write(conn, models.WSMessage{
		Event:    EventPlayClosed,
		ClientID: clientID,
		Balance:  &balance,
	})
}

// mapError() converts domain errors into messages formatted for the player
func mapError(err error) string {
	switch {
	case errors.Is(err, database.ErrActivePlayExists):
		return database.ErrActivePlayExists.Error()
	case errors.Is(err, database.ErrInsufficientFunds):
		return database.ErrInsufficientFunds.Error()
	case errors.Is(err, database.ErrNoActiveSession):
		return database.ErrNoActiveSession.Error()
	case errors.Is(err, database.ErrPlayerNotFound):
		return database.ErrPlayerNotFound.Error()
	default:
		log.Printf("websocket: unexpected error: %v", err)
		return "internal error"
	}
}

func (h *Handler) writeError(conn *websocket.Conn, clientID, message string) {
	h.write(conn, models.WSMessage{
		Event:    EventError,
		ClientID: clientID,
		Message:  message,
	})
}

func (h *Handler) write(conn *websocket.Conn, msg models.WSMessage) {
	if err := conn.WriteJSON(msg); err != nil {
		log.Printf("websocket: write error: %v", err)
	}
}
