package main

import (
	"log"
	"os"
	"path"

	"go-tribute-payments-hook-1/app/settings"

	"github.com/amarnathcjd/gogram/telegram"
)

func main() {
	log.Println("Starting Telegram Authentication Tool...")
	log.Printf("Using AppID: %d\n", settings.AppID)
	log.Printf("Session will be saved in: %s\n", settings.SessionPath)

	if settings.AppID == 0 || settings.AppHash == "" {
		log.Fatalln("Error: TELEGRAM_API_ID and TELEGRAM_API_HASH must be set in Environment Variables.")
	}

	// Ensure session directory exists
	os.MkdirAll(settings.SessionPath, os.ModePerm)

	// Create client config
	client, err := telegram.NewClient(telegram.ClientConfig{
		AppID:   settings.AppID,
		AppHash: settings.AppHash,
		Session: path.Join(settings.SessionPath, "gogram.dat"),
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	log.Println("Connecting to Telegram and waiting for interactive login...")
	// Connect and prompt for auth
	if err := client.Start(); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}
	if err := client.AuthPrompt(); err != nil {
		log.Fatalf("Authentication failed: %v", err)
	}

	// Test the connection by getting self info
	self, err := client.Self()
	if err != nil {
		log.Fatalf("Failed to get self info: %v", err)
	}

	log.Printf("Successfully authenticated as %s (ID: %d)\n", self.Username, self.ID)
	log.Println("Session file 'gogram.dat' has been created/updated successfully.")
	log.Println("You can now restart the main application from the Coolify UI.")
	client.Stop()
}
