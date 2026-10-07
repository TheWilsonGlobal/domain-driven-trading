package websocket

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/vortex-trading/vortex-trading-engine/internal/domain/order"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow cross-origin for trading frontend dev
	},
}

type MarketHub struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan []byte
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	mu         sync.RWMutex
	logger     *slog.Logger
}

func NewMarketHub(logger *slog.Logger) *MarketHub {
	return &MarketHub{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan []byte, 1024),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
		logger:     logger,
	}
}

func (h *MarketHub) Run() {
	for {
		select {
		case conn := <-h.register:
			h.mu.Lock()
			h.clients[conn] = true
			h.mu.Unlock()
			h.logger.Debug("websocket client registered")

		case conn := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[conn]; ok {
				delete(h.clients, conn)
				_ = conn.Close()
			}
			h.mu.Unlock()
			h.logger.Debug("websocket client disconnected")

		case message := <-h.broadcast:
			h.mu.RLock()
			for conn := range h.clients {
				err := conn.WriteMessage(websocket.TextMessage, message)
				if err != nil {
					h.logger.Debug("failed to write ws message, dropping client", "err", err)
					_ = conn.Close()
					delete(h.clients, conn)
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *MarketHub) BroadcastDepth(depth order.MarketDepthEvent) {
	data, err := json.Marshal(map[string]interface{}{
		"type": "MARKET_DEPTH",
		"data": depth,
	})
	if err == nil {
		select {
		case h.broadcast <- data:
		default:
			// Buffer full, drop oldest snapshot
		}
	}
}

func (h *MarketHub) BroadcastTrade(trade order.OrderMatchedEvent) {
	data, err := json.Marshal(map[string]interface{}{
		"type": "TRADE_TICK",
		"data": trade,
	})
	if err == nil {
		select {
		case h.broadcast <- data:
		default:
		}
	}
}

func (h *MarketHub) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("websocket upgrade error", "err", err)
		return
	}
	h.register <- conn

	// Keep alive & read pump
	go func() {
		defer func() {
			h.unregister <- conn
		}()
		conn.SetReadLimit(512)
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		conn.SetPongHandler(func(string) error {
			_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			return nil
		})
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	}()
}
