.PHONY: sqlc run
sqlc:
	sqlc generate
run:
	go run ./cmd/api