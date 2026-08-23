.PHONY: build test test-race test-integration test-migrations sqlc sqlc-check \
	verify verify-go-static verify-go-tests verify-frontends verify-compose \
	verify-migrations ci compose-up compose-down

build:
	./scripts/ci/go-modules.sh build

test:
	./scripts/ci/go-modules.sh test

test-race:
	./scripts/ci/go-modules.sh race

test-integration:
	./scripts/ci/integration.sh

test-migrations:
	./scripts/ci/migrations-empty.sh
	./scripts/ci/appointment-migration-upgrade.sh

sqlc:
	./scripts/ci/sqlc.sh generate

sqlc-check:
	./scripts/ci/sqlc.sh check

verify-go-static:
	./scripts/ci/go-modules.sh tidy-check
	./scripts/ci/go-modules.sh vet
	./scripts/ci/go-modules.sh build

verify-go-tests:
	./scripts/ci/go-modules.sh test
	./scripts/ci/go-modules.sh race

verify-frontends:
	./scripts/ci/frontend.sh admin-web
	./scripts/ci/frontend.sh booking-web

verify-compose:
	./scripts/ci/compose.sh

verify-migrations:
	./scripts/ci/migration-checksums.sh
	./scripts/ci/migrations-empty.sh

verify: verify-go-static verify-go-tests sqlc-check verify-frontends verify-compose

ci: verify verify-migrations test-integration

compose-up:
	docker compose up --build

compose-down:
	docker compose down
