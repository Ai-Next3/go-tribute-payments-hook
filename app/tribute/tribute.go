package tribute

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"go-tribute-api/settings"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

func fixValue(data string) string {
	// A simple way to fix escaped JSON values without parsing and writing tons of code to preserve the order.
	return strings.ReplaceAll(data, `\/`, `/`)
}

func MakeAuthorizationHeader(webViewURL string) (string, error) {
	webURL, err := url.Parse(webViewURL)
	if err != nil {
		return "", err
	}
	fragmentValues, err := url.ParseQuery(webURL.RawFragment)
	if err != nil {
		return "", err
	}
	tgWebAppData := fragmentValues.Get("tgWebAppData")
	initDataUnsafe, err := url.ParseQuery(tgWebAppData)
	if err != nil {
		return "", err
	}
	var values []string
	for k := range initDataUnsafe {
		if k != "hash" {
			values = append(values, k)
		}
	}
	slices.Sort(values)

	for idx := range values {
		value := fixValue(initDataUnsafe.Get(values[idx]))
		values[idx] = fmt.Sprintf("%s=%s", values[idx], value)
	}

	valuesStr := strings.Join(values, "\n")
	initData := base64.StdEncoding.EncodeToString([]byte(valuesStr))
	authPayload := fmt.Sprintf("1;%s;%s", initData, initDataUnsafe.Get("hash"))
	authKey := base64.StdEncoding.EncodeToString([]byte(authPayload))
	return fmt.Sprintf("TgAuth %s", authKey), nil
}

func FetchTransactions(tributeAuth string, lastKnownTxID int64) ([]Transaction, int64, error) {
	nextFrom := "0"
	var maxTxID int64 = -1
	var transactions []Transaction
	page := 1

	for {
		logger := slog.With("page", page, "start_from", nextFrom)
		logger.Info("Fetching transactions page")
		resp, err := requestTransactionsRetry(tributeAuth, nextFrom)
		if err != nil {
			return transactions, maxTxID, err
		}
		for _, tx := range resp.Transactions {
			if tx.ID > maxTxID {
				maxTxID = tx.ID
			}
			if tx.ID > lastKnownTxID {
				transactions = append(transactions, tx)
			} else {
				// Мы дошли до транзакций, которые уже видели,
				// но нужно дойти до конца этой страницы.
				// Если раскомментировать, остановится сразу, но может пропустить что-то.
				// break
			}
		}

		if resp.NextFrom == "" {
			logger.Info("Finished fetching all transaction pages.")
			break
		} else {
			nextFrom = resp.NextFrom
			page++
		}
	}
	return transactions, maxTxID, nil
}

func requestTransactionsRetry(tributeAuth string, nextFrom string) (*TransactionsResponse, error) {
	var lastErr error
	for attempt := 1; attempt <= settings.FetchRetryCount; attempt++ {
		resp, err := requestTransactions(tributeAuth, nextFrom)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		slog.Warn("Failed to request transactions", "attempt", attempt, "error", err)
		if attempt < settings.FetchRetryCount {
			time.Sleep(time.Second / 4)
		}
	}
	return nil, lastErr
}

func requestTransactions(tributeAuth string, nextFrom string) (*TransactionsResponse, error) {
	body, _ := json.Marshal(map[string]string{"list": "dashboard_creator", "mode": "creator", "startFrom": nextFrom})
	req, err := http.NewRequest("POST", "https://subscribebot.org/api/v4/dashboard/transactions", bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", tributeAuth)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status code %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	content := &TransactionsResponse{}
	return content, json.Unmarshal(data, content)
}
