package websocket_test

import (
	"net/http"

	wsTransport "github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/transport/websocket"
)

func httpHandlerFunc(h *wsTransport.MarketHub) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws/market", h.HandleWS)
	return mux
}
