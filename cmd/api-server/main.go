package main

import (
	"log"

	"github.com/iamvkosarev/audio-tag-editor/internal/app"
	"github.com/iamvkosarev/audio-tag-editor/internal/config"
	"github.com/joho/godotenv"
)

func main() {
	godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	application, err := app.New(cfg)
	if err != nil {
		log.Fatalf("failed to create application: %v", err)
	}
	if err := application.Run(); err != nil {
		log.Fatalf("failed to run: %v", err)
	}
}
