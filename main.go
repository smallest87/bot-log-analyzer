package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

const NumWorkers = 3

var ipRegex = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
var knownBots = []string{
	"Googlebot", "bingbot", "YandexBot", "Baiduspider", "Slurp", "DuckDuckBot", "Sogou",
	"GPTBot", "ChatGPT-User", "ClaudeBot", "anthropic-ai", "PerplexityBot", "Applebot-Extended", "Omgilibot", "Bytespider",
	"AhrefsBot", "SemrushBot", "DotBot", "MJ12bot", "Rogerbot", "Screaming Frog",
	"facebookexternalhit", "FacebookBot", "Twitterbot", "WhatsApp", "TelegramBot", "Discordbot", "LinkedInBot",
	"Pingdom", "UptimeRobot", "PetalBot",
}

type IPStats struct {
	IP        string
	BotType   string
	HitCount  int
	FirstSeen time.Time
	LastSeen  time.Time
	AlertSent bool // Anti-spam: Penanda apakah IP ini sudah dilaporkan ke Telegram
}

var intelligenceStore sync.Map

func main() {
	fmt.Println("[SYSTEM] Memulai Mesin Intelijen Bot Analyzer...")

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("FATAL: Gagal inisialisasi Docker client: %v", err)
	}

	logJobChannel := make(chan string, 1000)
	var wg sync.WaitGroup

	for w := 1; w <= NumWorkers; w++ {
		wg.Add(1)
		go workerAnalyzer(w, logJobChannel, &wg)
	}

	// TUGAS: Pastikan nama container ini sudah benar sesuai di Coolify
	targetContainer := "dpao7nun1z42116m98f06sy8-164825490843"
	go streamContainerLogs(cli, targetContainer, logJobChannel)

	go reportGenerator()

	select {}
}

func streamContainerLogs(cli *client.Client, containerID string, jobs chan<- string) {
	ctx := context.Background()
	options := container.LogsOptions{
		ShowStdout: true, ShowStderr: true, Follow: true, Tail: "100",
	}

	out, err := cli.ContainerLogs(ctx, containerID, options)
	if err != nil {
		fmt.Printf("[STREAM ERROR] Gagal attach ke %s: %v\n", containerID, err)
		return
	}
	defer out.Close()

	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		jobs <- scanner.Text()
	}
}

func workerAnalyzer(id int, jobs <-chan string, wg *sync.WaitGroup) {
	defer wg.Done()
	for logLine := range jobs {
		cleanLog := sanitizeDockerLog(logLine)
		ipMatch := ipRegex.FindString(cleanLog)
		if ipMatch == "" { continue }

		botDetected := "Unknown/Human"
		lowerLog := strings.ToLower(cleanLog)
		for _, bot := range knownBots {
			if strings.Contains(lowerLog, strings.ToLower(bot)) {
				botDetected = bot
				break
			}
		}

		if botDetected != "Unknown/Human" {
			now := time.Now()
			val, exists := intelligenceStore.Load(ipMatch)
			if exists {
				stats := val.(IPStats)
				stats.HitCount++
				stats.LastSeen = now
				intelligenceStore.Store(ipMatch, stats)
			} else {
				intelligenceStore.Store(ipMatch, IPStats{
					IP:        ipMatch,
					BotType:   strings.ToUpper(botDetected),
					HitCount:  1,
					FirstSeen: now,
					LastSeen:  now,
					AlertSent: false,
				})
			}
		}
	}
}

func sanitizeDockerLog(raw string) string {
	if len(raw) > 8 { return raw[8:] }
	return raw
}

// --- FUNGSI PENGIRIM TELEGRAM ---
func sendTelegramAlert(botType, ip string, hitCount int, rps float64) {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	chatID := os.Getenv("TELEGRAM_CHAT_ID")
	
	if token == "" || chatID == "" {
		fmt.Println("[WARNING] Token atau Chat ID Telegram belum diatur. Notifikasi dibatalkan.")
		return
	}

	// Format Pesan Markdown
	pesan := fmt.Sprintf("🚨 *ANOMALI BOT TERDETEKSI* 🚨\n\n🤖 *Bot:* %s\n🌐 *IP:* `%s`\n📈 *Total Hit:* %d\n⚡ *Kecepatan:* %.2f request/detik\n\n_Sistem memantau beban trafik yang tidak wajar dari IP ini. Segera cek server Anda._", botType, ip, hitCount, rps)

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	resp, err := http.PostForm(apiURL, url.Values{
		"chat_id":    {chatID},
		"text":       {pesan},
		"parse_mode": {"Markdown"},
	})
	
	if err != nil {
		fmt.Printf("[TELEGRAM ERROR] Gagal mengirim pesan: %v\n", err)
		return
	}
	defer resp.Body.Close()
	
	fmt.Printf("[TELEGRAM SUCCESS] Notifikasi anomali %s (%s) terkirim!\n", botType, ip)
}

func reportGenerator() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	// Mengambil nilai threshold dari Coolify (default: 10 RPS jika tidak diatur)
	thresholdStr := os.Getenv("ALERT_RPS_THRESHOLD")
	alertThreshold := 10.0 
	if t, err := strconv.ParseFloat(thresholdStr, 64); err == nil {
		alertThreshold = t
	}

	for range ticker.C {
		fmt.Println("\n===================================================")
		fmt.Printf("📊 LAPORAN PERGERAKAN BOT (Periode: %s)\n", time.Now().Format("15:04:05"))
		fmt.Println("===================================================")
		
		totalBots := 0
		
		intelligenceStore.Range(func(key, value interface{}) bool {
			stats := value.(IPStats)
			totalBots++
			
			duration := stats.LastSeen.Sub(stats.FirstSeen).Round(time.Second)
			if duration == 0 { duration = 1 * time.Second }
			
			rps := float64(stats.HitCount) / duration.Seconds()

			fmt.Printf("🤖 %-12s | IP: %-15s | Hit: %-4d | RPS: %.2f\n", stats.BotType, stats.IP, stats.HitCount, rps)
			
			// LOGIKA TRIGGER TELEGRAM
			if rps >= alertThreshold && !stats.AlertSent {
				// Jalankan pengiriman di background agar tidak memblokir laporan
				go sendTelegramAlert(stats.BotType, stats.IP, stats.HitCount, rps)
				
				// Tandai bahwa IP ini sudah dilaporkan agar tidak spam
				stats.AlertSent = true
				intelligenceStore.Store(key, stats)
			}
			
			return true 
		})

		if totalBots == 0 { fmt.Println("Tidak ada pergerakan bot yang terdeteksi.") }
		fmt.Println("===================================================\n")
	}
}