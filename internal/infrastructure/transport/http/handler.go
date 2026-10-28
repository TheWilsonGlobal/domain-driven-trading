package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/command"
	"github.com/domain-driven-trading/domain-driven-trading/internal/app/identity"
	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
	"github.com/domain-driven-trading/domain-driven-trading/internal/app/query"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

type HTTPHandler struct {
	placeOrderHandler  *command.PlaceOrderHandler
	cancelOrderHandler *command.CancelOrderHandler
	orderBookHandler   *query.GetOrderBookHandler
	balanceHandler     *query.GetBalanceHandler
	authenticate       func(http.Handler) http.Handler
	idem               ports.IdempotencyStore // optional, see WithIdempotency
	cfg                Config
}

// WithConfig replaces the hardening configuration (default: DefaultConfig).
func (h *HTTPHandler) WithConfig(cfg Config) *HTTPHandler {
	h.cfg = cfg
	return h
}

func NewHTTPHandler(
	placeOrderHandler *command.PlaceOrderHandler,
	cancelOrderHandler *command.CancelOrderHandler,
	orderBookHandler *query.GetOrderBookHandler,
	balanceHandler *query.GetBalanceHandler,
	authenticate func(http.Handler) http.Handler,
) *HTTPHandler {
	return &HTTPHandler{
		placeOrderHandler:  placeOrderHandler,
		cancelOrderHandler: cancelOrderHandler,
		orderBookHandler:   orderBookHandler,
		balanceHandler:     balanceHandler,
		authenticate:       authenticate,
		cfg:                DefaultConfig(),
	}
}

type PlaceOrderDTO struct {
	ClientOrderID string `json:"client_order_id"`
	// AccountID is accepted for backward compatibility and IGNORED: the order
	// is always placed for the authenticated account.
	AccountID string `json:"account_id"`
	Symbol    string `json:"symbol"`
	Side      string `json:"side"`       // BUY or SELL
	OrderType string `json:"order_type"` // LIMIT or MARKET
	Price     int64  `json:"price"`      // VND
	Quantity  uint32 `json:"quantity"`
}

func (h *HTTPHandler) Router() http.Handler {
	r := chi.NewRouter()

	r.Use(SecurityHeaders)
	r.Use(middleware.RequestID)
	// X-Forwarded-For / X-Real-IP are client-controlled: only trusted behind a
	// proxy we operate (TRUSTED_PROXY=true), otherwise the peer address counts.
	if h.cfg.TrustedProxy {
		r.Use(middleware.RealIP)
	}
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   h.cfg.AllowedOrigins, // exact match, never "*"
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", IdempotencyHeader},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false, // bearer-token API: no cookies
		MaxAge:           300,
	}))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "healthy",
			"engine": "domain-driven-trading-core",
			"mode":   "in-memory FIFO matching + EDA",
		})
	})

	r.Route("/v1", func(r chi.Router) {
		// Every /v1 route is authenticated; the caller's account id comes from
		// the verified identity only, never from the body or the query string.
		// Rate limiting runs first so unauthenticated floods are limited too.
		r.Use(noStore)
		r.Use(rateLimit(h.cfg.RateReadPerSec, h.cfg.RateWritePerSec))
		r.Use(h.authenticate)
		r.Post("/orders", h.handlePlaceOrder)
		r.Delete("/orders/{orderID}", h.handleCancelOrder)
		r.Get("/orderbook", h.handleGetOrderBook)
		r.Get("/balance/{accountID}", h.handleGetBalance)
	})

	return r
}

// maxBodyBytes caps a request body; a PlaceOrderDTO is a few hundred bytes.
const maxBodyBytes = 4 << 10

const (
	minLevels     = 1
	maxLevels     = query.MaxOrderBookLevels
	defaultLevels = query.DefaultOrderBookLevels
)

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// domainErrStatus maps a domain error to a status and a fixed message. Unknown
// errors (repository outage, publish failure, ...) are a generic 500 with the
// fixed text "internal error" (known=false); the raw error is only logged.
func domainErrStatus(err error) (status int, msg string, known bool) {
	switch {
	case errors.Is(err, common.ErrPartiallyExecuted):
		// Checked first: the wrapped cause may itself be a "known" rejection
		// (e.g. a maker account lookup), but the order already executed.
		return http.StatusInternalServerError, "internal error", false
	case errors.Is(err, common.ErrInvalidSide):
		return http.StatusBadRequest, "side must be BUY or SELL", true
	case errors.Is(err, common.ErrInvalidOrderType):
		return http.StatusBadRequest, "order_type must be LIMIT or MARKET", true
	case errors.Is(err, common.ErrInvalidQuantity):
		return http.StatusBadRequest, "quantity out of range", true
	case errors.Is(err, common.ErrInvalidPrice):
		return http.StatusBadRequest, "price out of range", true
	case errors.Is(err, common.ErrInvalidTickSize):
		return http.StatusBadRequest, "price is not a multiple of the tick size", true
	case errors.Is(err, common.ErrInvalidSymbol), errors.Is(err, common.ErrSymbolMismatch):
		return http.StatusBadRequest, "unknown or malformed symbol", true
	case errors.Is(err, common.ErrInvalidAccountID):
		return http.StatusBadRequest, "malformed account_id", true
	case errors.Is(err, common.ErrInvalidClientOrderID):
		return http.StatusBadRequest, "malformed client_order_id", true
	case errors.Is(err, common.ErrInvalidOrderID):
		return http.StatusBadRequest, "malformed order id", true
	case errors.Is(err, common.ErrArithmeticOverflow):
		return http.StatusBadRequest, "order value out of range", true
	case errors.Is(err, common.ErrNoLiquidity):
		return http.StatusUnprocessableEntity, "no liquidity for market order", true
	case errors.Is(err, common.ErrInsufficientBalance):
		return http.StatusUnprocessableEntity, "insufficient balance", true
	case errors.Is(err, common.ErrInsufficientHolding):
		return http.StatusUnprocessableEntity, "insufficient holding", true
	case errors.Is(err, common.ErrDuplicateClientOrderID), errors.Is(err, common.ErrOrderAlreadyExists):
		return http.StatusConflict, "duplicate order", true
	case errors.Is(err, common.ErrFeeCollectorMissing):
		return http.StatusInternalServerError, "internal error", true
	case errors.Is(err, common.ErrAccountNotFound):
		return http.StatusNotFound, "account not found", true
	case errors.Is(err, common.ErrOrderNotFound):
		return http.StatusNotFound, "order not found", true
	case errors.Is(err, common.ErrOrderCompleted):
		return http.StatusConflict, "order already completed", true
	default:
		return http.StatusInternalServerError, "internal error", false
	}
}

func writeDomainErr(w http.ResponseWriter, err error) {
	status, msg, known := domainErrStatus(err)
	if !known || status >= http.StatusInternalServerError {
		slog.Error("request failed", "error", err)
	}
	writeErr(w, status, msg)
}

func isJSONContentType(r *http.Request) bool {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mt == "application/json"
}

// callerID returns the authenticated account id. The middleware guarantees it
// on /v1; if it is somehow absent the request is refused, never defaulted.
func callerID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id, ok := identity.AccountID(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
	}
	return id, ok
}

func (h *HTTPHandler) handlePlaceOrder(w http.ResponseWriter, r *http.Request) {
	caller, ok := callerID(w, r)
	if !ok {
		return
	}
	if !isJSONContentType(r) {
		writeErr(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var dto PlaceOrderDTO
	if err := dec.Decode(&dto); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeErr(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeErr(w, http.StatusBadRequest, "malformed JSON body")
		return
	}
	// Exactly one JSON value is allowed.
	if _, err := dec.Token(); err != io.EOF {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeErr(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeErr(w, http.StatusBadRequest, "malformed JSON body")
		return
	}

	// The idempotency key is the X-Idempotency-Key header or the body's
	// client_order_id; when both are present they must agree.
	if h.idem != nil {
		hdr := strings.TrimSpace(r.Header.Get(IdempotencyHeader))
		if hdr != "" && dto.ClientOrderID != "" && hdr != dto.ClientOrderID {
			writeErr(w, http.StatusBadRequest, "X-Idempotency-Key and client_order_id differ")
			return
		}
		if dto.ClientOrderID == "" {
			dto.ClientOrderID = hdr
		}
		if dto.ClientOrderID == "" {
			writeErr(w, http.StatusBadRequest, "idempotency key required: send X-Idempotency-Key or client_order_id")
			return
		}
	}

	cmd := command.PlaceOrderCommand{
		ClientOrderID: dto.ClientOrderID,
		AccountID:     caller,
		Symbol:        order.Symbol(dto.Symbol),
		Side:          order.OrderSide(dto.Side),
		Type:          order.OrderType(dto.OrderType),
		Price:         order.Price(dto.Price),
		Quantity:      order.Quantity(dto.Quantity),
	}

	// Validation happens before the key is claimed: a malformed request never
	// burns a key.
	if err := cmd.Validate(); err != nil {
		writeDomainErr(w, err)
		return
	}

	if h.idem == nil {
		result, err := h.placeOrderHandler.Handle(r.Context(), cmd)
		if err != nil {
			writeDomainErr(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, placeResponse(result))
		return
	}
	h.placeIdempotent(w, r, caller, cmd)
}

// FillDTO is one execution as the TAKER sees it. The counterparty (maker
// account / order id / remaining / unlock) and internal reservation shares are
// deliberately absent.
type FillDTO struct {
	TradeID        string    `json:"trade_id"`
	Symbol         string    `json:"symbol"`
	TakerOrderID   string    `json:"taker_order_id"`
	TakerAccountID string    `json:"taker_account_id"`
	TakerSide      string    `json:"taker_side"`
	Price          int64     `json:"price"`
	Quantity       uint32    `json:"quantity"`
	TakerRemaining uint32    `json:"taker_remaining"`
	Timestamp      time.Time `json:"timestamp"`
}

func placeResponse(result *command.PlaceOrderResult) map[string]interface{} {
	fills := make([]FillDTO, 0, len(result.Matches))
	for _, m := range result.Matches {
		fills = append(fills, FillDTO{
			TradeID:        m.TradeID,
			Symbol:         string(m.Symbol),
			TakerOrderID:   m.TakerOrderID,
			TakerAccountID: m.TakerAccountID,
			TakerSide:      string(m.TakerSide),
			Price:          int64(m.Price),
			Quantity:       uint32(m.Quantity),
			TakerRemaining: uint32(m.TakerRemaining),
			Timestamp:      m.Timestamp,
		})
	}
	return map[string]interface{}{"order": result.Order, "matches": fills}
}

// placeIdempotent runs the command at most once per idem:{account}:{clientOrderId}.
//   - the first request claims the key, executes, stores status+body, answers;
//   - an identical request meanwhile waits (briefly) and gets the same answer;
//   - a replay after completion returns the ORIGINAL status and body;
//   - the same key with a different request is a 422;
//   - a rejected request (known domain error) releases the key so it can be
//     retried; an indeterminate failure (e.g. storage) keeps the key and stores
//     the failure, because the order may have partly executed and a silent
//     re-run could fill twice;
//   - a store error fails the request with 503 and claims nothing.
func (h *HTTPHandler) placeIdempotent(w http.ResponseWriter, r *http.Request, caller string, cmd command.PlaceOrderCommand) {
	key := "idem:" + caller + ":" + cmd.ClientOrderID
	fp := fingerprint(cmd)
	ctx := r.Context()

	deadline := time.Now().Add(idemWait)
	for {
		claimed, rec, err := h.idem.Claim(ctx, key, fp, idemPendingTTL)
		if err != nil {
			slog.Error("idempotency store unavailable", "error", err)
			writeErr(w, http.StatusServiceUnavailable, "service temporarily unavailable")
			return
		}
		if claimed {
			break
		}
		if rec.Fingerprint != fp {
			writeErr(w, http.StatusUnprocessableEntity, "idempotency key already used with a different request")
			return
		}
		if rec.Done {
			w.Header().Set("Idempotent-Replay", "true")
			writeRaw(w, rec.Status, rec.Body)
			return
		}
		if time.Now().After(deadline) {
			w.Header().Set("Retry-After", "1")
			writeErr(w, http.StatusConflict, "a request with this idempotency key is still in progress")
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Millisecond):
		}
	}

	// From here this request owns the key and must complete or release it,
	// even if the client disconnects.
	bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	finished := false
	defer func() {
		if !finished {
			if err := h.idem.Release(bg, key); err != nil {
				slog.Error("idempotency release failed", "error", err)
			}
		}
	}()

	result, err := h.placeOrderHandler.Handle(ctx, cmd)
	status, body := http.StatusCreated, []byte(nil)
	if err != nil {
		st, msg, known := domainErrStatus(err)
		if known {
			writeErr(w, st, msg)
			return // released by the deferred call
		}
		slog.Error("place order failed", "error", err)
		status, body = st, mustJSON(map[string]string{"error": msg})
	} else {
		body = mustJSON(placeResponse(result))
	}
	finished = true // keep the key; on a store error the claim lapses by TTL
	if err := h.idem.Complete(bg, key, ports.IdempotencyRecord{Fingerprint: fp, Status: status, Body: body}, idemDoneTTL); err != nil {
		slog.Error("idempotency complete failed; replays of this key stay in-flight until the claim lapses", "error", err)
	}
	writeRaw(w, status, body)
}

const (
	// IdempotencyHeader is the alternative to the body's client_order_id.
	IdempotencyHeader = "X-Idempotency-Key"

	idemPendingTTL = 2 * time.Minute
	idemDoneTTL    = 24 * time.Hour
	idemWait       = 3 * time.Second
)

// WithIdempotency enables idempotent order placement (and makes the key
// mandatory on POST /v1/orders). Without a store the key is optional and
// nothing is deduplicated; the gateway always sets one.
func (h *HTTPHandler) WithIdempotency(store ports.IdempotencyStore) *HTTPHandler {
	h.idem = store
	return h
}

// fingerprint identifies the request content so a reused key with a different
// request is detected.
func fingerprint(c command.PlaceOrderCommand) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%s|%d|%d", c.AccountID, c.Symbol, c.Side, c.Type, c.Price, c.Quantity)))
	return hex.EncodeToString(sum[:])
}

func mustJSON(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"error":"internal error"}`)
	}
	return append(b, '\n')
}

func writeRaw(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (h *HTTPHandler) handleCancelOrder(w http.ResponseWriter, r *http.Request) {
	caller, ok := callerID(w, r)
	if !ok {
		return
	}
	cmd := command.CancelOrderCommand{
		OrderID:   chi.URLParam(r, "orderID"),
		AccountID: caller,
		Symbol:    order.Symbol(r.URL.Query().Get("symbol")),
	}

	canceled, err := h.cancelOrderHandler.Handle(r.Context(), cmd)
	if err != nil {
		writeDomainErr(w, err)
		return
	}

	writeJSON(w, http.StatusOK, canceled)
}

// handleGetOrderBook: levels is clamped to 1..50 (default 10, non-numeric ->
// 400). The only book is the engine's; any other symbol is a 404.
func (h *HTTPHandler) handleGetOrderBook(w http.ResponseWriter, r *http.Request) {
	levels := defaultLevels
	if lStr := r.URL.Query().Get("levels"); lStr != "" {
		l, err := strconv.Atoi(lStr)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "levels must be an integer")
			return
		}
		if l < minLevels {
			l = minLevels
		}
		if l > maxLevels {
			l = maxLevels
		}
		levels = l
	}

	// Empty symbol means the book's own symbol (the query handler resolves it).
	depth, err := h.orderBookHandler.Handle(r.Context(), query.GetOrderBookQuery{
		Symbol:    order.Symbol(r.URL.Query().Get("symbol")),
		MaxLevels: levels,
	})
	if err != nil {
		if errors.Is(err, common.ErrSymbolMismatch) {
			writeErr(w, http.StatusNotFound, "unknown symbol")
			return
		}
		writeErr(w, http.StatusBadRequest, "request rejected")
		return
	}

	writeJSON(w, http.StatusOK, depth)
}

// handleGetBalance keeps the /balance/{accountID} shape, but the id must be the
// caller's own: any other id is a 404 identical to an unknown account.
func (h *HTTPHandler) handleGetBalance(w http.ResponseWriter, r *http.Request) {
	caller, ok := callerID(w, r)
	if !ok {
		return
	}
	if chi.URLParam(r, "accountID") != caller {
		writeErr(w, http.StatusNotFound, "account not found")
		return
	}
	acc, err := h.balanceHandler.Handle(r.Context(), query.GetBalanceQuery{AccountID: caller})
	if err != nil {
		if errors.Is(err, common.ErrAccountNotFound) {
			writeErr(w, http.StatusNotFound, "account not found")
			return
		}
		slog.Error("balance lookup failed", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, acc)
}
