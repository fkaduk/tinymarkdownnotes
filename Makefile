.PHONY: up down restart logs

up:
	docker-compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build

down:
	docker-compose -f docker-compose.yml -f docker-compose.prod.yml down

restart:
	docker-compose -f docker-compose.yml -f docker-compose.prod.yml restart

logs:
	docker-compose -f docker-compose.yml -f docker-compose.prod.yml logs -f
