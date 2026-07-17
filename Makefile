.PHONY: up down restart logs ps init

-include .env

DATA_DIR ?= data
NOTES_UID ?= 1000
NOTES_GID ?= 1000
COMPOSE := docker-compose

init:
	mkdir -p "$(DATA_DIR)"
	@if [ ! -w "$(DATA_DIR)" ]; then \
		echo "Fixing ownership of $(DATA_DIR)"; \
		sudo chown -R "$(NOTES_UID):$(NOTES_GID)" "$(DATA_DIR)"; \
	fi

up: init
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) --profile production down

restart:
	$(COMPOSE) restart

logs:
	$(COMPOSE) logs -f

ps:
	$(COMPOSE) ps
