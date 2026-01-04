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

	application := app.New(cfg)
	application.Run()
}
