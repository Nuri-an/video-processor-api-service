package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type VideoJob struct {
	ID        string    `json:"id"`
	User      string    `json:"user"`
	Email     string    `json:"email,omitempty"`
	ObjectKey string    `json:"object_key"`
	OutputKey string    `json:"output_key"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
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
	if envOr("JWT_SECRET", "") == "" {
		log.Fatal("JWT_SECRET deve ser configurado")
	}
	jobs, err := NewPostgresJobRepository(postgresConfig())
	if err != nil {
		log.Fatal(err)
	}
	queue, err := NewRabbitQueue(rabbitConfig())
	if err != nil {
		log.Fatal(err)
	}
	logger, err := NewMongoLogger(mongoConfig())
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
	startOutboxDispatcher(context.Background(), api.jobs, api.queue)
	if err := api.logger.Init(); err != nil {
		log.Fatal(err)
	}

	r := gin.Default()
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "FIAP X API Gateway") })
	r.GET("/swagger", swaggerUI)
	r.GET("/swagger/openapi.yaml", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/yaml; charset=utf-8", openAPISpec)
	})
	r.POST("/auth/login", api.login)

	protected := r.Group("/")
	protected.Use(jwtAuthMiddleware())
	protected.POST("/upload", api.upload)
	protected.GET("/api/status", api.status)
	protected.GET("/download/:filename", api.download)

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
	user := c.GetString("user")

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

	job := VideoJob{ID: jobID, User: user, Email: c.GetString("email"), ObjectKey: objectKey, OutputKey: outputKey(jobID), Status: "Pendente", CreatedAt: time.Now()}
	if err := api.jobs.CreateJob(job); err != nil {
		api.logger.Log("job_database_error", jobID, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao registrar tarefa"})
		return
	}
	api.logger.Log("job_queued", job.ID, "video job stored in outbox")

	c.JSON(http.StatusAccepted, job)
}

func (api API) status(c *gin.Context) {
	user := c.GetString("user")

	jobs, err := api.jobs.ListByUser(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao consultar tarefas"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"jobs": jobs, "total": len(jobs)})
}

func (api API) download(c *gin.Context) {
	filename := filepath.Base(c.Param("filename"))
	owned, err := api.jobs.HasOutputForUser(filename, c.GetString("user"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao validar arquivo"})
		return
	}
	if !owned {
		c.JSON(http.StatusNotFound, gin.H{"error": "arquivo nao encontrado"})
		return
	}
	path := filepath.Join(envOr("API_OUTPUT_DIR", "outputs"), filename)
	if _, err := os.Stat(path); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "arquivo nao encontrado"})
		return
	}
	c.FileAttachment(path, filename)
}

func outputKey(jobID string) string {
	return "frames_" + jobID + ".zip"
}

func (api API) login(c *gin.Context) {
	var credentials struct {
		User     string `json:"user"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&credentials); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "informe user e password"})
		return
	}

	user := strings.TrimSpace(credentials.User)
	password := credentials.Password
	if user == "" || password == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "usuario ou senha invalidos"})
		return
	}
	authenticatedUser, err := api.jobs.AuthenticateUser(user, password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "usuario ou senha invalidos"})
		return
	}

	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": authenticatedUser.Username,
		"email": authenticatedUser.Email,
		"iat": now.Unix(),
		"exp": now.Add(24 * time.Hour).Unix(),
	})
	signedToken, err := token.SignedString([]byte(envOr("JWT_SECRET", "")))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao gerar token"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": signedToken})
}

func jwtAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authorization := strings.TrimSpace(c.GetHeader("Authorization"))
		if !strings.HasPrefix(authorization, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "informe o token JWT"})
			return
		}

		token, err := jwt.Parse(strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer ")), func(token *jwt.Token) (interface{}, error) {
			if token.Method != jwt.SigningMethodHS256 {
				return nil, fmt.Errorf("metodo de assinatura invalido")
			}
			return []byte(envOr("JWT_SECRET", "")), nil
		})
		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token JWT invalido ou expirado"})
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		user, userOK := claims["sub"].(string)
		if !ok || !userOK || strings.TrimSpace(user) == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token JWT sem usuario"})
			return
		}
		c.Set("user", user)
		if email, emailOK := claims["email"].(string); emailOK {
			c.Set("email", email)
		}
		c.Next()
	}
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