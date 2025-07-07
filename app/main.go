package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go-tribute-api/settings"
	"go-tribute-api/tg"
	"go-tribute-api/tribute"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amarnathcjd/gogram/telegram"
	_ "github.com/lib/pq"
	"github.com/natefinch/atomic"
)

var newTXLock = &sync.Mutex{}

// initDB создает и проверяет пул подключений к базе данных.
// Эта функция вызывается один раз при старте приложения.
func initDB() (*sql.DB, error) {
	connStr := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		settings.DBHost,
		settings.DBPort,
		settings.DBUser,
		settings.DBPassword,
		settings.DBName,
	)
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}

	// Проверяем, что соединение с БД действительно установлено.
	if err = db.Ping(); err != nil {
		db.Close() // Закрываем, если пинг не прошел
		return nil, err
	}

	slog.Info("Database connection pool established successfully.")
	return db, nil
}

func fetchNewTransactions(db *sql.DB, client *telegram.Client, retry bool) {
	if !retry {
		if !newTXLock.TryLock() {
			return
		}
		defer newTXLock.Unlock()
	}

	tributeAuth, err := getTributeAuth(client, retry)
	if err != nil {
		slog.Error("Failed to get tribute auth", "error", err)
		return
	}

	savedTxID, err := readLastKnownTxID()
	if err != nil {
		slog.Warn("Failed to read last known transaction ID", "error", err)
	}

	var transactions []tribute.Transaction
	var maxTxID int64

	// Улучшенная логика с повторными попытками
	for i := 1; i <= settings.FetchRetryCount+1; i++ {
		transactions, maxTxID, err = tribute.FetchTransactions(tributeAuth, savedTxID)
		if err == nil {
			// Успех, выходим из цикла
			slog.Info("Successfully fetched transactions", "count", len(transactions))
			break
		}

		slog.Warn("Failed to fetch transactions", "attempt", i, "total_attempts", settings.FetchRetryCount+1, "error", err)

		if i > settings.FetchRetryCount {
			// Это была последняя попытка, выходим с ошибкой
			slog.Error("All attempts to fetch transactions failed. Giving up.")
			return
		}

		// Ждем перед следующей попыткой
		sleepDuration := time.Second * time.Duration(5*i) // Увеличиваем задержку с каждой попыткой (5s, 10s, 15s)
		slog.Info("Waiting before retrying...", "duration", sleepDuration)
		time.Sleep(sleepDuration)
	}

	if err := sendTransactions(db, transactions); err != nil {
		slog.Error("Failed to send transactions", "error", err)
		return
	}

	if maxTxID > savedTxID {
		if err := saveLastKnownTxID(maxTxID); err != nil {
			slog.Warn("Failed to save last known transaction ID", "error", err)
		}
	}
}

// Проверка, был ли donation уже обработан
func isDonationProcessed(db *sql.DB, donationID int64) (bool, error) {
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM processed_donations WHERE donation_id = $1)", donationID).Scan(&exists)
	return exists, err
}

// Отметить donation как обработанный
func markDonationProcessed(db *sql.DB, donationID int64) error {
	_, err := db.Exec("INSERT INTO processed_donations (donation_id) VALUES ($1) ON CONFLICT DO NOTHING", donationID)
	return err
}

func sendTransactions(db *sql.DB, transactions []tribute.Transaction) error {
	if len(transactions) == 0 {
		return nil
	}
	if settings.WebhookURL == "" {
		slog.Info("Webhook URL not set, skipping sending transactions.")
		return nil
	}

	if settings.WebhookBatch {
		slog.Info("Sending batched transactions to webhook", "count", len(transactions))
		data, _ := json.Marshal(transactions)
		return sendDataRetry(data, "batch")
	} else {
		waiter := sync.WaitGroup{}
		var (
			failed     error
			failedLock sync.Mutex
		)
		for _, transaction := range transactions {
			waiter.Add(1)
			go func(tx tribute.Transaction) {
				defer waiter.Done()
				logger := slog.With("transaction_id", tx.ID, "user_id", tx.User.TelegramID, "amount", tx.Donation.Amount, "currency", tx.Currency)
				logger.Info("Processing transaction")

				// --- НАЧАЛО: Сохранение доната в таблицу donations ---
				if err := saveDonationToTable(db, tx); err != nil {
					logger.Warn("Failed to save donation to donations table", "error", err)
				} else {
					logger.Info("Donation saved to donations table")
				}
				// --- КОНЕЦ: Сохранение доната в таблицу donations ---

				// --- НАЧАЛО: Проверка и начисление баланса ---
				desc := tx.DonationRequest.Description
				donationID := tx.Donation.ID
				if desc != "" &&
					strings.Contains(desc, "Ваша благодарность вернется к вам на счет в полном объеме в приложении GPT³") &&
					donationID != 0 {

					processed, err := isDonationProcessed(db, donationID)
					if err != nil {
						logger.Warn("Failed to check if donation was processed", "donation_id", donationID, "error", err)
						return
					}
					if processed {
						logger.Info("Donation already processed, skipping", "donation_id", donationID)
						return
					}

					tgID := tx.User.TelegramID
					// ИСПРАВЛЕНИЕ: Берем сумму из фактического доната (Donation),
					// а не из запроса (DonationRequest), чтобы избежать ошибки
					// с суммой по умолчанию (100) при обработке старых транзакций.
					amount := tx.Donation.Amount
					if tgID != 0 && amount > 0 {
						err := addBalanceByTgID(db, tgID, amount)
						if err != nil {
							logger.Warn("Failed to add balance to user", "error", err)
						} else {
							logger.Info("Balance added to user", "amount", amount)
							if err := markDonationProcessed(db, donationID); err != nil {
								logger.Warn("Failed to mark donation as processed", "donation_id", donationID, "error", err)
							}
						}
					}
				}
				// --- КОНЕЦ: Проверка и начисление баланса ---

				if err := sendDataRetry(tx, fmt.Sprintf("tx_%d", tx.ID)); err != nil {
					logger.Warn("Failed to send transaction to webhook", "error", err)
					failedLock.Lock()
					if failed == nil {
						failed = err
					}
					failedLock.Unlock()
				}
			}(transaction)
		}

		waiter.Wait()
		return failed
	}
}

func sendDataRetry(data interface{}, logContext string) error {
	if data == nil {
		return nil
	}
	logger := slog.With("context", logContext)

	body, err := json.Marshal(data)
	if err != nil {
		logger.Error("Failed to marshal data for webhook", "error", err)
		return err
	}

	var signature string
	if settings.WebhookSignatureKey != "" {
		signer := hmac.New(sha256.New, []byte(settings.WebhookSignatureKey))
		signer.Write(body)
		signature = hex.EncodeToString(signer.Sum(nil))
	}

	var lastErr error
	for attempt := 1; attempt <= settings.FetchRetryCount; attempt++ {
		lastErr = sendData(body, signature)
		if lastErr == nil {
			return nil
		}
		logger.Warn("Failed to send data to webhook", "attempt", attempt, "error", lastErr)
		if attempt < settings.FetchRetryCount {
			time.Sleep(time.Second / 4)
		}
	}
	return lastErr
}

func sendData(body []byte, signature string) error {
	req, err := http.NewRequest("POST", settings.WebhookURL, bytes.NewBuffer(body))
	if err != nil {
		return err
	}

	if settings.WebhookLogin != "" || settings.WebhookPassword != "" {
		req.SetBasicAuth(settings.WebhookLogin, settings.WebhookPassword)
	}
	if signature != "" {
		req.Header.Set(settings.WebhookSignatureHeader, signature)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("status code %d", resp.StatusCode)
	}
	return nil
}

func saveLastKnownTxID(txID int64) error {
	slog.Info("Saving last known transaction ID", "tx_id", txID)
	return atomic.WriteFile(path.Join(settings.SessionPath, "last_known_tx.dat"), bytes.NewBuffer([]byte(strconv.FormatInt(txID, 10))))
}

func readLastKnownTxID() (int64, error) {
	if fd, err := os.Open(path.Join(settings.SessionPath, "last_known_tx.dat")); err != nil {
		return 0, err
	} else {
		defer fd.Close()
		data, err := io.ReadAll(fd)
		if err != nil {
			return 0, err
		}
		return strconv.ParseInt(string(data), 10, 64)
	}
}

func getTributeAuth(client *telegram.Client, reset bool) (string, error) {
	if !reset {
		if fd, err := os.Open(path.Join(settings.SessionPath, "tribute.auth")); err == nil {
			defer fd.Close()
			data, err := io.ReadAll(fd)
			if err != nil {
				return "", err
			}
			slog.Info("Successfully loaded tribute.auth from cache.")
			return string(data), nil
		}
	}

	slog.Info("Requesting new tribute.auth token...")
	webViewURL, err := tg.RequestBotWebView(client, settings.BotUsername)
	if err != nil {
		return "", err
	}
	auth, err := tribute.MakeAuthorizationHeader(webViewURL.URL)
	if err != nil {
		return "", err
	}
	slog.Info("New tribute.auth token generated.")
	if err := atomic.WriteFile(path.Join(settings.SessionPath, "tribute.auth"), bytes.NewBuffer([]byte(auth))); err != nil {
		slog.Warn("Failed to save tribute.auth", "error", err)
	}
	return auth, nil
}

// Обновление баланса пользователя по tg_id
func addBalanceByTgID(db *sql.DB, tgID int64, amount float64) error {
	_, err := db.Exec(`UPDATE users SET balance = balance + $1 WHERE tg_id = $2`, amount, tgID)
	return err
}

// Сохранение информации о донате в таблицу donations
func saveDonationToTable(db *sql.DB, transaction tribute.Transaction) error {
	// Преобразуем транзакцию в JSON для поля raw_data
	rawData, err := json.Marshal(transaction)
	if err != nil {
		return err
	}

	// Вставляем данные в таблицу donations
	_, err = db.Exec(`
		INSERT INTO donations (id, tg_id, amount, currency, event_type, raw_data, created_at) 
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO NOTHING`,
		transaction.ID,
		transaction.User.TelegramID,
		transaction.Amount,
		transaction.Currency,
		transaction.Type,
		string(rawData),
		time.Unix(transaction.CreatedAt, 0),
	)
	return err
}

func main() {
	// Инициализация логгера
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	slog.Info("Starting service...")

	db, err := initDB()
	if err != nil {
		slog.Error("Failed to initialize database connection pool", "error", err)
		os.Exit(1)
	}
	defer db.Close() // Гарантируем, что пул соединений будет закрыт при выходе.

	client, err := tg.RunningClient()
	if err != nil {
		slog.Error("Failed to get running client", "error", err)
		os.Exit(1)
	}
	defer client.Stop()

	slog.Info("Resolving bot username", "username", settings.BotUsername)
	botUser, err := client.ResolveUsername(settings.BotUsername)
	if err != nil {
		slog.Error("Failed to resolve bot username", "username", settings.BotUsername, "error", err)
		os.Exit(1)
	}

	var forwardPeer any = nil
	if settings.ForwardTo != 0 {
		if p, err := client.GetPeer(settings.ForwardTo); err != nil {
			slog.Warn("Failed to fetch forward peer", "peer_id", settings.ForwardTo, "error", err)
		} else {
			forwardPeer = p
			slog.Info("Successfully fetched forward peer", "peer_id", settings.ForwardTo)
		}
	}

	client.On(telegram.OnMessage, func(m *telegram.NewMessage) error {
		msgLogger := slog.With("message_id", m.Message.ID)
		if m.Message.Out {
			return nil
		}
		if forwardPeer != nil {
			msgLogger.Info("Forwarding message", "to_peer_id", settings.ForwardTo)
			if _, err := m.Client.Forward(forwardPeer, m.Peer, []int32{m.Message.ID}); err != nil {
				msgLogger.Warn("Failed to forward message", "error", err)
			}
		}
		msgLogger.Info("Triggering transaction fetch from new message.")
		go fetchNewTransactions(db, m.Client, false)
		return nil
	}, telegram.FilterUsers(botUser.(*telegram.UserObj).ID))

	slog.Info("Performing initial fetch of new transactions")
	go fetchNewTransactions(db, client, false)

	slog.Info("Service started. Waiting for messages...", "from_bot", settings.BotUsername)
	client.Idle()
}
