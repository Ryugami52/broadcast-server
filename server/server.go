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
	conn          *websocket.Conn
	send          chan []byte
	username      string
	authenticated bool
}

type Server struct {
	clients      map[*Client]bool
	userMap      map[string]*Client
	broadcast    chan []byte
	register     chan *Client
	unregister   chan *Client
	mu           sync.Mutex
	history      [][]byte
	userStore    *UserStore
	historyStore *HistoryStore
}

func NewServer(mongoURI, dbName string) *Server {
	store, client, err := NewUserStore(mongoURI, dbName)
	if err != nil {
		log.Fatal("Failed to connect to MongoDB:", err)
	}

	historyStore := NewHistoryStore(client, dbName)

	return &Server{
		clients:      make(map[*Client]bool),
		userMap:      make(map[string]*Client),
		broadcast:    make(chan []byte),
		register:     make(chan *Client),
		unregister:   make(chan *Client),
		history:      make([][]byte, 0, 10),
		userStore:    store,
		historyStore: historyStore,
	}
}

func (s *Server) Run() {
	for {
		select {
		case client := <-s.register:
			s.mu.Lock()
			s.clients[client] = true
			s.mu.Unlock()

			// Load public history from MongoDB
			messages, err := s.historyStore.GetLast("public", 10)
			if err == nil && len(messages) > 0 {
				client.send <- []byte("--- Last messages ---")
				for _, msg := range messages {
					line := fmt.Sprintf("[%s] %s", msg.Timestamp.Format("15:04:05"), msg.Content)
					client.send <- []byte(line)
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

		// Handle REGISTER:username:contact:password
		if strings.HasPrefix(text, "REGISTER:") {
			parts := strings.SplitN(text, ":", 4)
			if len(parts) != 4 {
				c.send <- []byte("SYSTEM:Invalid registration format.")
				continue
			}
			username, contact, password := parts[1], parts[2], parts[3]
			err := s.userStore.Register(username, contact, password)
			if err != nil {
				c.send <- []byte("SYSTEM:Registration failed: " + err.Error())
				continue
			}
			c.username = username
			c.authenticated = true
			s.mu.Lock()
			s.userMap[username] = c
			s.mu.Unlock()
			c.send <- []byte("AUTH_OK:" + username + ":Welcome! You are now registered and connected.")
			s.broadcast <- []byte(username + " has joined the chat!")
			continue
		}

		// Handle LOGIN:username:password
		if strings.HasPrefix(text, "LOGIN:") {
			parts := strings.SplitN(text, ":", 3)
			if len(parts) != 3 {
				c.send <- []byte("SYSTEM:Invalid login format.")
				continue
			}
			username, password := parts[1], parts[2]
			user, err := s.userStore.Login(username, password)
			if err != nil {
				c.send <- []byte("AUTH_FAIL:" + err.Error())
				continue
			}
			c.username = user.Username
			c.authenticated = true
			s.mu.Lock()
			s.userMap[user.Username] = c
			s.mu.Unlock()
			c.send <- []byte("AUTH_OK:" + user.Username + ":Welcome back " + user.Username + "!")
			s.broadcast <- []byte(user.Username + " has joined the chat!")
			continue
		}

		// Block unauthenticated clients
		if !c.authenticated {
			c.send <- []byte("SYSTEM:You must register or login first.")
			continue
		}

		// Handle LIST
		if text == "LIST:" {
			s.mu.Lock()
			users := []string{}
			for client := range s.clients {
				if client.username != "" {
					users = append(users, client.username)
				}
			}
			s.mu.Unlock()
			c.send <- []byte("SYSTEM:Online users: " + strings.Join(users, ", "))
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
					// Save to history
					room := "private:" + c.username + ":" + targetUsername
					go s.historyStore.Save(room, c.username, privateMsg)

					target.send <- []byte("PRIVATE_FROM:" + c.username + ":" + privateMsg)
					c.send <- []byte("PRIVATE_TO:" + targetUsername + ":" + privateMsg)
				}

			}
			continue
		}
		if strings.HasPrefix(text, "HISTORY:") {
			targetUsername := text[8:]
			room := "private:" + c.username + ":" + targetUsername
			// Also check reverse order
			messages, err := s.historyStore.GetLast(room, 20)
			if err == nil && len(messages) == 0 {
				room = "private:" + targetUsername + ":" + c.username
				messages, _ = s.historyStore.GetLast(room, 20)
			}
			if len(messages) > 0 {
				for _, msg := range messages {
					line := fmt.Sprintf("PRIVATE_HISTORY:%s:%s:%s",
						msg.Sender,
						msg.Timestamp.Format("15:04:05"),
						msg.Content)
					c.send <- []byte(line)
				}
			}
			continue
		}

		go s.historyStore.Save("public", c.username, text)
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
