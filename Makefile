.PHONY: up down restart logs init

init:
	#fix this hack
	mkdir -p notes
	sudo chown -R 1000:1000 notes/

up:
	docker-compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build

down:
	docker-compose -f docker-compose.yml -f docker-compose.prod.yml down

restart:
	docker-compose -f docker-compose.yml -f docker-compose.prod.yml restart

logs:
	docker-compose -f docker-compose.yml -f docker-compose.prod.yml logs -f
