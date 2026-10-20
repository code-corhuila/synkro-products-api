package config

import (
	"errors"
	"os"
	"strings"
)

type Config struct {
	HTTPPort    string
	DatabaseURL string
	// JWTPublicKey is the PEM public key of synkro-auth-api, the only
	// holder of the private key (ADR-006). Only the public half is read.
	JWTPublicKey string
}

// Load reads the environment. DATABASE_URL is required: without it the
// service would have nowhere durable to write, and silently falling back
// to memory would lose data in a misconfigured deployment. Exactly one of
// JWT_PUBLIC_KEY (PEM) and JWT_PUBLIC_KEY_FILE (path) is required too:
// without a key no token can be verified, and the service must not start
// by accepting unverified ones. Errors name the variable, never its value.
func Load() (Config, error) {
	port := os.Getenv("HTTP_PORT")
	if port == "" {
		port = "8080"
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return Config{}, errors.New("DATABASE_URL is required (see .env.example)")
	}
	key, err := publicKey()
	if err != nil {
		return Config{}, err
	}
	return Config{HTTPPort: port, DatabaseURL: dbURL, JWTPublicKey: key}, nil
}

func publicKey() (string, error) {
	inline := os.Getenv("JWT_PUBLIC_KEY")
	file := os.Getenv("JWT_PUBLIC_KEY_FILE")
	if (strings.TrimSpace(inline) == "") == (file == "") {
		return "", errors.New("configure exactly one of JWT_PUBLIC_KEY or JWT_PUBLIC_KEY_FILE (see .env.example)")
	}
	if file == "" {
		return inline, nil
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return "", errors.New("cannot read JWT_PUBLIC_KEY_FILE")
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", errors.New("JWT_PUBLIC_KEY_FILE is empty")
	}
	return string(data), nil
}
