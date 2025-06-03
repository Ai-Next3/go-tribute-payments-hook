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
	"log"
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

func fetchNewTransactions(client *telegram.Client, retry bool) {
	if !retry {
		if !newTXLock.TryLock() {
			return
		}
		defer newTXLock.Unlock()
	}

	tributeAuth, err := getTributeAuth(client, retry)
	if err != nil {
		log.Println("Failed to read last known transaction ID", err)
		return
	}

	savedTxID, err := readLastKnownTxID()
	if err != nil {
		log.Println("[WARN] Failed to read last known transaction ID", err)
	}

	transactions, maxTxID, err := tribute.FetchTransactions(tributeAuth, savedTxID)
	if err != nil {
		if retry {
			log.Println("[WARN] Failed to fetch transactions, will reset tribute tokens:", err)
			fetchNewTransactions(client, true)
		} else {
			log.Println("Failed to fetch transactions:", err)
		}
		return
	}

	if err := sendTransactions(transactions); err != nil {
		log.Println("Failed to send transactions:", err)
		return
	}

	if maxTxID > savedTxID {
		if err := saveLastKnownTxID(maxTxID); err != nil {
			log.Println("[WARN] Failed to save last known transaction ID:", err)
		}
	}
}

// Проверка, был ли donation уже обработан
func isDonationProcessed(donationID int64) (bool, error) {
	db, err := openDB()
	if err != nil {
		return false, err
	}
	defer db.Close()

	var exists bool
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM processed_donations WHERE donation_id = $1)", donationID).Scan(&exists)
	return exists, err
}

// Отметить donation как обработанный
func markDonationProcessed(donationID int64) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec("INSERT INTO processed_donations (donation_id) VALUES ($1) ON CONFLICT DO NOTHING", donationID)
	return err
}

func sendTransactions(transactions []tribute.Transaction) error {
	if len(transactions) == 0 || settings.WebhookURL == "" {
		log.Println("No new transactions")
		return nil
	}
	if settings.WebhookBatch {
		log.Println("Sending batched transactions to webhook:", len(transactions))
		data, _ := json.Marshal(transactions)
		return sendDataRetry(data)
	} else {
		waiter := sync.WaitGroup{}
		var failed error
		for _, transaction := range transactions {
			waiter.Add(1)
			go func(tx tribute.Transaction) {
				defer waiter.Done()
				log.Println("Sending transaction", tx.ID)

				// --- НАЧАЛО: Проверка и начисление баланса ---
				desc := tx.DonationRequest.Description
				donationID := tx.Donation.ID
				if desc != "" &&
					strings.Contains(desc, "Ваша благодарность вернется к вам на счет в полном объеме в приложении GPT³") &&
					donationID != 0 {

					processed, err := isDonationProcessed(donationID)
					if err != nil {
						log.Println("[WARN] Не удалось проверить donation_id:", donationID, err)
						return
					}
					if processed {
						log.Println("Донат уже обработан, пропускаем:", donationID)
						return
					}

					tgID := tx.User.TelegramID
					amount := tx.DonationRequest.Amount
					if tgID != 0 && amount > 0 {
						err := addBalanceByTgID(tgID, amount)
						if err != nil {
							log.Println("[WARN] Не удалось начислить баланс пользователю:", tgID, err)
						} else {
							log.Println("Баланс успешно начислен пользователю:", tgID, "на сумму:", amount)
							if err := markDonationProcessed(donationID); err != nil {
								log.Println("[WARN] Не удалось отметить donation_id как обработанный:", donationID, err)
							}
						}
					}
				}
				// --- КОНЕЦ: Проверка и начисление баланса ---

				if err := sendDataRetry(tx); err != nil {
					log.Println("[WARN] Failed to send transaction:", err)
					failed = err
				}
			}(transaction)
		}

		waiter.Wait()
		return failed
	}
}

func sendDataRetry(data interface{}) error {
	if data == nil {
		return nil
	}

	body, err := json.Marshal(data)
	if err != nil {
		return err
	}

	var signature string
	if settings.WebhookSignatureKey != "" {
		signer := hmac.New(sha256.New, []byte(settings.WebhookSignatureKey))
		signer.Write(body)
		signature = hex.EncodeToString(signer.Sum(nil))
	}

	attempt := 0
	for {
		attempt++
		if err := sendData(body, signature); err == nil {
			return nil
		} else if attempt >= settings.FetchRetryCount {
			return err
		}
		time.Sleep(time.Second / 4)
	}
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

	if resp.StatusCode != 200 {
		return fmt.Errorf("status code %d", resp.StatusCode)
	}
	return nil
}

func saveLastKnownTxID(txID int64) error {
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
			return string(data), nil
		}
	}

	webViewURL, err := tg.RequestBotWebView(client, settings.BotUsername)
	if err != nil {
		return "", err
	}
	auth, err := tribute.MakeAuthorizationHeader(webViewURL.URL)
	if err != nil {
		return "", err
	}
	if err := atomic.WriteFile(path.Join(settings.SessionPath, "tribute.auth"), bytes.NewBuffer([]byte(auth))); err != nil {
		log.Println("[WARN] Failed to save tribute.auth:", err)
	}
	return auth, nil
}

// Подключение к базе данных PostgreSQL
func openDB() (*sql.DB, error) {
	connStr := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		settings.DBHost,
		settings.DBPort,
		settings.DBUser,
		settings.DBPassword,
		settings.DBName,
	)
	return sql.Open("postgres", connStr)
}

// Обновление баланса пользователя по tg_id
func addBalanceByTgID(tgID int64, amount float64) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	// Обновляем баланс пользователя
	_, err = db.Exec(`UPDATE users SET balance = balance + $1 WHERE tg_id = $2`, amount, tgID)
	return err
}

func main() {
	client, err := tg.RunningClient()
	if err != nil {
		log.Fatalln(err)
	}
	defer client.Stop()

	// // <<< ДОБАВЛЯЕМ ЗАДЕРЖКУ ЗДЕСЬ >>>
	// log.Println("Waiting a bit for synchronization...")
	// time.Sleep(10 * time.Second) // Ждем, например, 10 секунд
	// log.Println("Done waiting.")
	// // <<< КОНЕЦ ЗАДЕРЖКИ >>>

	botUser, err := client.ResolveUsername(settings.BotUsername)
	if err != nil {
		log.Fatalln(err)
	}

	var forwardPeer any = nil
	if settings.ForwardTo != 0 {
		if p, err := client.GetPeer(settings.ForwardTo); err != nil {
			log.Println("Failed to fetch forward peer:", err)
		} else {
			forwardPeer = p
		}
	}

	client.On(telegram.OnMessage, func(m *telegram.NewMessage) error {
		if m.Message.Out {
			return nil
		}
		if forwardPeer != nil {
			log.Println("Forward message", m.Message.ID, "to", settings.ForwardTo)
			if _, err := m.Client.Forward(forwardPeer, m.Peer, []int32{m.Message.ID}); err != nil {
				log.Println("[WARN] Failed to forward message", m.Message.ID, err)
			}
		}
		fetchNewTransactions(m.Client, false)
		return nil
	}, telegram.FilterUsers(botUser.(*telegram.UserObj).ID))

	log.Println("Fetching new transactions")
	go fetchNewTransactions(client, false)

	log.Println("Wait for messages from", settings.BotUsername)
	client.Idle()
}
