package server

import (
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// upgrader converts a regular HTTP connection into a WebSocket connection
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // allow all connections
	},
}

// Client represents a single connected WebSocket client
type Client struct {
	conn *websocket.Conn
	send chan []byte
}

// Server holds all connected clients
type Server struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	mu         sync.Mutex
}

// NewServer creates and returns a new Server
func NewServer() *Server {
	return &Server{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

// Run is the main event loop of the server
func (s *Server) Run() {
	for {
		select {
		case client := <-s.register:
			s.mu.Lock()
			s.clients[client] = true
			s.mu.Unlock()
			fmt.Println("New client connected. Total:", len(s.clients))

		case client := <-s.unregister:
			s.mu.Lock()
			if _, ok := s.clients[client]; ok {
				delete(s.clients, client)
				close(client.send)
			}
			s.mu.Unlock()
			fmt.Println("Client disconnected. Total:", len(s.clients))

		case message := <-s.broadcast:
			s.mu.Lock()
			for client := range s.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(s.clients, client)
				}
			}
			s.mu.Unlock()
		}
	}
}

// HandleConnection upgrades HTTP to WebSocket and manages a client's lifecycle
func (s *Server) HandleConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}

	client := &Client{conn: conn, send: make(chan []byte, 256)}
	s.register <- client

	// Run reading and writing in separate goroutines
	go client.writePump()
	go client.readPump(s)
}

// readPump reads messages from the client and sends them to broadcast
func (c *Client) readPump(s *Server) {
	defer func() {
		s.unregister <- c
		c.conn.Close()
	}()

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break // client disconnected or error
		}
		s.broadcast <- message
	}
}

// writePump sends messages from the send channel to the client
func (c *Client) writePump() {
	defer c.conn.Close()

	for message := range c.send {
		err := c.conn.WriteMessage(websocket.TextMessage, message)
		if err != nil {
			break
		}
	}
}

// Start launches the HTTP server
func (s *Server) Start(port string) {
	http.HandleFunc("/ws", s.HandleConnection)
	fmt.Println("Server started on port", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
