.PHONY: build run docker-build docker-run test-alert help

APP_NAME = alert-tg-adapter
TAG = v1.11
PORT = 9087

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: down ## Build the binary
	go build -o $(APP_NAME) .

run: build ## Build and run the binary locally
	./$(APP_NAME)
down: 
	-kill -9 $$(lsof -t -i:9087)
docker-build: ## Build the docker image for amd64
	docker buildx build --platform linux/amd64 -t $(APP_NAME):$(TAG) .

docker-push: ## Push the docker image to Harbor
	docker push $(APP_NAME):$(TAG)

deploy: docker-build docker-push ## Build and push the docker image

all: docker-build docker-push

docker-run: ## Run with docker-compose
	docker-compose up -d

docker-stop: ## Stop docker-compose
	docker-compose down

test-alert: ## Send a test alert to the running instance (requires running instance)
	@echo "Sending test alert to http://localhost:$(PORT)/webhook..."
	@curl -X POST -H "Content-Type: application/json" \
		-d @test/payload.json \
		"http://localhost:$(PORT)/webhook?chat_id=$(CHAT_ID)"
	@echo "\nDone."

test-alert-resolved: ## Send a resolved test alert
	@echo "Sending resolved alert..."
	@sed 's/"status": "firing"/"status": "resolved"/g' test/payload.json | \
	curl -X POST -H "Content-Type: application/json" \
		-d @- \
		"http://localhost:$(PORT)/webhook?chat_id=$(CHAT_ID)"
	@echo "\nDone."
