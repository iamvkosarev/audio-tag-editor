.PHONY: run templ-generate

run:
	docker-compose up --build -d

templ-generate:
	templ generate ./internal/templates
