package server

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type Client struct {
	conn     *websocket.Conn
	send     chan []byte
	username string
}

type Server struct {
	clients    map[*Client]bool
	userMap    map[string]*Client // username -> client
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	mu         sync.Mutex
	history    [][]byte
}

func NewServer() *Server {
	return &Server{
		clients:    make(map[*Client]bool),
		userMap:    make(map[string]*Client),
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		history:    make([][]byte, 0, 10),
	}
}

func (s *Server) Run() {
	for {
		select {
		case client := <-s.register:
			s.mu.Lock()
			s.clients[client] = true
			s.mu.Unlock()

			if len(s.history) > 0 {
				client.send <- []byte("--- Last messages ---")
				for _, msg := range s.history {
					client.send <- msg
				}
				client.send <- []byte("--- End of history ---")
			}

			fmt.Println("New client connected. Total:", len(s.clients))

		case client := <-s.unregister:
			s.mu.Lock()
			if _, ok := s.clients[client]; ok {
				delete(s.clients, client)
				if client.username != "" {
					delete(s.userMap, client.username)
				}
				close(client.send)
			}
			s.mu.Unlock()
			fmt.Println("Client disconnected. Total:", len(s.clients))

		case message := <-s.broadcast:
			s.history = append(s.history, message)
			if len(s.history) > 10 {
				s.history = s.history[1:]
			}

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

func (s *Server) HandleConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}

	client := &Client{conn: conn, send: make(chan []byte, 256)}
	s.register <- client

	go client.writePump()
	go client.readPump(s)
}

func (c *Client) readPump(s *Server) {
	defer func() {
		if c.username != "" {
			s.broadcast <- []byte(c.username + " has left the chat.")
		}
		s.unregister <- c
		c.conn.Close()
	}()

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}

		text := string(message)

		// Handle JOIN
		if strings.HasPrefix(text, "JOIN:") {
			c.username = text[5:]
			s.mu.Lock()
			s.userMap[c.username] = c
			s.mu.Unlock()
			s.broadcast <- []byte(c.username + " has joined the chat!")
			continue
		}

		// Handle PRIVATE:targetUser:message
		if strings.HasPrefix(text, "PRIVATE:") {
			parts := strings.SplitN(text, ":", 3)
			if len(parts) == 3 {
				targetUsername := parts[1]
				privateMsg := parts[2]

				s.mu.Lock()
				target, ok := s.userMap[targetUsername]
				s.mu.Unlock()

				if ok {
					// Send to target
					target.send <- []byte("PRIVATE_FROM:" + c.username + ":" + privateMsg)
					// Send back to sender as confirmation
					c.send <- []byte("PRIVATE_TO:" + targetUsername + ":" + privateMsg)
				} else {
					// User not found
					c.send <- []byte("SYSTEM:User " + targetUsername + " not found or offline.")
				}
			}
			continue
		}

		s.broadcast <- message
	}
}

func (c *Client) writePump() {
	defer c.conn.Close()

	for message := range c.send {
		err := c.conn.WriteMessage(websocket.TextMessage, message)
		if err != nil {
			break
		}
	}
}

func (s *Server) Start(port string) {
	http.HandleFunc("/ws", s.HandleConnection)
	fmt.Println("Server started on port", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
