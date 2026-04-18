package telegram

import (
	"bot-log-analyzer/internal/domain"
	"fmt"
	"net/http"
	"net/url"
)

type telegramNotifier struct {
	token  string
	chatID string
}

func NewTelegramNotifier(token, chatID string) domain.AlertNotifier {
	return &telegramNotifier{
		token:  token,
		chatID: chatID,
	}
}

func (t *telegramNotifier) SendAlert(botType, ip string, hitCount int, rps float64) error {
	// Abaikan jika kredensial tidak diatur di environment
	if t.token == "" || t.chatID == "" {
		return nil
	}

	pesan := fmt.Sprintf("🚨 *ANOMALI BOT TERDETEKSI* 🚨\n\n🤖 *Bot:* %s\n🌐 *IP:* `%s`\n📈 *Total Hit:* %d\n⚡ *Kecepatan:* %.2f request/detik", botType, ip, hitCount, rps)

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.token)
	_, err := http.PostForm(apiURL, url.Values{
		"chat_id":    {t.chatID},
		"text":       {pesan},
		"parse_mode": {"Markdown"},
	})
	
	return err
}