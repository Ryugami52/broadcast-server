package cmd

import (
	"bufio"
	"fmt"
	"log"
	"os"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
)

var serverAddr string

var connectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Connect to the broadcast server",
	Run: func(cmd *cobra.Command, args []string) {
		conn, _, err := websocket.DefaultDialer.Dial("ws://"+serverAddr+"/ws", nil)
		if err != nil {
			log.Fatal("Connection failed:", err)
		}
		defer conn.Close()
		fmt.Println("Connected to server at", serverAddr)
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
				fmt.Println(">>", string(message))
			}
		}()

		// Read user input on the MAIN goroutine (blocks until EOF or error)
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			text := scanner.Text()
			if text == "" {
				continue
			}
			err := conn.WriteMessage(websocket.TextMessage, []byte(text))
			if err != nil {
				log.Println("Send error:", err)
				break
			}
		}

		conn.Close()
		<-done // wait for receiver to finish
	},
}

func init() {
	connectCmd.Flags().StringVarP(&serverAddr, "address", "a", "localhost:8080", "Server address")
	rootCmd.AddCommand(connectCmd)
}
