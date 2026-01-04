package config

import (
	"fmt"
	"github.com/ilyakaznacheev/cleanenv"
	"time"
)

type App struct {
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" env-default:"10s"`
	LogMode         string        `env:"LOG_MODE" env-default:"debug"` // debug, dev or prod
}

type ServerConfig struct {
	Host         string        `env:"SERVER_HOST" env-default:"0.0.0.0"`
	Port         string        `env:"HTTP_PORT" env-default:"8080"`
	IdleTimeout  time.Duration `env:"SERVER_IDLE_TIMEOUT" env-default:"60s""`
	ReadTimeout  time.Duration `env:"HTTP_READ_TIMEOUT" env-default:"15s"`
	WriteTimeout time.Duration `env:"HTTP_WRITE_TIMEOUT" env-default:"15s"`
}

type Config struct {
	Server ServerConfig
	App    App
}

func Load() (*Config, error) {
	var cfg Config
	if err := cleanenv.ReadEnv(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *ServerConfig) Address() string {
	return fmt.Sprintf("%s:%s", c.Host, c.Port)
}
