package cmd

import (
	"broadcast-server/server"

	"github.com/spf13/cobra"
)

var port string

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the broadcast server",
	Run: func(cmd *cobra.Command, args []string) {
		s := server.NewServer()
		go s.Run()
		s.Start(port)
	},
}

func init() {
	startCmd.Flags().StringVarP(&port, "port", "p", "8080", "Port to listen on")
	rootCmd.AddCommand(startCmd)
}
