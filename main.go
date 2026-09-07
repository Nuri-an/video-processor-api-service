package main

import (
	_ "embed"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type VideoJob struct {
	ID        string    `json:"id"`
	User      string    `json:"user"`
	ObjectKey string    `json:"object_key"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type API struct {
	storage Storage
	queue   Queue
	jobs    JobRepository
	logger  Logger
}

//go:embed docs/openapi.yaml
var openAPISpec []byte

func main() {
	jobs, err := NewPostgresJobRepository(postgresConfig())
	if err != nil {
		log.Fatal(err)
	}
	queue, err := NewRabbitQueue(rabbitConfig())
	if err != nil {
		log.Fatal(err)
	}
	logger, err := NewRedisLogger(redisConfig())
	if err != nil {
		log.Fatal(err)
	}

	api := API{
		storage: LocalStorage{Root: envOr("API_STORAGE_DIR", "uploads")},
		queue:   queue,
		jobs:    jobs,
		logger:  logger,
	}

	if err := api.storage.Init(); err != nil {
		log.Fatal(err)
	}
	if err := api.jobs.Init(); err != nil {
		log.Fatal(err)
	}
	if err := api.queue.Init(); err != nil {
		log.Fatal(err)
	}
	if err := api.logger.Init(); err != nil {
		log.Fatal(err)
	}

	r := gin.Default()
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "FIAP X API Gateway") })
	r.GET("/swagger", swaggerUI)
	r.GET("/swagger/openapi.yaml", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/yaml; charset=utf-8", openAPISpec)
	})
	r.POST("/upload", api.upload)
	r.GET("/api/status", api.status)
	r.GET("/download/:filename", api.download)

	log.Println("API Gateway iniciado na porta 8080")
	log.Fatal(r.Run(":8080"))
}

func swaggerUI(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(`<!doctype html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<title>Video Processor API - Swagger</title>
	<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
	<div id="swagger-ui"></div>
	<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
	<script>
		window.onload = () => SwaggerUIBundle({
			url: "/swagger/openapi.yaml",
			dom_id: "#swagger-ui"
		});
	</script>
</body>
</html>`))
}

func (api API) upload(c *gin.Context) {
	user, ok := authenticatedUser(c)
	if !ok {
		return
	}

	file, header, err := c.Request.FormFile("video")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "envie um arquivo no campo video"})
		return
	}
	defer file.Close()

	if !isValidVideoFile(header.Filename) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "formato de video nao suportado"})
		return
	}

	jobID := fmt.Sprintf("%d", time.Now().UnixNano())
	objectKey := filepath.Join(jobID+"_"+filepath.Base(header.Filename))
	if err := api.storage.Put(objectKey, file); err != nil {
		api.logger.Log("upload_storage_error", jobID, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao armazenar video"})
		return
	}

	job := VideoJob{ID: jobID, User: user, ObjectKey: objectKey, Status: "Pendente", CreatedAt: time.Now()}
	if err := api.jobs.Save(job); err != nil {
		api.logger.Log("job_database_error", jobID, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao registrar tarefa"})
		return
	}
	if err := api.queue.Publish(job); err != nil {
		api.logger.Log("job_queue_error", jobID, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao publicar tarefa"})
		return
	}
	api.logger.Log("job_published", job.ID, "video job published")

	c.JSON(http.StatusAccepted, job)
}

func (api API) status(c *gin.Context) {
	user, ok := authenticatedUser(c)
	if !ok {
		return
	}

	jobs, err := api.jobs.ListByUser(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao consultar tarefas"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"jobs": jobs, "total": len(jobs)})
}

func (api API) download(c *gin.Context) {
	if _, ok := authenticatedUser(c); !ok {
		return
	}
	filename := filepath.Base(c.Param("filename"))
	path := filepath.Join(envOr("API_OUTPUT_DIR", "outputs"), filename)
	if _, err := os.Stat(path); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "arquivo nao encontrado"})
		return
	}
	c.FileAttachment(path, filename)
}

func authenticatedUser(c *gin.Context) (string, bool) {
	user := strings.TrimSpace(c.GetHeader("X-User"))
	password := c.GetHeader("X-Password")
	if user == "" || password == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "informe X-User e X-Password"})
		return "", false
	}
	if user != envOr("API_USER", "admin") || password != envOr("API_PASSWORD", "admin") {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "usuario ou senha invalidos"})
		return "", false
	}
	return user, true
}

func isValidVideoFile(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".mp4", ".avi", ".mov", ".mkv", ".wmv", ".flv", ".webm":
		return true
	default:
		return false
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}