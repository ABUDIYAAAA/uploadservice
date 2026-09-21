package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

type Config struct {
	// Server
	Port string `env:"PORT,required"`

	// Auth / Identity Service
	IdentityServiceURL string `env:"IDENTITY_SERVICE_URL" envDefault:"http://localhost:8080"`
	ServiceID          string `env:"SERVICE_ID,required"`
	ServiceSecret      string `env:"SERVICE_SECRET,required"`

	// S3
	AccessKeyID     string `env:"ACCESS_KEY_ID,required"`
	SecretAccessKey string `env:"SECRET_KEY,required"`
	BucketName      string `env:"BUCKET_NAME,required"`
	APIURL          string `env:"API_URL,required"`
}

func NewConfig() (*Config, error) {
	_ = godotenv.Load()

	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("parse config environment variables: %w", err)
	}

	return &cfg, nil
}
