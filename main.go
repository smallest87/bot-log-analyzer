package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// Konfigurasi Worker
const (
	NumWorkers = 3 // Jumlah "pekerja" yang menganalisis log secara paralel
)

// Struktur Data Hasil Analisis Sementara
type AnalyzedLog struct {
	Timestamp string
	BotType   string
	IP        string
	Action    string
}

func main() {
	fmt.Println("[SYSTEM] Memulai Bot Log Analyzer...")

	// 1. Inisialisasi Klien Docker
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("FATAL: Gagal inisialisasi Docker client: %v", err)
	}

	// Buat channel sebagai jembatan (Buffer) antara Pengekstrak Log dan Penganalisis
	logJobChannel := make(chan string, 1000) // Buffer 1000 baris agar RAM aman

	var wg sync.WaitGroup

	// 2. Menjalankan Pasukan Pekerja (Worker Pool)
	fmt.Printf("[SYSTEM] Menyiapkan %d Worker untuk analisis mendalam...\n", NumWorkers)
	for w := 1; w <= NumWorkers; w++ {
		wg.Add(1)
		go workerAnalyzer(w, logJobChannel, &wg)
	}

	// 3. Memulai Streaming Log dari Docker (Disimulasikan mencari container tertentu nanti)
	// CATATAN: Untuk testing lokal, kita panggil fungsi pembaca stream di background
	go streamContainerLogs(cli, "NAMA_ATAU_ID_CONTAINER_BOT_ANDA", logJobChannel)

	// Agar program tidak langsung mati, kita tahan main goroutine-nya
	// Di Coolify, aplikasi ini akan berjalan 24/7 di blok ini
	select {} 
}

// ==========================================
// FUNGSI 1: KONEKTOR STREAMING (The Listener)
// ==========================================
func streamContainerLogs(cli *client.Client, containerID string, jobs chan<- string) {
	ctx := context.Background()

	// Opsi untuk mengambil log
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true, // Sama seperti -f
		Tail:       "50", // Batasi histori awal agar ringan
	}

	// Membuka "Pipa" stream ke Docker Socket
	out, err := cli.ContainerLogs(ctx, containerID, options)
	if err != nil {
		// Jika error karena container tidak ada, kita print saja tanpa membuat program mati (karena worker masih bisa jalan)
		fmt.Printf("[STREAM ERROR] Gagal attach ke container %s: %v\n", containerID, err)
		fmt.Println(">> (Ini wajar jika Anda di Windows dan tidak ada container bernama tersebut)")
		return
	}
	defer out.Close()

	fmt.Printf("[STREAM] Berhasil attach ke container: %s. Menunggu log...\n", containerID)

	// Membaca baris demi baris menggunakan Scanner (Sangat hemat RAM)
	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		logLine := scanner.Text()
		
		// Lempar baris mentah ke channel untuk dianalisis oleh Worker
		jobs <- logLine
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("[STREAM ERROR] Terputus dari container: %v\n", err)
	}
}

// ==========================================
// FUNGSI 2: ANALIS MENDALAM (The Worker)
// ==========================================
func workerAnalyzer(id int, jobs <-chan string, wg *sync.WaitGroup) {
	defer wg.Done()

	for logLine := range jobs {
		// Di sinilah analisis mendalam terjadi (Parsing IP, Waktu, Bot)
		// Ini adalah contoh parser sederhana (nantinya kita ganti dengan Regex yang kuat)
		
		// Contoh membersihkan karakter aneh dari Docker multiplexer header (8 byte pertama biasanya binary)
		cleanLog := sanitizeDockerLog(logLine)

		// Simulasi deteksi sederhana
		botType := "Unknown"
		if strings.Contains(cleanLog, "Googlebot") {
			botType = "Googlebot"
		} else if strings.Contains(cleanLog, "bingbot") {
			botType = "Bingbot"
		}

		// Menampilkan hasil analisis
		// Pada hasil akhir, ini tidak di-print, melainkan disimpan ke Database/Map In-Memory
		fmt.Printf("[Worker-%d] Mendeteksi %s | Data: %s\n", id, botType, cleanLog)
	}
}

// Fungsi pembantu untuk membersihkan output raw docker
func sanitizeDockerLog(raw string) string {
	if len(raw) > 8 {
		// Membuang header 8-byte Docker stream (STDOUT/STDERR identifier)
		// Perhatian: Ini implementasi kasar, nantinya menggunakan stdcopy.StdCopy
		return raw[8:] 
	}
	return raw
}