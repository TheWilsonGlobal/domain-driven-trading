package websocket

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
	"github.com/gorilla/websocket"
)

// Options tunes the hub. Zero values select the defaults below.
type Options struct {
	// AllowedOrigins is the exact-match Origin allowlist. A request without an
	// Origin header (a non-browser client) is accepted; any other Origin that
	// is not listed is refused with 403. Empty list = no browser origin allowed.
	AllowedOrigins []string
	MaxClients     int           // default 1000; beyond it the upgrade is refused with 503
	SendBuffer     int           // per-client queue, default 64; a full queue drops messages
	WriteTimeout   time.Duration // per write, default 10s
	PongWait       time.Duration // read deadline, extended by every pong; default 60s
	PingPeriod     time.Duration // default PongWait * 9/10
}

const (
	defaultMaxClients   = 1000
	defaultSendBuffer   = 64
	defaultWriteTimeout = 10 * time.Second
	defaultPongWait     = 60 * time.Second
)

func (o Options) withDefaults() Options {
	if o.MaxClients <= 0 {
		o.MaxClients = defaultMaxClients
	}
	if o.SendBuffer <= 0 {
		o.SendBuffer = defaultSendBuffer
	}
	if o.WriteTimeout <= 0 {
		o.WriteTimeout = defaultWriteTimeout
	}
	if o.PongWait <= 0 {
		o.PongWait = defaultPongWait
	}
	if o.PingPeriod <= 0 {
		o.PingPeriod = o.PongWait * 9 / 10
	}
	return o
}

// client owns one connection. Only its writePump writes to conn, so a slow
// peer can only ever stall its own goroutine, never the hub.
type client struct {
	conn *websocket.Conn
	send chan []byte
}

type MarketHub struct {
	clients   map[*client]struct{}
	pending   int // upgrades in flight, counted against the cap
	broadcast chan []byte
	mu        sync.RWMutex
	logger    *slog.Logger
	opt       Options
	upgrader  websocket.Upgrader
	dropped   atomic.Uint64
}

func NewMarketHub(logger *slog.Logger) *MarketHub {
	h := &MarketHub{
		clients:   make(map[*client]struct{}),
		broadcast: make(chan []byte, 1024),
		logger:    logger,
	}
	return h.Configure(Options{})
}

// Configure applies the options. Call it before the hub serves connections.
func (h *MarketHub) Configure(opt Options) *MarketHub {
	h.opt = opt.withDefaults()
	h.upgrader = websocket.Upgrader{CheckOrigin: h.originAllowed}
	return h
}

func (h *MarketHub) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	for _, a := range h.opt.AllowedOrigins {
		if a == origin {
			return true
		}
	}
	return false
}

// Run fans broadcast messages out to the clients. It never blocks on a
// client: a full send queue drops the message for that client only.
func (h *MarketHub) Run() {
	for message := range h.broadcast {
		h.mu.RLock()
		for c := range h.clients {
			select {
			case c.send <- message:
			default:
				h.dropped.Add(1)
			}
		}
		h.mu.RUnlock()
	}
}

// ClientCount returns the number of currently registered websocket clients.
func (h *MarketHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Dropped is the number of messages discarded because a client queue was full.
func (h *MarketHub) Dropped() uint64 { return h.dropped.Load() }

func (h *MarketHub) BroadcastDepth(depth order.MarketDepthEvent) {
	h.enqueue("MARKET_DEPTH", depth)
}

func (h *MarketHub) BroadcastTrade(trade order.OrderMatchedEvent) {
	h.enqueue("TRADE_TICK", trade)
}

func (h *MarketHub) enqueue(kind string, payload interface{}) {
	data, err := json.Marshal(map[string]interface{}{"type": kind, "data": payload})
	if err != nil {
		return
	}
	select {
	case h.broadcast <- data:
	default:
		h.dropped.Add(1) // hub buffer full: drop this snapshot
	}
}

// addClient registers conn and starts its pumps.
func (h *MarketHub) addClient(conn *websocket.Conn) *client {
	c := &client{conn: conn, send: make(chan []byte, h.opt.SendBuffer)}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	go h.writePump(c)
	go h.readPump(c)
	return c
}

// remove unregisters c and closes its connection; it is safe to call twice.
// The queue is closed under the write lock, so Run (read lock) never sends on
// a closed channel.
func (h *MarketHub) remove(c *client) {
	h.mu.Lock()
	_, ok := h.clients[c]
	if ok {
		delete(h.clients, c)
		close(c.send)
	}
	h.mu.Unlock()
	if ok && c.conn != nil {
		_ = c.conn.Close()
		h.logger.Debug("websocket client disconnected")
	}
}

func (h *MarketHub) writePump(c *client) {
	ticker := time.NewTicker(h.opt.PingPeriod)
	defer ticker.Stop()
	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				return // removed
			}
			_ = c.conn.SetWriteDeadline(time.Now().Add(h.opt.WriteTimeout))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				h.logger.Debug("failed to write ws message, dropping client", "err", err)
				h.remove(c)
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(h.opt.WriteTimeout))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				h.remove(c)
				return
			}
		}
	}
}

// readPump discards inbound frames (the feed is one-way); its job is to
// process control frames and enforce the pong deadline.
func (h *MarketHub) readPump(c *client) {
	defer h.remove(c)
	c.conn.SetReadLimit(512)
	_ = c.conn.SetReadDeadline(time.Now().Add(h.opt.PongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(h.opt.PongWait))
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (h *MarketHub) HandleWS(w http.ResponseWriter, r *http.Request) {
	// Reject a foreign Origin before anything else (cross-site hijacking).
	if !h.originAllowed(r) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	h.mu.Lock()
	if len(h.clients)+h.pending >= h.opt.MaxClients {
		h.mu.Unlock()
		http.Error(w, "too many connections", http.StatusServiceUnavailable)
		return
	}
	h.pending++
	h.mu.Unlock()

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.mu.Lock()
		h.pending--
		h.mu.Unlock()
		h.logger.Debug("websocket upgrade error", "err", err)
		return
	}
	h.addClient(conn)
	h.mu.Lock()
	h.pending--
	h.mu.Unlock()
}
