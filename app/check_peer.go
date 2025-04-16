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

func main() {
	// Загружаем переменные из .env файла
	err := godotenv.Load(".env") // Запускаем из папки app, путь теперь просто ".env"
	if err != nil {
		log.Fatalf("Error loading .env file: %v\n Run 'cp .env.example .env' and fill it", err)
	}

	// Читаем необходимые переменные
	apiIDStr := os.Getenv("TELEGRAM_API_ID")
	apiHash := os.Getenv("TELEGRAM_API_HASH")
	sessionPath := os.Getenv("TELEGRAM_SESSION_PATH") // Путь к папке
	forwardToStr := os.Getenv("TELEGRAM_FORWARD_TO")

	if apiIDStr == "" || apiHash == "" || sessionPath == "" || forwardToStr == "" {
		log.Fatal("Error: Missing required environment variables (TELEGRAM_API_ID, TELEGRAM_API_HASH, TELEGRAM_SESSION_PATH, TELEGRAM_FORWARD_TO)")
	}

	// Конвертируем ID в числа
	apiID, err := strconv.Atoi(apiIDStr)
	if err != nil {
		log.Fatalf("Error parsing TELEGRAM_API_ID: %v", err)
	}
	forwardToID, err := strconv.ParseInt(forwardToStr, 10, 64)
	if err != nil {
		log.Fatalf("Error parsing TELEGRAM_FORWARD_TO: %v", err)
	}

	log.Println("Using API ID:", apiID)
	log.Println("Using Session Path (folder):", sessionPath)
	log.Println("Checking Peer ID:", forwardToID)

	// Конфигурация клиента gogram - ИСПРАВЛЕНА согласно tg.go
	config := telegram.ClientConfig{
		AppID:        int32(apiID), // Правильное поле: AppID
		AppHash:      apiHash,      // Правильное поле: AppHash
		Session:      path.Join(sessionPath, "gogram.dat"), // Правильное поле: Session (путь к файлу)
		DisableCache: true,         // Добавляем флаг как в основном коде
	}

	// Исправляем вызов NewClient: ожидаем два значения (client, err)
	// и передаем config (теперь типа ClientConfig)
	client, err := telegram.NewClient(config)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err) // Добавляем проверку ошибки создания клиента
	}

	// Подключаемся к Telegram
	log.Println("Connecting to Telegram...")
	err = client.Connect() // Используем уже созданный client
	if err != nil {
		// Важно: Не используем client.Start() или client.AuthPrompt() здесь,
		// так как нам нужно только подключиться с существующей сессией
		log.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect() // Отключаемся в конце
	log.Println("Connected successfully!")

	// Пытаемся найти пир
	log.Printf("Attempting to find peer with ID %d using GetPeer...\n", forwardToID)
	peer, err := client.GetPeer(forwardToID)

	// Выводим результат
	if err != nil {
		log.Printf("!!! FAILED to find peer %d using GetPeer: %v\n", forwardToID, err)
		// Попробуем еще ResolvePeer, вдруг поможет?
		log.Printf("Attempting to find peer with ID %d using ResolvePeer...\n", forwardToID)
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