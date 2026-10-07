package config

import (
	"errors"
	"os"
)

type Config struct {
	HTTPPort    string
	DatabaseURL string
}

// Load reads the environment. DATABASE_URL is required: without it the
// service would have nowhere durable to write, and silently falling back
// to memory would lose data in a misconfigured deployment.
func Load() (Config, error) {
	port := os.Getenv("HTTP_PORT")
	if port == "" {
		port = "8080"
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return Config{}, errors.New("DATABASE_URL is required (see .env.example)")
	}
	return Config{HTTPPort: port, DatabaseURL: dbURL}, nil
}
