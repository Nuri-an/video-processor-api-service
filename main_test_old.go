package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type fakeJobRepository struct {
	jobs       []VideoJob
	users      map[string]User
	lastUser   string
	lastStatus string
}

func (f *fakeJobRepository) Init() error { return nil }

func (f *fakeJobRepository) Save(job VideoJob) error {
	f.jobs = append(f.jobs, job)
	return nil
}

func (f *fakeJobRepository) CreateJob(job VideoJob) error {
	return f.Save(job)
}

func (f *fakeJobRepository) UpdateStatus(id, status, errorMessage string) error {
	f.lastUser = id
	f.lastStatus = status
	for i := range f.jobs {
		if f.jobs[i].ID == id {
			f.jobs[i].Status = status
			f.jobs[i].Error = errorMessage
			return nil
		}
	}
	return nil
}

func (f *fakeJobRepository) ListPendingOutbox(limit int) ([]OutboxEntry, error) { return nil, nil }
func (f *fakeJobRepository) MarkOutboxSent(id int64) error { return nil }
func (f *fakeJobRepository) MarkOutboxFailed(id int64, attempts int, nextAttemptAt time.Time, reason string) error {
	return nil
}
func (f *fakeJobRepository) ListByUser(user string) ([]VideoJob, error) {
	jobs := make([]VideoJob, 0)
	for _, job := range f.jobs {
		if job.User == user {
			jobs = append(jobs, job)
		}
	}
	return jobs, nil
}
func (f *fakeJobRepository) AuthenticateUser(username, password string) (User, error) {
	if user, ok := f.users[username]; ok {
		if user.PasswordHash == password {
			return user, nil
		}
		return User{}, fmt.Errorf("credenciais invalidas")
	}
	return User{}, fmt.Errorf("usuario nao encontrado")
}
func (f *fakeJobRepository) HasOutputForUser(filename, username string) (bool, error) {
	for _, job := range f.jobs {
		if job.User == username && outputKey(job.ID) == filename {
			return true, nil
		}
	}
	return false, nil
}

type fakeQueue struct { published []VideoJob }

func (q *fakeQueue) Init() error { return nil }
func (q *fakeQueue) Publish(job VideoJob) error {
	q.published = append(q.published, job)
	return nil
}

type fakeStorage struct{ root string }

func (s *fakeStorage) Init() error { return os.MkdirAll(s.root, 0o755) }
func (s *fakeStorage) Put(key string, source io.Reader) error {
	path := filepath.Join(s.root, filepath.Base(key))
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, source)
	return err
}

type fakeLogger struct{ events []string }

func (l *fakeLogger) Init() error { return nil }
func (l *fakeLogger) Log(event, jobID, message string) { l.events = append(l.events, event+":"+jobID+":"+message) }

func TestOutputKey(t *testing.T) {
	if got := outputKey("123"); got != "frames_123.zip" {
		t.Fatalf("outputKey() = %q", got)
	}
}

func TestJWTAuthMiddlewareRejectsMissingToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(jwtAuthMiddleware())
	router.GET("/protected", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestJWTAuthMiddlewareAcceptsValidToken(t *testing.T) {
	previous := os.Getenv("JWT_SECRET")
	t.Cleanup(func() { os.Setenv("JWT_SECRET", previous) })
	os.Setenv("JWT_SECRET", "test-secret")
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "admin",
		"exp": time.Now().Add(time.Minute).Unix(),
	})
	signed, err := token.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(jwtAuthMiddleware())
	router.GET("/protected", func(c *gin.Context) { c.String(http.StatusOK, c.GetString("user")) })
	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.Header.Set("Authorization", "Bearer "+signed)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "admin" {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
}

func TestAPIUploadCreatesJobAndStoresVideo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	storageDir := t.TempDir()
	repo := &fakeJobRepository{users: map[string]User{"admin": {Username: "admin", Email: "admin@example.com", PasswordHash: "secret"}}}
	queue := &fakeQueue{}
	logger := &fakeLogger{}
	api := API{storage: &fakeStorage{root: storageDir}, queue: queue, jobs: repo, logger: logger}

	if err := api.storage.Init(); err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("video", "sample.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("video-bytes")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	os.Setenv("JWT_SECRET", "test-secret")
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  "admin",
		"email": "admin@example.com",
		"exp":  time.Now().Add(time.Minute).Unix(),
	})
	signed, err := token.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", "Bearer "+signed)
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = request
	ctx.Set("user", "admin")
	ctx.Set("email", "admin@example.com")
	api.upload(ctx)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusAccepted, response.Body.String())
	}
	if len(repo.jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(repo.jobs))
	}
	if len(queue.published) != 0 {
		t.Fatalf("expected no queue publish in direct upload flow, got %d", len(queue.published))
	}
	if !strings.HasPrefix(repo.jobs[0].ObjectKey, "") || repo.jobs[0].Status != "Pendente" {
		t.Fatalf("unexpected job data: %#v", repo.jobs[0])
	}
	matches, err := filepath.Glob(filepath.Join(storageDir, "*.mp4"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected file stored in local storage, matches=%v err=%v", matches, err)
	}
}

func TestLoginGeneratesTokenForValidUser(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret")
	repo := &fakeJobRepository{users: map[string]User{
		"admin": {Username: "admin", Email: "admin@example.com", PasswordHash: "secret"},
	}}
	api := API{jobs: repo}

	request := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"user":"admin","password":"secret"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = request
	api.login(ctx)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["token"] == "" {
		t.Fatalf("expected token in response: %s", response.Body.String())
	}
}
