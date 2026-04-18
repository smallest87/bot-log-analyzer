package usecase

import (
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"bot-log-analyzer/internal/domain"
)

var ipRegex = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

var standardBots = []string{
	"Googlebot", "bingbot", "YandexBot", "Baiduspider", "Slurp", "DuckDuckBot", "Sogou",
	"GPTBot", "ChatGPT-User", "ClaudeBot", "anthropic-ai", "PerplexityBot", "Applebot-Extended", "Omgilibot", "Bytespider",
	"AhrefsBot", "SemrushBot", "DotBot", "MJ12bot", "Rogerbot", "Screaming Frog",
	"facebookexternalhit", "FacebookBot", "Twitterbot", "WhatsApp", "TelegramBot", "Discordbot", "LinkedInBot",
	"Pingdom", "UptimeRobot", "PetalBot",
}

var scraperKeywords = []string{
	"python", "aiohttp", "requests", "urllib",
	"curl", "wget", "libcurl",
	"go-http-client",
	"java", "apache-httpclient",
	"node-fetch", "axios",
	"libwww-perl",
	"scrapy", "puppeteer", "selenium",
}

// BotAnalyzerUsecase membungkus state dan dependensi
type BotAnalyzerUsecase struct {
	repo     domain.BotRepository
	notifier domain.AlertNotifier
	store    sync.Map
}

func NewBotAnalyzerUsecase(repo domain.BotRepository, notifier domain.AlertNotifier) *BotAnalyzerUsecase {
	return &BotAnalyzerUsecase{
		repo:     repo,
		notifier: notifier,
	}
}

func isPrivateIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	return ip.IsPrivate() || ip.IsLoopback()
}

func sanitizeDockerLog(raw string) string {
	if len(raw) > 8 {
		return raw[8:]
	}
	return raw
}

// ProcessLog adalah pekerja utama yang dipanggil oleh goroutine
func (u *BotAnalyzerUsecase) ProcessLog(logLine string) {
	cleanLog := sanitizeDockerLog(logLine)
	ipMatch := ipRegex.FindString(cleanLog)
	if ipMatch == "" {
		return
	}

	botDetected := "Unknown/Human"
	lowerLog := strings.ToLower(cleanLog)

	if isPrivateIP(ipMatch) {
		botDetected = "System (Internal)"
	} else {
		for _, bot := range standardBots {
			if strings.Contains(lowerLog, strings.ToLower(bot)) {
				botDetected = bot
				break
			}
		}

		if botDetected == "Unknown/Human" {
			for _, keyword := range scraperKeywords {
				if strings.Contains(lowerLog, keyword) {
					botDetected = "Scanner (" + keyword + ")"
					break
				}
			}
		}
	}

	if botDetected != "Unknown/Human" {
		now := time.Now()
		val, exists := u.store.Load(ipMatch)

		finalLabel := strings.ToUpper(botDetected)

		if exists {
			stats := val.(domain.IPStats)
			stats.HitCount++
			stats.LastSeen = now
			u.store.Store(ipMatch, stats)
		} else {
			u.store.Store(ipMatch, domain.IPStats{
				IP:        ipMatch,
				BotType:   finalLabel,
				HitCount:  1,
				FirstSeen: now,
				LastSeen:  now,
				AlertSent: false,
			})
		}
	}
}

// ReportAndSync bertugas merekap data dan mengirimnya ke infrastruktur
func (u *BotAnalyzerUsecase) ReportAndSync() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	thresholdStr := os.Getenv("ALERT_RPS_THRESHOLD")
	alertThreshold := 10.0
	if t, err := strconv.ParseFloat(thresholdStr, 64); err == nil {
		alertThreshold = t
	}

	for range ticker.C {
		fmt.Println("\n===================================================")
		fmt.Printf("📊 SINKRONISASI DB & LAPORAN BOT (Periode: %s)\n", time.Now().Format("15:04:05"))
		fmt.Println("===================================================")

		totalBots := 0

		u.store.Range(func(key, value interface{}) bool {
			stats := value.(domain.IPStats)
			totalBots++

			duration := stats.LastSeen.Sub(stats.FirstSeen).Round(time.Second)
			if duration == 0 {
				duration = 1 * time.Second
			}
			rps := float64(stats.HitCount) / duration.Seconds()

			fmt.Printf("🤖 %-20s | IP: %-15s | Hit: %-4d | RPS: %.2f\n", stats.BotType, stats.IP, stats.HitCount, rps)

			// Memanggil interface (Usecase tidak tahu ini PostgreSQL atau yang lain)
			go u.repo.UpsertBotStat(stats)

			if rps >= alertThreshold && !stats.AlertSent {
				go u.notifier.SendAlert(stats.BotType, stats.IP, stats.HitCount, rps)
				stats.AlertSent = true
				u.store.Store(key, stats)
			}

			return true
		})

		if totalBots == 0 {
			fmt.Println("Tidak ada pergerakan bot yang terdeteksi.")
		}
		fmt.Println("===================================================\n")
	}
}