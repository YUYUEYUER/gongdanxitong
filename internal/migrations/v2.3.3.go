package migrations

import (
	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

func V2_3_3(db *sqlx.DB, fs stuffbin.FileSystem, ko *koanf.Koanf) error {
	_, err := db.Exec(`
		INSERT INTO settings (key, value)
		VALUES ('app.public_ticket_require_order_number', 'false'::jsonb)
		ON CONFLICT (key) DO NOTHING;
	`)
	return err
}
