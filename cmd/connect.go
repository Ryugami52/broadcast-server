package cmd

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
)

var serverAddr string

// mode tracks whether we are in public or private chat
var mode = "public"
var privateTarget = ""

// separate histories for public and private chats
var publicHistory []string
var privateHistory = map[string][]string{}

func clearScreen() {
	fmt.Print("\033[H\033[2J")
}

func printPublicHistory() {
	clearScreen()
	fmt.Println("=== Public Chat ===")
	fmt.Println("Type /msg <username> to open a private chat")
	fmt.Println("-------------------")
	for _, line := range publicHistory {
		fmt.Println(line)
	}
}

func printPrivateHistory(target string) {
	clearScreen()
	fmt.Printf("=== Private Chat with %s ===\n", target)
	fmt.Println("Type /exit to go back to public chat")
	fmt.Println("-------------------")
	for _, line := range privateHistory[target] {
		fmt.Println(line)
	}
}

var connectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Connect to the broadcast server",
	Run: func(cmd *cobra.Command, args []string) {
		// Ask username FIRST
		fmt.Print("Enter your username: ")
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Scan()
		username := scanner.Text()
		if username == "" {
			username = "Anonymous"
		}

		// Connect to server
		conn, _, err := websocket.DefaultDialer.Dial("ws://"+serverAddr+"/ws", nil)
		if err != nil {
			log.Fatal("Connection failed:", err)
		}
		defer conn.Close()

		// Send JOIN message
		conn.WriteMessage(websocket.TextMessage, []byte("JOIN:"+username))

		printPublicHistory()
		fmt.Printf("Connected as %s!\n", username)

		done := make(chan struct{})

		// Goroutine: receive and print incoming messages
		go func() {
			defer close(done)
			for {
				_, message, err := conn.ReadMessage()
				if err != nil {
					fmt.Println("Disconnected from server.")
					return
				}
				text := string(message)
				t := time.Now().Format("15:04:05")

				// Private message received from someone
				if strings.HasPrefix(text, "PRIVATE_FROM:") {
					parts := strings.SplitN(text, ":", 3)
					if len(parts) == 3 {
						from := parts[1]
						msg := parts[2]
						line := fmt.Sprintf(">> [%s] [%s]: %s", t, from, msg)
						privateHistory[from] = append(privateHistory[from], line)

						if mode == "private" && privateTarget == from {
							// We are already in their private chat, just print
							fmt.Println(line)
						} else {
							// Notify in whatever mode we are in
							fmt.Printf("\n** Private message from %s - type /msg %s to open **\n", from, from)
						}
					}
					continue
				}

				// Confirmation of our own private message sent
				if strings.HasPrefix(text, "PRIVATE_TO:") {
					parts := strings.SplitN(text, ":", 3)
					if len(parts) == 3 {
						to := parts[1]
						msg := parts[2]
						line := fmt.Sprintf(">> [%s] [You -> %s]: %s", t, to, msg)
						privateHistory[to] = append(privateHistory[to], line)

						if mode == "private" && privateTarget == to {
							fmt.Println(line)
						}
					}
					continue
				}

				// System message (e.g. user not found)
				if strings.HasPrefix(text, "SYSTEM:") {
					fmt.Println("**", text[7:], "**")
					continue
				}

				// Public message
				line := fmt.Sprintf(">> [%s] %s", t, text)
				publicHistory = append(publicHistory, line)

				if mode == "public" {
					fmt.Println(line)
				} else {
					// In private mode, just notify
					fmt.Printf("\n** New public message - type /exit to go back **\n")
				}
			}
		}()

		// Main goroutine: read user input
		for scanner.Scan() {
			text := scanner.Text()
			if text == "" {
				continue
			}

			// Switch to private chat
			if strings.HasPrefix(text, "/msg ") {
				target := strings.TrimPrefix(text, "/msg ")
				target = strings.TrimSpace(target)
				if target == "" {
					fmt.Println("Usage: /msg <username>")
					continue
				}
				mode = "private"
				privateTarget = target
				printPrivateHistory(target)
				continue
			}

			// Exit private chat back to public
			if text == "/exit" {
				mode = "public"
				privateTarget = ""
				printPublicHistory()
				continue
			}

			// Send message based on current mode
			if mode == "private" {
				msg := fmt.Sprintf("PRIVATE:%s:%s", privateTarget, text)
				err := conn.WriteMessage(websocket.TextMessage, []byte(msg))
				if err != nil {
					log.Println("Send error:", err)
					break
				}
			} else {
				message := fmt.Sprintf("[%s]: %s", username, text)
				err := conn.WriteMessage(websocket.TextMessage, []byte(message))
				if err != nil {
					log.Println("Send error:", err)
					break
				}
			}
		}

		conn.Close()
		<-done
	},
}

func init() {
	connectCmd.Flags().StringVarP(&serverAddr, "address", "a", "localhost:8080", "Server address")
	rootCmd.AddCommand(connectCmd)
}
