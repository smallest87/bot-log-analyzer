package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

const NumWorkers = 3

var ipRegex = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

var knownBots = []string{
	// Search Engines
	"Googlebot", "bingbot", "YandexBot", "Baiduspider", "Slurp", "DuckDuckBot", "Sogou",
	
	// AI & LLM
	"GPTBot", "ChatGPT-User", "ClaudeBot", "anthropic-ai", "PerplexityBot", "Applebot-Extended", "Omgilibot", "Bytespider",
	
	// SEO & Analytics
	"AhrefsBot", "SemrushBot", "DotBot", "MJ12bot", "Rogerbot", "Screaming Frog",
	
	// Social Media & Previews
	"facebookexternalhit", "FacebookBot", "Twitterbot", "WhatsApp", "TelegramBot", "Discordbot", "LinkedInBot",
	
	// Monitors
	"Pingdom", "UptimeRobot", "PetalBot",
}

// Struktur untuk menyimpan rekam jejak per IP
type IPStats struct {
	IP        string
	BotType   string
	HitCount  int
	FirstSeen time.Time
	LastSeen  time.Time
}

// sync.Map aman untuk concurency (Goroutines)
var intelligenceStore sync.Map 

func main() {
	fmt.Println("[SYSTEM] Memulai Mesin Intelijen Bot Analyzer...")

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("FATAL: Gagal inisialisasi Docker client: %v", err)
	}

	logJobChannel := make(chan string, 1000)
	var wg sync.WaitGroup

	// Menjalankan Worker
	for w := 1; w <= NumWorkers; w++ {
		wg.Add(1)
		go workerAnalyzer(w, logJobChannel, &wg)
	}

	// TUGAS: Ganti dengan ID/Nama container Anda yang benar
	targetContainer := "dpao7nun1z42116m98f06sy8-164825490843"
	go streamContainerLogs(cli, targetContainer, logJobChannel)

	// Menjalankan Goroutine Pelapor (Reporter)
	go reportGenerator()

	select {}
}

// --- FUNGSI STREAM ---
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

// --- FUNGSI WORKER (PENGUMPUL DATA) ---
func workerAnalyzer(id int, jobs <-chan string, wg *sync.WaitGroup) {
	defer wg.Done()

	for logLine := range jobs {
		cleanLog := sanitizeDockerLog(logLine)

		ipMatch := ipRegex.FindString(cleanLog)
		if ipMatch == "" {
			continue 
		}

		botDetected := "Unknown/Human"
		lowerLog := strings.ToLower(cleanLog)
		for _, bot := range knownBots {
			if strings.Contains(lowerLog, strings.ToLower(bot)) {
				botDetected = bot
				break
			}
		}

		// Jika itu bot, kita masukkan ke dalam "Ingatan" (Store)
		if botDetected != "Unknown/Human" {
			now := time.Now()
			
			// Cek apakah IP ini sudah ada di database memori kita
			val, exists := intelligenceStore.Load(ipMatch)
			if exists {
				// Jika ada, update jumlah Hit dan Waktu Terakhir
				stats := val.(IPStats)
				stats.HitCount++
				stats.LastSeen = now
				intelligenceStore.Store(ipMatch, stats)
			} else {
				// Jika IP baru, buat rekaman baru
				intelligenceStore.Store(ipMatch, IPStats{
					IP:        ipMatch,
					BotType:   strings.ToUpper(botDetected),
					HitCount:  1,
					FirstSeen: now,
					LastSeen:  now,
				})
			}
		}
	}
}

func sanitizeDockerLog(raw string) string {
	if len(raw) > 8 { return raw[8:] }
	return raw
}

// --- FUNGSI REPORTER (ANALISIS BERKALA) ---
func reportGenerator() {
	// Buat loop yang melaporkan hasil setiap 15 detik
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		fmt.Println("\n===================================================")
		fmt.Printf("📊 LAPORAN PERGERAKAN BOT (Periode: %s)\n", time.Now().Format("15:04:05"))
		fmt.Println("===================================================")
		
		totalBots := 0
		
		// Membaca isi memory
		intelligenceStore.Range(func(key, value interface{}) bool {
			stats := value.(IPStats)
			totalBots++
			
			// Hitung durasi aktivitas
			duration := stats.LastSeen.Sub(stats.FirstSeen).Round(time.Second)
			if duration == 0 { duration = 1 * time.Second } // Mencegah 0 detik
			
			// Hitung agresi (Request per detik)
			rps := float64(stats.HitCount) / duration.Seconds()

			fmt.Printf("🤖 %-12s | IP: %-15s | Total Hit: %-4d | RPS: %.2f hit/dtk\n", 
				stats.BotType, stats.IP, stats.HitCount, rps)
			
			return true // Lanjut iterasi
		})

		if totalBots == 0 {
			fmt.Println("Tidak ada pergerakan bot yang terdeteksi.")
		}
		fmt.Println("===================================================\n")
	}
}