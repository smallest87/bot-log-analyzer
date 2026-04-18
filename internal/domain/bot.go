package domain

import "time"

// IPStats merepresentasikan bentuk data analitik dari sebuah IP.
// Ini adalah entitas utama bisnis kita.
type IPStats struct {
	IP        string
	BotType   string
	HitCount  int
	FirstSeen time.Time
	LastSeen  time.Time
	AlertSent bool
}

// BotRepository adalah kontrak kerja.
// Siapapun yang mengklaim sebagai "Database" di aplikasi ini,
// wajib memiliki fungsi UpsertBotStat.
type BotRepository interface {
	UpsertBotStat(stat IPStats) error
}

// AlertNotifier adalah kontrak kerja.
// Siapapun yang bertugas mengirim pesan (entah Telegram, WhatsApp, atau Email),
// wajib memiliki fungsi SendAlert dengan parameter ini.
type AlertNotifier interface {
	SendAlert(botType, ip string, hitCount int, rps float64) error
}

// LogStreamer adalah kontrak kerja.
// Siapapun penyedia log-nya (Docker, file teks, atau layanan cloud),
// wajib bisa menyalurkan teks log ke dalam channel 'jobs'.
type LogStreamer interface {
	StreamLogs(containerID string, jobs chan<- string) error
}