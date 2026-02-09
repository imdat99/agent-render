package http

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"picpic.render/internal/core/ports"
)

// Message represents a message to be sent to connected clients
type Message struct {
	Type    string      `json:"type"` // "log", "job_update", "agent_update"
	Payload interface{} `json:"payload"`
}

type Hub struct {
	// Registered clients.
	clients map[*Client]bool

	// Inbound messages from the system to be broadcasted to clients.
	broadcast chan Message

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client

	// Lock for client map safety
	mu sync.Mutex

	pubsub ports.LogPubSub
}

func NewHub(pubsub ports.LogPubSub) *Hub {
	return &Hub{
		broadcast:  make(chan Message),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
		pubsub:     pubsub,
	}
}

func (h *Hub) Run() {
	// Start listening to Redis PubSub for logs
	// This is a bit tricky because SubscribeLogs is blocking or returns a channel.
	// We'll trust the caller to setup the pubsub subscription consumption elsewhere
	// or we start a goroutine here.
	// For now, let's assume we receive messages via Broadcast() method calls from the Service layer,
	// OR we subscribe to redis here.
	// Let's implement a listener loop if the interface supports it.

	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
		case message := <-h.broadcast:
			h.mu.Lock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.Unlock()
		}
	}
}

// Broadcast publishes a message to all connected clients
func (h *Hub) Broadcast(msg Message) {
	h.broadcast <- msg
}

// LogConsumer consumes logs from the PubSub port and broadcasts them
func (h *Hub) LogConsumer(ctx context.Context) {
	// Subscribe to all logs (empty jobID means subscribe to all jobs)
	logCh, err := h.pubsub.Subscribe(ctx, "")
	if err != nil {
		log.Printf("Failed to subscribe to logs: %v", err)
		return
	}

	log.Println("LogConsumer started, listening for logs from Redis PubSub...")

	for {
		select {
		case <-ctx.Done():
			log.Println("LogConsumer shutting down...")
			return
		case entry, ok := <-logCh:
			if !ok {
				log.Println("Log channel closed, LogConsumer exiting")
				return
			}
			// Broadcast log entry to all connected WebSocket clients
			h.Broadcast(Message{
				Type:    "log",
				Payload: entry,
			})
		}
	}
}

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all for dev
	},
}

// Client is a middleman between the websocket connection and the hub.
type Client struct {
	hub *Hub

	// The websocket connection.
	conn *websocket.Conn

	// Buffered channel of outbound messages.
	send chan Message
}

// writePump pumps messages from the hub to the websocket connection.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			json.NewEncoder(w).Encode(message)

			// Add queued chat messages to the current websocket message.
			n := len(c.send)
			for i := 0; i < n; i++ {
				json.NewEncoder(w).Encode(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ServeWs handles websocket requests from the peer.
func ServeWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	client := &Client{hub: hub, conn: conn, send: make(chan Message, 256)}
	client.hub.register <- client

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go client.writePump()
}
