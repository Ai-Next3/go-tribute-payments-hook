package main

import (
	// "fmt" // Убираем неиспользуемый импорт
	"log"
	"os"
	"path" // Добавляем импорт path для Session
	"strconv"

	"github.com/amarnathcjd/gogram/telegram"
	"github.com/joho/godotenv" // Для чтения .env файла
)

func checkPeer() {
	// Загружаем переменные из .env файла
	err := godotenv.Load(".env")
	if err != nil {
		log.Printf("Error loading .env file: %v", err)
		return
	}

	apiIDStr := os.Getenv("TELEGRAM_API_ID")
	apiHash := os.Getenv("TELEGRAM_API_HASH")
	sessionPath := os.Getenv("TELEGRAM_SESSION_PATH")
	forwardToStr := os.Getenv("TELEGRAM_FORWARD_TO")

	if apiIDStr == "" || apiHash == "" || sessionPath == "" || forwardToStr == "" {
		log.Println("Error: Missing required environment variables")
		return
	}

	apiID, err := strconv.Atoi(apiIDStr)
	if err != nil {
		log.Printf("Error parsing TELEGRAM_API_ID: %v", err)
		return
	}
	forwardToID, err := strconv.ParseInt(forwardToStr, 10, 64)
	if err != nil {
		log.Printf("Error parsing TELEGRAM_FORWARD_TO: %v", err)
		return
	}

	log.Println("Using API ID:", apiID)
	log.Println("Using Session Path (folder):", sessionPath)
	log.Println("Checking Peer ID:", forwardToID)

	config := telegram.ClientConfig{
		AppID:        int32(apiID),
		AppHash:      apiHash,
		Session:      path.Join(sessionPath, "gogram.dat"),
		DisableCache: true,
	}

	client, err := telegram.NewClient(config)
	if err != nil {
		log.Printf("Failed to create client: %v", err)
		return
	}

	log.Println("Connecting to Telegram...")
	err = client.Connect()
	if err != nil {
		log.Printf("Failed to connect: %v", err)
		return
	}
	defer client.Disconnect()
	log.Println("Connected successfully!")

	log.Printf("Attempting to find peer with ID %d using GetPeer...\n", forwardToID)
	peer, err := client.GetPeer(forwardToID)
	if err != nil {
		log.Printf("!!! FAILED to find peer %d using GetPeer: %v\n", forwardToID, err)
		peer, err = client.ResolvePeer(forwardToID)
		if err != nil {
			log.Printf("!!! FAILED to find peer %d using ResolvePeer either: %v\n", forwardToID, err)
		} else {
			log.Printf("!!! SUCCESS finding peer %d using ResolvePeer: %+v\n", forwardToID, peer)
		}
	} else {
		log.Printf(">>> SUCCESS finding peer %d using GetPeer: %+v\n", forwardToID, peer)
	}

	log.Println("Check finished.")
}
