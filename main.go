package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

const NumWorkers = 3

// 1. Compile Regex secara Global (Mencegah CPU overload)
// Regex ini akan mencari pola IP Address standar (IPv4)
var ipRegex = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

// Daftar bot yang ingin kita pantau
var knownBots = []string{
	"Googlebot",
	"bingbot",
	"YandexBot",
	"Baiduspider",
	"AhrefsBot",
	"SemrushBot",
}

// Struktur Data Hasil Analisis
type AnalyzedLog struct {
	IP      string
	BotType string
	RawData string
}

func main() {
	fmt.Println("[SYSTEM] Memulai Bot Log Analyzer...")

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("FATAL: Gagal inisialisasi Docker client: %v", err)
	}

	logJobChannel := make(chan string, 1000)
	var wg sync.WaitGroup

	fmt.Printf("[SYSTEM] Menyiapkan %d Worker Analyzer...\n", NumWorkers)
	for w := 1; w <= NumWorkers; w++ {
		wg.Add(1)
		go workerAnalyzer(w, logJobChannel, &wg)
	}

	// TUGAS ANDA: Ganti string di bawah ini dengan nama container web/proxy Anda di Coolify
	// Contoh: "coolify-proxy" atau ID container seperti "dpao7nun1z42116m98f06sy8"
	targetContainer := "dpao7nun1z42116m98f06sy8-164825490843"
	
	go streamContainerLogs(cli, targetContainer, logJobChannel)

	select {}
}

func streamContainerLogs(cli *client.Client, containerID string, jobs chan<- string) {
	ctx := context.Background()
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Tail:       "50",
	}

	out, err := cli.ContainerLogs(ctx, containerID, options)
	if err != nil {
		fmt.Printf("[STREAM ERROR] Gagal attach ke %s: %v\n", containerID, err)
		return
	}
	defer out.Close()

	fmt.Printf("[STREAM] Berhasil attach ke: %s. Mulai memantau pergerakan...\n", containerID)

	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		jobs <- scanner.Text()
	}
}

func workerAnalyzer(id int, jobs <-chan string, wg *sync.WaitGroup) {
	defer wg.Done()

	for logLine := range jobs {
		cleanLog := sanitizeDockerLog(logLine)

		// 1. Ekstrak IP Address menggunakan Regex
		ipMatch := ipRegex.FindString(cleanLog)
		if ipMatch == "" {
			continue // Abaikan log yang tidak memiliki IP (biasanya log internal/error sistem)
		}

		// 2. Identifikasi Jenis Bot
		botDetected := "Manusia / Unknown"
		lowerLog := strings.ToLower(cleanLog) // Ubah ke huruf kecil untuk pencocokan yang aman
		
		for _, bot := range knownBots {
			if strings.Contains(lowerLog, strings.ToLower(bot)) {
				botDetected = bot
				break
			}
		}

		// 3. Masukkan ke Struct Data
		data := AnalyzedLog{
			IP:      ipMatch,
			BotType: botDetected,
			RawData: cleanLog,
		}

		// Jika terdeteksi bot, tampilkan dengan format khusus
		if data.BotType != "Manusia / Unknown" {
			fmt.Printf(">> [WORKER-%d] 🤖 %s TERDETEKSI | IP: %s\n", id, strings.ToUpper(data.BotType), data.IP)
		} else {
			// Opsional: Buka komentar di bawah ini jika ingin melihat akses manusia biasa
			// fmt.Printf("[WORKER-%d] 👤 Akses Biasa | IP: %s\n", id, data.IP)
		}
	}
}

func sanitizeDockerLog(raw string) string {
	if len(raw) > 8 {
		return raw[8:]
	}
	return raw
}