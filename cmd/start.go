package cmd

import (
	"broadcast-server/server"
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

var port string

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the broadcast server",
	Run: func(cmd *cobra.Command, args []string) {
		// Load .env file
		if err := godotenv.Load(); err != nil {
			fmt.Println("No .env file found, using environment variables")
		}

		mongoURI := os.Getenv("MONGODB_URI")
		dbName := os.Getenv("DB_NAME")

		if mongoURI == "" {
			fmt.Println("❌ MONGODB_URI not set in .env or environment")
			os.Exit(1)
		}
		if dbName == "" {
			dbName = "broadcast_chat"
		}

		s := server.NewServer(mongoURI, dbName)
		go s.Run()
		s.Start(port)
	},
}

func init() {
	startCmd.Flags().StringVarP(&port, "port", "p", "8080", "Port to listen on")
	rootCmd.AddCommand(startCmd)
}
