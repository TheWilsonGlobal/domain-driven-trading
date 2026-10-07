package http

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/vortex-trading/vortex-trading-engine/internal/app/command"
	"github.com/vortex-trading/vortex-trading-engine/internal/app/query"
	"github.com/vortex-trading/vortex-trading-engine/internal/domain/order"
)

type HTTPHandler struct {
	placeOrderHandler  *command.PlaceOrderHandler
	cancelOrderHandler *command.CancelOrderHandler
	orderBookHandler   *query.GetOrderBookHandler
	balanceHandler     *query.GetBalanceHandler
}

func NewHTTPHandler(
	placeOrderHandler *command.PlaceOrderHandler,
	cancelOrderHandler *command.CancelOrderHandler,
	orderBookHandler *query.GetOrderBookHandler,
	balanceHandler *query.GetBalanceHandler,
) *HTTPHandler {
	return &HTTPHandler{
		placeOrderHandler:  placeOrderHandler,
		cancelOrderHandler: cancelOrderHandler,
		orderBookHandler:   orderBookHandler,
		balanceHandler:     balanceHandler,
	}
}

type PlaceOrderDTO struct {
	ClientOrderID string `json:"client_order_id"`
	AccountID     string `json:"account_id"`
	Symbol        string `json:"symbol"`
	Side          string `json:"side"` // BUY or SELL
	OrderType     string `json:"order_type"` // LIMIT or MARKET
	Price         int64  `json:"price"` // VND
	Quantity      uint32 `json:"quantity"`
}

func (h *HTTPHandler) Router() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Idempotency-Key"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "healthy",
			"engine": "vortex-trading-core",
			"mode":   "in-memory FIFO matching + EDA",
		})
	})

	r.Route("/v1", func(r chi.Router) {
		r.Post("/orders", h.handlePlaceOrder)
		r.Delete("/orders/{orderID}", h.handleCancelOrder)
		r.Get("/orderbook", h.handleGetOrderBook)
		r.Get("/balance/{accountID}", h.handleGetBalance)
	})

	return r
}

func (h *HTTPHandler) handlePlaceOrder(w http.ResponseWriter, r *http.Request) {
	var dto PlaceOrderDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	cmd := command.PlaceOrderCommand{
		ClientOrderID: dto.ClientOrderID,
		AccountID:     dto.AccountID,
		Symbol:        order.Symbol(dto.Symbol),
		Side:          order.OrderSide(dto.Side),
		Type:          order.OrderType(dto.OrderType),
		Price:         order.Price(dto.Price),
		Quantity:      order.Quantity(dto.Quantity),
	}

	result, err := h.placeOrderHandler.Handle(r.Context(), cmd)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"order":   result.Order,
		"matches": result.Matches,
	})
}

func (h *HTTPHandler) handleCancelOrder(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "orderID")
	accountID := r.URL.Query().Get("account_id")
	symbol := r.URL.Query().Get("symbol")

	cmd := command.CancelOrderCommand{
		OrderID:   orderID,
		AccountID: accountID,
		Symbol:    order.Symbol(symbol),
	}

	canceled, err := h.cancelOrderHandler.Handle(r.Context(), cmd)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(canceled)
}

func (h *HTTPHandler) handleGetOrderBook(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		symbol = "VPB"
	}
	levels := 10
	if lStr := r.URL.Query().Get("levels"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil {
			levels = l
		}
	}

	depth := h.orderBookHandler.Handle(r.Context(), query.GetOrderBookQuery{
		Symbol:    order.Symbol(symbol),
		MaxLevels: levels,
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(depth)
}

func (h *HTTPHandler) handleGetBalance(w http.ResponseWriter, r *http.Request) {
	accountID := chi.URLParam(r, "accountID")
	acc, err := h.balanceHandler.Handle(r.Context(), query.GetBalanceQuery{AccountID: accountID})
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(acc)
}
