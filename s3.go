package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Storage struct {
	client *s3.Client
	bucket string
}

func NewS3Storage() (*S3Storage, error) {
	endpoint := envOr("S3_ENDPOINT", "http://localhost:9000")
	region := envOr("S3_REGION", "us-east-1")
	bucket := envOr("S3_BUCKET", "video-processor")
	accessKey := envOr("S3_ACCESS_KEY", "minio")
	secretKey := envOr("S3_SECRET_KEY", "minio123")

	cfg, err := config.LoadDefaultConfig(
		context.Background(),
		config.WithRegion(region),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				accessKey,
				secretKey,
				"",
			),
		),
	)
	if err != nil {
		return nil, err
	}

	client := s3.NewFromConfig(cfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(endpoint)
		options.UsePathStyle = true
	})

	return &S3Storage{
		client: client,
		bucket: bucket,
	}, nil
}

func (s *S3Storage) Init() error {
	ctx := context.Background()

	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	})

	if err == nil {
		return nil
	}

	_, err = s.client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(s.bucket),
	})

	if err != nil {
		return fmt.Errorf("erro ao criar bucket %s: %w", s.bucket, err)
	}

	return nil
}

func (s *S3Storage) Put(key string, source io.Reader) error {
	_, err := s.client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   source,
	})

	if err != nil {
		return fmt.Errorf("erro ao enviar objeto %s: %w", key, err)
	}

	return nil
}

func (s *S3Storage) Get(key string) (io.ReadCloser, error) {
	result, err := s.client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		return nil, fmt.Errorf("erro ao baixar objeto %s: %w", key, err)
	}

	return result.Body, nil
}

func s3Enabled() bool {
	return os.Getenv("S3_ENDPOINT") != ""
}