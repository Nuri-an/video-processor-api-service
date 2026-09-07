package main

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

type JobRepository interface {
	Init() error
	Save(VideoJob) error
	ListByUser(string) ([]VideoJob, error)
}

type PostgresConfig struct{ DSN string }

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
	_, err := r.db.Exec(`CREATE TABLE IF NOT EXISTS video_jobs (
		id TEXT PRIMARY KEY,
		username TEXT NOT NULL,
		object_key TEXT NOT NULL,
		status TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`)
	return err
}

func (r *PostgresJobRepository) Save(job VideoJob) error {
	_, err := r.db.Exec(`INSERT INTO video_jobs (id, username, object_key, status, created_at)
		VALUES ($1, $2, $3, $4, $5)`, job.ID, job.User, job.ObjectKey, job.Status, job.CreatedAt)
	return err
}

func (r *PostgresJobRepository) ListByUser(user string) ([]VideoJob, error) {
	rows, err := r.db.Query(`SELECT id, username, object_key, status, created_at
		FROM video_jobs WHERE username = $1 ORDER BY created_at DESC`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobs := make([]VideoJob, 0)
	for rows.Next() {
		var job VideoJob
		if err := rows.Scan(&job.ID, &job.User, &job.ObjectKey, &job.Status, &job.CreatedAt); err != nil {
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
		envOr("POSTGRES_PASSWORD", "video_processor"),
		envOr("POSTGRES_DB", "video_processor"),
	))
	return PostgresConfig{DSN: dsn}
}
