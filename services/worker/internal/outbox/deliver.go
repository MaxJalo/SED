package outbox

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Deliver(ctx context.Context, pool *pgxpool.Pool, baseURL string) {
	if baseURL == "" {
		return
	}
	rows, err := pool.Query(ctx, `
		SELECT id::text, document_id::text, payload, attempts
		FROM outbox_events
		WHERE status IN ('ожидает', 'ошибка')
		  AND (next_retry_at IS NULL OR next_retry_at <= now())
		ORDER BY created_at
		LIMIT 20
	`)
	if err != nil {
		log.Printf("outbox list: %v", err)
		return
	}
	defer rows.Close()
	type event struct {
		id, documentID, payload string
		attempts                int
	}
	var batch []event
	for rows.Next() {
		var item event
		if err := rows.Scan(&item.id, &item.documentID, &item.payload, &item.attempts); err != nil {
			log.Printf("outbox scan: %v", err)
			return
		}
		batch = append(batch, item)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	for _, item := range batch {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/documents", bytes.NewBufferString(item.payload))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
		}
		if err == nil && resp.StatusCode < 300 {
			_, _ = pool.Exec(ctx, `UPDATE outbox_events SET status = 'доставлен' WHERE id = $1::uuid`, item.id)
			_, _ = pool.Exec(ctx, `UPDATE documents SET onec_sync_status = 'синхронизирован' WHERE id = $1::uuid`, item.documentID)
			continue
		}
		waitSeconds := 1 << min(item.attempts, 8)
		_, _ = pool.Exec(ctx, `
			UPDATE outbox_events
			SET status = 'ошибка', attempts = attempts + 1, next_retry_at = now() + make_interval(secs => $2)
			WHERE id = $1::uuid
		`, item.id, waitSeconds)
		_, _ = pool.Exec(ctx, `UPDATE documents SET onec_sync_status = 'ошибка' WHERE id = $1::uuid`, item.documentID)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
