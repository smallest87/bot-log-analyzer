package postgres

import (
	"bot-log-analyzer/internal/domain"
	"database/sql"
	"fmt"
)

// postgresRepository adalah struct yang mengimplementasikan domain.BotRepository
type postgresRepository struct {
	db *sql.DB
}

// NewBotRepository adalah constructor untuk membuat pekerja database baru
func NewBotRepository(db *sql.DB) domain.BotRepository {
	return &postgresRepository{
		db: db,
	}
}

// UpsertBotStat adalah implementasi dari kontrak di domain
func (r *postgresRepository) UpsertBotStat(stat domain.IPStats) error {
	if r.db == nil {
		return fmt.Errorf("koneksi database kosong, mengabaikan penyimpanan")
	}

	query := `
		INSERT INTO bot_analytics (ip, bot_type, hit_count, first_seen, last_seen) 
		VALUES ($1, $2, $3, $4, $5) 
		ON CONFLICT (ip) DO UPDATE 
		SET hit_count = EXCLUDED.hit_count, 
		    last_seen = EXCLUDED.last_seen;
	`
	_, err := r.db.Exec(query, stat.IP, stat.BotType, stat.HitCount, stat.FirstSeen, stat.LastSeen)
	return err
}