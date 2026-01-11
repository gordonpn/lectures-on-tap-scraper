default: build

build:
	go build ./cmd/lectures-notifier

run:
	set -a; source .env; set +a; ./lectures-notifier
