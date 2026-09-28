package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

type JobRepository interface {
	Init() error
	Save(VideoJob) error
	CreateJob(VideoJob) error
	UpdateStatus(id, status, errorMessage string) error
	ListPendingOutbox(limit int) ([]OutboxEntry, error)
	MarkOutboxSent(id int64) error
	MarkOutboxFailed(id int64, attempts int, nextAttemptAt time.Time, reason string) error
	ListByUser(string) ([]VideoJob, error)
	AuthenticateUser(username, password string) (User, error)
	HasOutputForUser(filename, username string) (bool, error)
}

type User struct {
	Username     string
	Email        string
	PasswordHash string
}

type PostgresConfig struct{ DSN string }

type OutboxEntry struct {
	ID            int64
	Job           VideoJob
	Attempts      int
	NextAttemptAt time.Time
}

type PostgresJobRepository struct{ db *sql.DB }

func NewPostgresJobRepository(config PostgresConfig) (*PostgresJobRepository, error) {
	db, err := sql.Open("postgres", config.DSN)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &PostgresJobRepository{db: db}, nil
}

func (r *PostgresJobRepository) Init() error {
	_, err := r.db.Exec(`CREATE TABLE IF NOT EXISTS users (
		username TEXT PRIMARY KEY,
		email TEXT NOT NULL DEFAULT '',
		password_hash TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`)
	if err != nil {
		return err
	}
	if err := r.seedDefaultUser(); err != nil {
		return err
	}

	_, err = r.db.Exec(`CREATE TABLE IF NOT EXISTS video_jobs (
		id TEXT PRIMARY KEY,
		username TEXT NOT NULL,
		email TEXT NOT NULL DEFAULT '',
		object_key TEXT NOT NULL,
		status TEXT NOT NULL,
		error_message TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMPTZ NOT NULL,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`ALTER TABLE video_jobs ADD COLUMN IF NOT EXISTS error_message TEXT NOT NULL DEFAULT ''`)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`ALTER TABLE video_jobs ADD COLUMN IF NOT EXISTS email TEXT NOT NULL DEFAULT ''`)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`CREATE TABLE IF NOT EXISTS job_outbox (
		id BIGSERIAL PRIMARY KEY,
		job_id TEXT NOT NULL UNIQUE REFERENCES video_jobs(id),
		payload JSONB NOT NULL,
		attempts INTEGER NOT NULL DEFAULT 0,
		next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		last_error TEXT NOT NULL DEFAULT '',
		sent_at TIMESTAMPTZ
	)`)
	return err
}

func (r *PostgresJobRepository) seedDefaultUser() error {
	username := envOr("API_USER", "admin")
	password := envOr("API_PASSWORD", "")
	if password == "" {
		return fmt.Errorf("API_PASSWORD deve ser configurado para criar o usuario seed")
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("erro ao gerar hash da senha seed: %w", err)
	}
	_, err = r.db.Exec(`INSERT INTO users (username, email, password_hash)
		VALUES ($1, $2, $3)
		ON CONFLICT (username) DO NOTHING`,
		username, envOr("API_USER_EMAIL", ""), string(passwordHash))
	return err
}

func (r *PostgresJobRepository) AuthenticateUser(username, password string) (User, error) {
	var user User
	err := r.db.QueryRow(`SELECT username, email, password_hash FROM users WHERE username = $1`, username).
		Scan(&user.Username, &user.Email, &user.PasswordHash)
	if err != nil {
		return User{}, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return User{}, fmt.Errorf("credenciais invalidas")
	}
	return user, nil
}

func (r *PostgresJobRepository) HasOutputForUser(filename, username string) (bool, error) {
	jobID := strings.TrimSuffix(strings.TrimPrefix(filename, "frames_"), ".zip")
	if jobID == filename || jobID == "" {
		return false, nil
	}
	var exists bool
	err := r.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM video_jobs WHERE id = $1 AND username = $2)`, jobID, username).Scan(&exists)
	return exists, err
}

func (r *PostgresJobRepository) Save(job VideoJob) error {
	return r.CreateJob(job)
}

func (r *PostgresJobRepository) CreateJob(job VideoJob) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO video_jobs (id, username, email, object_key, status, error_message, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, job.ID, job.User, job.Email, job.ObjectKey, job.Status, job.Error, job.CreatedAt); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO job_outbox (job_id, payload) VALUES ($1, $2)`, job.ID, payload); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *PostgresJobRepository) UpdateStatus(id, status, errorMessage string) error {
	_, err := r.db.Exec(`UPDATE video_jobs
		SET status = $1, error_message = $2, updated_at = NOW()
		WHERE id = $3`, status, errorMessage, id)
	return err
}

func (r *PostgresJobRepository) ListPendingOutbox(limit int) ([]OutboxEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Query(`SELECT id, payload, attempts, next_attempt_at
		FROM job_outbox WHERE sent_at IS NULL AND next_attempt_at <= NOW()
		ORDER BY id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]OutboxEntry, 0)
	for rows.Next() {
		var entry OutboxEntry
		var payload []byte
		if err := rows.Scan(&entry.ID, &payload, &entry.Attempts, &entry.NextAttemptAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payload, &entry.Job); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (r *PostgresJobRepository) MarkOutboxSent(id int64) error {
	_, err := r.db.Exec(`UPDATE job_outbox SET sent_at = NOW() WHERE id = $1`, id)
	return err
}

func (r *PostgresJobRepository) MarkOutboxFailed(id int64, attempts int, nextAttemptAt time.Time, reason string) error {
	_, err := r.db.Exec(`UPDATE job_outbox SET attempts = $1, next_attempt_at = $2, last_error = $3 WHERE id = $4`, attempts, nextAttemptAt, reason, id)
	return err
}

func (r *PostgresJobRepository) ListByUser(user string) ([]VideoJob, error) {
	rows, err := r.db.Query(`SELECT id, username, email, object_key, status, error_message, created_at
		FROM video_jobs WHERE username = $1 ORDER BY created_at DESC`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobs := make([]VideoJob, 0)
	for rows.Next() {
		var job VideoJob
		if err := rows.Scan(&job.ID, &job.User, &job.Email, &job.ObjectKey, &job.Status, &job.Error, &job.CreatedAt); err != nil {
			return nil, err
		}
		job.OutputKey = outputKey(job.ID)
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func postgresConfig() PostgresConfig {
	dsn := envOr("POSTGRES_DSN", fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		envOr("POSTGRES_HOST", "localhost"),
		envOr("POSTGRES_PORT", "5432"),
		envOr("POSTGRES_USER", "video_processor"),
		envOr("POSTGRES_PASSWORD", ""),
		envOr("POSTGRES_DB", "video_processor"),
	))
	return PostgresConfig{DSN: dsn}
}
