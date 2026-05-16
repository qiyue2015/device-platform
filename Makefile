.PHONY: setup-local check-services check-db dev-backend dev-frontend check-backend check-frontend check

setup-local:
	@test -f backend/.env || cp backend/.env.example backend/.env
	@test -f frontend/.env.development || cp frontend/.env.example frontend/.env.development
	@echo "Local env files are ready."

check-services:
	@pg_isready -h localhost -p 5432
	@redis-cli -h localhost -p 6379 ping

check-db:
	@$(MAKE) setup-local >/dev/null
	@set -a; . backend/.env; set +a; \
	if [ -z "$$DATABASE_URL" ]; then \
		echo "DATABASE_URL is missing in backend/.env"; \
		exit 1; \
	fi; \
	psql "$$DATABASE_URL" -Atc "SELECT current_database();"

dev-backend:
	$(MAKE) -C backend run

dev-frontend:
	pnpm --dir frontend dev

check-backend:
	$(MAKE) -C backend test
	$(MAKE) -C backend lint

check-frontend:
	pnpm --dir frontend type:check
	pnpm --dir frontend build
	pnpm --dir frontend i18n:check

check: check-services check-backend check-frontend
