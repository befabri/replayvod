// Package subscriptions adapts tRPC's subscription protocol to one WebSocket.
// Procedure dispatch, validation, and authorization remain owned by trpcgo.
package subscriptions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/befabri/trpcgo"
	"github.com/coder/websocket"
)

const maxSubscriptions = 32

// Handler serves only subscriptions. Queries and mutations retain the HTTP
// transport and its CSRF, batching, response-header, and keepalive behavior.
type Handler struct {
	router            *trpcgo.Router
	procedures        *trpcgo.ProcedureMap
	origins           map[string]bool
	mu                sync.Mutex
	connections       map[*websocket.Conn]context.CancelFunc
	closed            bool
	heartbeatInterval time.Duration
	heartbeatTimeout  time.Duration
}

func NewHandler(router *trpcgo.Router, origins []string) *Handler {
	h := &Handler{router: router, procedures: router.BuildProcedureMap(), origins: make(map[string]bool), connections: make(map[*websocket.Conn]context.CancelFunc)}
	h.heartbeatInterval = 25 * time.Second
	h.heartbeatTimeout = 10 * time.Second
	for _, origin := range origins {
		h.origins[origin] = true
	}
	return h
}

// Close cancels hijacked connections, which http.Server.Shutdown cannot close.
func (h *Handler) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for conn, cancel := range h.connections {
		cancel()
		_ = conn.CloseNow()
	}
	return nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// WebSocket upgrades are GETs, so HTTP's mutation CSRF protection alone is
	// insufficient. Match configured origins exactly; also allow same-host clients.
	if origin := r.Header.Get("Origin"); origin != "" && !h.origins[origin] {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host != r.Host || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			http.Error(w, "untrusted WebSocket origin", http.StatusForbidden)
			return
		}
	}
	// Origin was checked above, including scheme for configured external origins.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(64 << 10)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.connections[conn] = cancel
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.connections, conn); h.mu.Unlock() }()
	c := &connection{handler: h, conn: conn, ctx: ctx, cancel: cancel, request: r, active: make(map[string]context.CancelFunc)}
	defer c.workers.Wait()
	defer cancel()
	c.workers.Add(1)
	go func() {
		defer c.workers.Done()
		c.heartbeat()
	}()
	c.read()
}

// Protocol-level pings are answered by browsers independently of JavaScript
// timers and incoming application traffic. Keep reading concurrently so Ping
// can observe the pong; a failed probe cancels every feed on this connection.
func (c *connection) heartbeat() {
	ticker := time.NewTicker(c.handler.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(c.ctx, c.handler.heartbeatTimeout)
			err := c.conn.Ping(ctx)
			cancel()
			if err != nil {
				c.cancel()
				return
			}
		}
	}
}

type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params struct {
		Path        string          `json:"path"`
		Input       json.RawMessage `json:"input"`
		LastEventID string          `json:"lastEventId,omitempty"`
	} `json:"params"`
}

type connection struct {
	handler *Handler
	conn    *websocket.Conn
	ctx     context.Context
	cancel  context.CancelFunc
	request *http.Request
	mu      sync.Mutex
	active  map[string]context.CancelFunc
	workers sync.WaitGroup
}

func (c *connection) write(value any) bool {
	data, err := json.Marshal(value)
	if err != nil {
		c.cancel()
		return false
	}
	return c.writeBytes(data)
}

func (c *connection) writeBytes(data []byte) bool {
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageText, data); err != nil {
		c.cancel()
		return false
	}
	return true
}

func (c *connection) result(id json.RawMessage, kind string, data any, eventID string) bool {
	result := map[string]any{"type": kind}
	if kind == "data" {
		result["data"] = data
	}
	if eventID != "" {
		result["id"] = eventID
	}
	return c.write(map[string]any{"id": id, "result": result})
}

func (c *connection) fail(ctx context.Context, req request, err error) {
	safe := trpcgo.SanitizeError(err)
	if callback := c.handler.router.ErrorCallback(); callback != nil {
		var original *trpcgo.Error
		if !errors.As(err, &original) {
			original = trpcgo.WrapError(trpcgo.CodeInternalServerError, "internal server error", err)
		}
		callback(ctx, original, req.Params.Path)
	}
	// Use the same error formatter as HTTP; only the correlation ID is added.
	formatted := c.handler.router.FormatError(safe, req.Params.Path, req.Params.Input, ctx, trpcgo.ProcedureSubscription)
	raw, marshalErr := json.Marshal(formatted)
	if marshalErr != nil {
		c.cancel()
		return
	}
	var envelope struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(raw, &envelope) != nil || len(envelope.Error) == 0 {
		c.cancel()
		return
	}
	c.write(map[string]any{"id": req.ID, "error": envelope.Error})
}

func (c *connection) read() {
	for {
		kind, data, err := c.conn.Read(c.ctx)
		if err != nil {
			return
		}
		if kind != websocket.MessageText {
			return
		}
		if string(data) == "PING" {
			if !c.writeBytes([]byte("PONG")) {
				return
			}
			continue
		}
		data = bytes.TrimSpace(data)
		messages := []json.RawMessage{data}
		if len(data) > 0 && data[0] == '[' {
			if json.Unmarshal(data, &messages) != nil || len(messages) > maxSubscriptions {
				return
			}
		}
		for _, message := range messages {
			var req request
			if json.Unmarshal(message, &req) != nil {
				return
			}
			var valid bool
			req.ID, valid = normalizeID(req.ID)
			if !valid {
				return
			}
			c.handle(req)
		}
	}
}

// IDs are JSON values, not source spellings: escaped strings and equivalent
// numbers must identify the same operation for cancellation and duplicate checks.
func normalizeID(raw json.RawMessage) (json.RawMessage, bool) {
	var id any
	if json.Unmarshal(raw, &id) != nil {
		return nil, false
	}
	switch value := id.(type) {
	case string:
	case float64:
		if value == 0 {
			id = float64(0) // JavaScript treats negative zero as the same map key.
		}
	default:
		return nil, false
	}
	canonical, err := json.Marshal(id)
	return canonical, err == nil
}

func (c *connection) handle(req request) {
	key := string(req.ID)
	if req.Method == "subscription.stop" {
		c.mu.Lock()
		if cancel := c.active[key]; cancel != nil {
			cancel()
		}
		c.mu.Unlock()
		return
	}
	if req.Method != "subscription" {
		c.fail(c.ctx, req, trpcgo.NewError(trpcgo.CodeMethodNotSupported, "only subscriptions use this transport"))
		return
	}
	entry, ok := c.handler.procedures.Lookup(req.Params.Path)
	if !ok || entry.Type() != trpcgo.ProcedureSubscription {
		c.fail(c.ctx, req, trpcgo.NewError(trpcgo.CodeNotFound, "subscription not found"))
		return
	}
	c.mu.Lock()
	if _, exists := c.active[key]; exists {
		c.mu.Unlock()
		// Two operations cannot share a response ID. Close the connection so the
		// original operation cannot continue after an error terminates its client.
		c.cancel()
		return
	}
	if len(c.active) >= maxSubscriptions {
		c.mu.Unlock()
		c.fail(c.ctx, req, trpcgo.NewError(trpcgo.CodeTooManyRequests, "subscription limit reached"))
		return
	}
	ctx, cancel := context.WithCancel(c.ctx)
	c.active[key] = cancel
	c.mu.Unlock()
	c.workers.Add(1)
	go func() {
		defer c.workers.Done()
		defer cancel()
		defer func() { c.mu.Lock(); delete(c.active, key); c.mu.Unlock() }()
		defer func() {
			if recovered := recover(); recovered != nil {
				c.fail(ctx, req, fmt.Errorf("subscription panic: %v", recovered))
			}
		}()
		if create := c.handler.router.ContextCreator(); create != nil {
			ctx = create(ctx, c.request)
		}
		ctx = trpcgo.WithProcedureMeta(ctx, trpcgo.ProcedureMeta{Path: req.Params.Path, Type: entry.Type(), Meta: entry.Meta()})
		if req.Params.LastEventID != "" {
			var input map[string]json.RawMessage
			if len(req.Params.Input) > 0 && string(req.Params.Input) != "null" {
				if err := json.Unmarshal(req.Params.Input, &input); err != nil {
					c.fail(ctx, req, trpcgo.NewError(trpcgo.CodeBadRequest, "invalid resume input"))
					return
				}
			}
			if input == nil {
				input = make(map[string]json.RawMessage)
			}
			input["lastEventId"], _ = json.Marshal(req.Params.LastEventID)
			req.Params.Input, _ = json.Marshal(input)
		}
		output, err := c.handler.router.ExecuteEntry(ctx, entry, req.Params.Input)
		if err != nil {
			if ctx.Err() == nil {
				c.fail(ctx, req, err)
			}
			return
		}
		stream := trpcgo.ConsumeStream(output)
		if stream == nil {
			c.fail(ctx, req, errors.New("subscription returned no stream"))
			return
		}
		if !c.result(req.ID, "started", nil, "") {
			return
		}
		for {
			data, eventID, _, err := stream.Recv(ctx)
			if ctx.Err() != nil || err == io.EOF {
				c.result(req.ID, "stopped", nil, "")
				return
			}
			if err != nil {
				c.fail(ctx, req, err)
				return
			}
			if !c.result(req.ID, "data", data, eventID) {
				return
			}
		}
	}()
}
