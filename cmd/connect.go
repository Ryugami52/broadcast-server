package cmd

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
)

var serverAddr string

var connectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Connect to the broadcast server",
	Run: func(cmd *cobra.Command, args []string) {
		// Ask username FIRST before connecting
		fmt.Print("Enter your username: ")
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Scan()
		username := scanner.Text()
		if username == "" {
			username = "Anonymous"
		}

		// THEN connect to server
		conn, _, err := websocket.DefaultDialer.Dial("ws://"+serverAddr+"/ws", nil)
		if err != nil {
			log.Fatal("Connection failed:", err)
		}
		defer conn.Close()

		fmt.Printf("Connected as %s!\n", username)
		fmt.Println("Type a message and press Enter to send:")

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
				t := time.Now().Format("15:04:05")
				fmt.Printf(">> [%s] %s\n", t, string(message))
			}
		}()

		// Read user input on the MAIN goroutine (blocks until EOF or error)
		for scanner.Scan() {
			text := scanner.Text()
			if text == "" {
				continue
			}
			message := fmt.Sprintf("[%s]: %s", username, text)
			err := conn.WriteMessage(websocket.TextMessage, []byte(message))
			if err != nil {
				log.Println("Send error:", err)
				break
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
