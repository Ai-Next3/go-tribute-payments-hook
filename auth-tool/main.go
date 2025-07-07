package main

import (
	"go-tribute-api/settings"
	"log"
	"os"
	"path"

	"github.com/amarnathcjd/gogram/telegram"
)

func main() {
	log.Println("Starting Telegram Authentication Tool...")
	log.Printf("Using AppID: %d\n", settings.AppID)
	// Важно: сессия будет сохранена относительно того места, где запускается утилита.
	// Dockerfile настроит это правильно.
	log.Printf("Session will be saved in: %s\n", settings.SessionPath)

	if settings.AppID == 0 || settings.AppHash == "" {
		log.Fatalln("Error: TELEGRAM_API_ID and TELEGRAM_API_HASH must be set in Environment Variables.")
	}

	// Убедимся, что папка для сессии существует
	os.MkdirAll(settings.SessionPath, os.ModePerm)

	// Создаем конфигурацию клиента
	client, err := telegram.NewClient(telegram.ClientConfig{
		AppID:   settings.AppID,
		AppHash: settings.AppHash,
		Session: path.Join(settings.SessionPath, "gogram.dat"),
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	log.Println("Connecting to Telegram and waiting for interactive login...")
	// Подключаемся и ждем ввода данных
	if err := client.Start(); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}
	if err := client.AuthPrompt(); err != nil {
		log.Fatalf("Authentication failed: %v", err)
	}

	// Проверяем подключение
	self, err := client.Me() // Правильный метод - Me()
	if err != nil {
		log.Fatalf("Failed to get self info: %v", err)
	}

	log.Printf("Successfully authenticated as %s (ID: %d)\n", self.Username, self.ID)
	log.Println("Session file 'gogram.dat' has been created/updated successfully.")
	log.Println("You can now restart the main application from the Coolify UI.")
	client.Stop()
}
