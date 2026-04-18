Sistem intelijen Anda sudah terstruktur dengan sangat baik. Penggunaan *Goroutine* dan *Channel* untuk memproses *log* secara paralel (*Worker Pool*) adalah tanda bahwa kode ini ditulis dengan standar *production*.

Saya telah merombak kode Anda untuk mengimplementasikan **Analisis 2 Dimensi (IP vs User-Agent)**. 

Berikut adalah perubahannya:
1. Menambahkan *package* `"net"`.
2. Memecah `knownBots` menjadi `standardBots` dan `scraperKeywords` agar kita bisa memberikan label khusus untuk penyerang.
3. Menambahkan fungsi `isPrivateIP()`.
4. Merombak logika di dalam `workerAnalyzer`.

Silakan timpa seluruh isi *file* `main.go` Anda dengan kode final di bawah ini:

```go
package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
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
	_ "github.com/lib/pq" // Driver PostgreSQL
)

const NumWorkers = 3

var ipRegex = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

// 1. Pisahkan daftar bot standar
var standardBots = []string{
	"Googlebot", "bingbot", "YandexBot", "Baiduspider", "Slurp", "DuckDuckBot", "Sogou",
	"GPTBot", "ChatGPT-User", "ClaudeBot", "anthropic-ai", "PerplexityBot", "Applebot-Extended", "Omgilibot", "Bytespider",
	"AhrefsBot", "SemrushBot", "DotBot", "MJ12bot", "Rogerbot", "Screaming Frog",
	"facebookexternalhit", "FacebookBot", "Twitterbot", "WhatsApp", "TelegramBot", "Discordbot", "LinkedInBot",
	"Pingdom", "UptimeRobot", "PetalBot",
}

// 2. Pisahkan daftar senjata scraper untuk pelabelan khusus
var scraperKeywords = []string{
	"python", "aiohttp", "requests", "urllib",
	"curl", "wget", "libcurl",
	"go-http-client",
	"java", "apache-httpclient",
	"node-fetch", "axios",
	"libwww-perl",
	"scrapy", "puppeteer", "selenium",
}

type IPStats struct {
	IP        string
	BotType   string
	HitCount  int
	FirstSeen time.Time
	LastSeen  time.Time
	AlertSent bool
}

var intelligenceStore sync.Map
var db *sql.DB

func main() {
	fmt.Println("[SYSTEM] Memulai Mesin Intelijen Bot Analyzer (Dengan Integrasi DB)...")

	initDatabase()

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

	targetContainer := "dpao7nun1z42116m98f06sy8-164825490843"
	go streamContainerLogs(cli, targetContainer, logJobChannel)

	go reportAndSyncDB()

	select {}
}

// ==========================================
// FUNGSI PENGECEKAN IP (BARU)
// ==========================================
func isPrivateIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	// Mengamankan 10.x.x.x, 172.16.x.x, 192.168.x.x dan 127.0.0.1
	return ip.IsPrivate() || ip.IsLoopback()
}

// ==========================================
// FUNGSI DATABASE
// ==========================================
func initDatabase() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Println("[DB WARNING] DATABASE_URL tidak ditemukan. Berjalan dalam mode In-Memory saja.")
		return
	}

	var err error
	db, err = sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("FATAL: Gagal membuka koneksi database: %v", err)
	}

	if err = db.Ping(); err != nil {
		log.Fatalf("FATAL: Tidak dapat melakukan ping ke database: %v", err)
	}

	fmt.Println("[DB SUCCESS] Berhasil terhubung ke PostgreSQL!")

	createTableQuery := `
	CREATE TABLE IF NOT EXISTS bot_analytics (
		ip VARCHAR(50) PRIMARY KEY,
		bot_type VARCHAR(100),
		hit_count INT,
		first_seen TIMESTAMP,
		last_seen TIMESTAMP
	);`
	
	_, err = db.Exec(createTableQuery)
	if err != nil {
		log.Fatalf("FATAL: Gagal membuat tabel bot_analytics: %v", err)
	}
	fmt.Println("[DB SUCCESS] Tabel analitik siap digunakan.")
}

func syncToDatabase(stats IPStats) {
	if db == nil {
		return
	}

	query := `
		INSERT INTO bot_analytics (ip, bot_type, hit_count, first_seen, last_seen) 
		VALUES ($1, $2, $3, $4, $5) 
		ON CONFLICT (ip) DO UPDATE 
		SET hit_count = EXCLUDED.hit_count, 
		    last_seen = EXCLUDED.last_seen;
	`
	_, err := db.Exec(query, stats.IP, stats.BotType, stats.HitCount, stats.FirstSeen, stats.LastSeen)
	if err != nil {
		fmt.Printf("[DB ERROR] Gagal menyimpan data IP %s: %v\n", stats.IP, err)
	}
}

// ==========================================
// FUNGSI STREAM & WORKER
// ==========================================
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

		// LOGIKA 2 DIMENSI DIMULAI DI SINI
		if isPrivateIP(ipMatch) {
			botDetected = "System (Internal)"
		} else {
			// Cek bot standar terlebih dahulu
			for _, bot := range standardBots {
				if strings.Contains(lowerLog, strings.ToLower(bot)) {
					botDetected = bot
					break
				}
			}

			// Jika bukan bot standar, cek apakah menggunakan tool scraper
			if botDetected == "Unknown/Human" {
				for _, keyword := range scraperKeywords {
					if strings.Contains(lowerLog, keyword) {
						botDetected = "Scanner (" + keyword + ")"
						break
					}
				}
			}
		}

		// Proses penyimpanan state
		if botDetected != "Unknown/Human" {
			now := time.Now()
			val, exists := intelligenceStore.Load(ipMatch)
			
			// Jika label sudah pernah terekam, kita tetap gunakan uppercase
			finalLabel := strings.ToUpper(botDetected)

			if exists {
				stats := val.(IPStats)
				stats.HitCount++
				stats.LastSeen = now
				intelligenceStore.Store(ipMatch, stats)
			} else {
				intelligenceStore.Store(ipMatch, IPStats{
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
}

func sanitizeDockerLog(raw string) string {
	if len(raw) > 8 { return raw[8:] }
	return raw
}

// ==========================================
// FUNGSI REPORTER & TELEGRAM
// ==========================================
func sendTelegramAlert(botType, ip string, hitCount int, rps float64) {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	chatID := os.Getenv("TELEGRAM_CHAT_ID")
	if token == "" || chatID == "" { return }

	pesan := fmt.Sprintf("🚨 *ANOMALI BOT TERDETEKSI* 🚨\n\n🤖 *Bot:* %s\n🌐 *IP:* `%s`\n📈 *Total Hit:* %d\n⚡ *Kecepatan:* %.2f request/detik", botType, ip, hitCount, rps)

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	http.PostForm(apiURL, url.Values{"chat_id": {chatID}, "text": {pesan}, "parse_mode": {"Markdown"}})
}

func reportAndSyncDB() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	thresholdStr := os.Getenv("ALERT_RPS_THRESHOLD")
	alertThreshold := 10.0 
	if t, err := strconv.ParseFloat(thresholdStr, 64); err == nil { alertThreshold = t }

	for range ticker.C {
		fmt.Println("\n===================================================")
		fmt.Printf("📊 SINKRONISASI DB & LAPORAN BOT (Periode: %s)\n", time.Now().Format("15:04:05"))
		fmt.Println("===================================================")
		
		totalBots := 0
		
		intelligenceStore.Range(func(key, value interface{}) bool {
			stats := value.(IPStats)
			totalBots++
			
			duration := stats.LastSeen.Sub(stats.FirstSeen).Round(time.Second)
			if duration == 0 { duration = 1 * time.Second }
			rps := float64(stats.HitCount) / duration.Seconds()

			fmt.Printf("🤖 %-20s | IP: %-15s | Hit: %-4d | RPS: %.2f\n", stats.BotType, stats.IP, stats.HitCount, rps)
			
			go syncToDatabase(stats)

			if rps >= alertThreshold && !stats.AlertSent {
				go sendTelegramAlert(stats.BotType, stats.IP, stats.HitCount, rps)
				stats.AlertSent = true
				intelligenceStore.Store(key, stats)
			}
			
			return true 
		})

		if totalBots == 0 { fmt.Println("Tidak ada pergerakan bot yang terdeteksi.") }
		fmt.Println("===================================================\n")
	}
}
```