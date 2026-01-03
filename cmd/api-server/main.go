package main

import (
	"log"

	"github.com/iamvkosarev/music-tag-editor/internal/app"
	"github.com/iamvkosarev/music-tag-editor/internal/config"
)

func main() {
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	application := app.New(cfg)
	application.Run()
}
