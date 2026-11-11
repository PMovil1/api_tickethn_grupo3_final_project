package config

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	AppEnv      string
	Port        string
	DatabaseURL string
}

func Load() (Config, error) {
	appEnv := os.Getenv("APP_ENV")
	if appEnv == "" || appEnv == "development" {
		if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
			return Config{}, errors.New("could not load .env; check file permissions and syntax")
		}
		appEnv = os.Getenv("APP_ENV")
	}
	if appEnv == "" {
		appEnv = "development"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	portNumber, err := strconv.ParseUint(port, 10, 16)
	if err != nil || portNumber == 0 {
		return Config{}, errors.New("PORT must be an integer between 1 and 65535")
	}

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}

	return Config{AppEnv: appEnv, Port: strconv.FormatUint(portNumber, 10), DatabaseURL: databaseURL}, nil
}
