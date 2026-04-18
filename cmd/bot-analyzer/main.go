package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"sync"

	// SESUAIKAN IMPORT INI DENGAN NAMA MODUL ANDA
	"bot-log-analyzer/internal/infrastructure/docker"
	"bot-log-analyzer/internal/infrastructure/postgres"
	"bot-log-analyzer/internal/infrastructure/telegram"
	"bot-log-analyzer/internal/usecase"

	"github.com/docker/docker/client"
	_ "github.com/lib/pq"
)

const NumWorkers = 3

func main() {
	fmt.Println("[SYSTEM] Memulai Mesin Intelijen Bot Analyzer (Clean Architecture)...")

	// 1. Setup Database Connection
	dbURL := os.Getenv("DATABASE_URL")
	var dbConn *sql.DB
	if dbURL != "" {
		var err error
		dbConn, err = sql.Open("postgres", dbURL)
		if err != nil {
			log.Fatalf("FATAL: Gagal membuka koneksi database: %v", err)
		}
		if err = dbConn.Ping(); err != nil {
			log.Fatalf("FATAL: Tidak dapat melakukan ping ke database: %v", err)
		}
		fmt.Println("[DB SUCCESS] Berhasil terhubung ke PostgreSQL!")

		// Pastikan tabel siap
		createTableQuery := `
		CREATE TABLE IF NOT EXISTS bot_analytics (
			ip VARCHAR(50) PRIMARY KEY,
			bot_type VARCHAR(100),
			hit_count INT,
			first_seen TIMESTAMP,
			last_seen TIMESTAMP
		);`
		if _, err = dbConn.Exec(createTableQuery); err != nil {
			log.Fatalf("FATAL: Gagal membuat tabel bot_analytics: %v", err)
		}
	} else {
		fmt.Println("[DB WARNING] DATABASE_URL tidak ditemukan. Berjalan tanpa DB.")
	}

	// 2. Setup Docker Client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("FATAL: Gagal inisialisasi Docker client: %v", err)
	}

	// 3. WIRING (Injeksi Dependensi ke Usecase)
	repo := postgres.NewBotRepository(dbConn)
	notifier := telegram.NewTelegramNotifier(os.Getenv("TELEGRAM_BOT_TOKEN"), os.Getenv("TELEGRAM_CHAT_ID"))
	streamer := docker.NewDockerStreamer(cli)
	
	analyzer := usecase.NewBotAnalyzerUsecase(repo, notifier)

	// 4. Inisialisasi Channel & Worker Pool
	logJobChannel := make(chan string, 1000)
	var wg sync.WaitGroup

	for w := 1; w <= NumWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for logLine := range logJobChannel {
				// Memanggil fungsi dari Usecase
				analyzer.ProcessLog(logLine)
			}
		}()
	}

	// 5. Jalankan Streamer (JANGAN DI-HARDCODE LAGI)
	targetContainer := os.Getenv("TARGET_CONTAINER")
	if targetContainer == "" {
		// Nilai fallback sementara jika di .env belum diatur
		targetContainer = "dpao7nun1z42116m98f06sy8-164825490843"
		fmt.Println("[WARNING] TARGET_CONTAINER tidak ada di .env. Menggunakan nilai fallback.")
	}

	go func() {
		if err := streamer.StreamLogs(targetContainer, logJobChannel); err != nil {
			fmt.Printf("[STREAM ERROR] %v\n", err)
		}
	}()

	// 6. Jalankan Mesin Sinkronisasi
	go analyzer.ReportAndSync()

	// Menahan agar program tetap berjalan
	select {}
}