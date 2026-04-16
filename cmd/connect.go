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

var mode = "public"
var privateTarget = ""

var publicHistory []string
var privateHistory = map[string][]string{}

func clearScreen() {
	fmt.Print("\033[H\033[2J")
}

func printPublicHistory() {
	clearScreen()
	fmt.Println("=== Public Chat ===")
	fmt.Println("Commands: /msg <username> | /list | /exit")
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

func readPassword(scanner *bufio.Scanner, prompt string) string {
	fmt.Print(prompt)
	scanner.Scan()
	return scanner.Text()
}

func isValidEmail(email string) bool {
	if len(email) > 254 {
		return false
	}
	if strings.Contains(email, " ") {
		return false
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}
	if len(parts[0]) == 0 {
		return false
	}
	domainParts := strings.Split(parts[1], ".")
	if len(domainParts) < 2 {
		return false
	}
	for _, p := range domainParts {
		if len(p) == 0 {
			return false
		}
	}
	return true
}

func isValidPhone(phone string) bool {
	if strings.Contains(phone, " ") {
		return false
	}
	if len(phone) < 7 || len(phone) > 15 {
		return false
	}
	for _, c := range phone {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

var connectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Connect to the broadcast server",
	Run: func(cmd *cobra.Command, args []string) {
		scanner := bufio.NewScanner(os.Stdin)

		// Connect to server first
		conn, _, err := websocket.DefaultDialer.Dial("ws://"+serverAddr+"/ws", nil)
		if err != nil {
			log.Fatal("Connection failed:", err)
		}
		defer conn.Close()

		fmt.Println("=== Welcome to Broadcast Chat ===")
		fmt.Print("Are you a new user? (yes/no): ")
		scanner.Scan()
		isNew := strings.ToLower(strings.TrimSpace(scanner.Text()))

		var authMsg string
		var username string

		if isNew == "yes" {
			// Registration flow
			fmt.Println("\n--- Register ---")

			for {
				fmt.Print("Choose a username (3-20 chars, no spaces): ")
				scanner.Scan()
				username = strings.TrimSpace(scanner.Text())
				if len(username) < 3 {
					fmt.Println("❌ Username must be at least 3 characters.")
					continue
				}
				if len(username) > 20 {
					fmt.Println("❌ Username must be at most 20 characters.")
					continue
				}
				if strings.ContainsAny(username, " \t") {
					fmt.Println("❌ Username cannot contain spaces.")
					continue
				}
				break
			}

			var contact string
			for {
				fmt.Print("Enter email or phone number: ")
				scanner.Scan()
				contact = strings.TrimSpace(scanner.Text())
				if strings.Contains(contact, "@") {
					if !isValidEmail(contact) {
						fmt.Println("❌ Invalid email format. Example: user@example.com")
						continue
					}
				} else {
					if !isValidPhone(contact) {
						fmt.Println("❌ Invalid phone: digits only, 7-15 digits.")
						continue
					}
				}
				break
			}

			var password string
			for {
				password = readPassword(scanner, "Choose a password (min 6 chars): ")
				if len(password) < 6 {
					fmt.Println("❌ Password must be at least 6 characters.")
					continue
				}
				break
			}

			authMsg = fmt.Sprintf("REGISTER:%s:%s:%s", username, contact, password)

		} else {
			// Login flow
			fmt.Println("\n--- Login ---")

			fmt.Print("Username: ")
			scanner.Scan()
			username = strings.TrimSpace(scanner.Text())

			password := readPassword(scanner, "Password: ")
			authMsg = fmt.Sprintf("LOGIN:%s:%s", username, password)
		}

		// Send auth message to server
		conn.WriteMessage(websocket.TextMessage, []byte(authMsg))

		done := make(chan struct{})
		authOK := make(chan string, 1)

		// Goroutine: receive messages
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

				// Auth success
				if strings.HasPrefix(text, "AUTH_OK:") {
					parts := strings.SplitN(text, ":", 3)
					if len(parts) == 3 {
						authOK <- parts[1] + ":" + parts[2]
					}
					continue
				}

				// Auth failure
				// Auth failure
				if strings.HasPrefix(text, "AUTH_FAIL:") {
					fmt.Println("\n❌", text[10:])
					fmt.Println("Please restart and try again.")
					authOK <- "error:" // unblock the main goroutine
					conn.Close()
					return
				}

				// Private message received
				if strings.HasPrefix(text, "PRIVATE_FROM:") {
					parts := strings.SplitN(text, ":", 3)
					if len(parts) == 3 {
						from := parts[1]
						msg := parts[2]
						line := fmt.Sprintf(">> [%s] [%s]: %s", t, from, msg)
						privateHistory[from] = append(privateHistory[from], line)
						if mode == "private" && privateTarget == from {
							fmt.Println(line)
						} else {
							fmt.Printf("\n** Private message from %s - type /msg %s to open **\n", from, from)
						}
					}
					continue
				}

				// Confirmation of own private message
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

				if strings.HasPrefix(text, "PRIVATE_HISTORY:") {
					parts := strings.SplitN(text, ":", 4)
					if len(parts) == 4 {
						sender := parts[1]
						timestamp := parts[2]
						content := parts[3]
						var line string
						if sender == username {
							line = fmt.Sprintf(">> [%s] [You]: %s", timestamp, content)
						} else {
							line = fmt.Sprintf(">> [%s] [%s]: %s", timestamp, sender, content)
						}
						privateHistory[privateTarget] = append(privateHistory[privateTarget], line)
						if mode == "private" {
							fmt.Println(line)
						}
					}
					continue
				}

				// System message
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
					fmt.Printf("\n** New public message - type /exit to go back **\n")
				}
			}
		}()

		// Wait for auth result
		payload := <-authOK
		parts := strings.SplitN(payload, ":", 2)
		if len(parts) == 2 {
			if parts[0] == "error" {
				<-done // wait for goroutine to finish
				return
			}
			username = parts[0]
			printPublicHistory()
			fmt.Println("✅", parts[1])
		}

		// Main input loop
		for scanner.Scan() {
			text := scanner.Text()
			if text == "" {
				continue
			}

			if strings.HasPrefix(text, "/msg ") {
				target := strings.TrimSpace(strings.TrimPrefix(text, "/msg "))
				if target == "" {
					fmt.Println("Usage: /msg <username>")
					continue
				}
				mode = "private"
				privateTarget = target
				// Request private history from server
				conn.WriteMessage(websocket.TextMessage, []byte("HISTORY:"+target))
				printPrivateHistory(target)
				continue
			}

			if text == "/exit" {
				mode = "public"
				privateTarget = ""
				printPublicHistory()
				continue
			}

			if text == "/list" {
				conn.WriteMessage(websocket.TextMessage, []byte("LIST:"))
				continue
			}

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
