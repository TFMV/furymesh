package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// rootCmd is the base command for the FuryMesh CLI.
var rootCmd = &cobra.Command{
	Use:   "furyctl",
	Short: "FuryMesh CLI - Manage P2P WebRTC nodes",
	Long:  "FuryMesh CLI provides commands to manage P2P WebRTC nodes, share data, and configure network parameters.",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Welcome to FuryMesh CLI! Use 'furyctl --help' for available commands.")
	},
}

// Execute runs the root command.
func Execute() {
	cobra.OnInitialize(initConfig)
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

// initConfig initializes Viper to read in configuration.
func initConfig() {
	viper.SetConfigName("config") // config file name (without extension)
	viper.SetConfigType("yaml")   // config file type
	viper.AddConfigPath(".")      // look for the config in the current directory

	if err := viper.ReadInConfig(); err != nil {
		fmt.Println("No config file found, using defaults.")
	}
}
