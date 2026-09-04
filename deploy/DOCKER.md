# RelayDeck Docker Image

RelayDeck is an AI API Gateway Platform for distributing and managing AI product subscription API quotas.

## Quick Start

```bash
docker run -d \
  --name relaydeck \
  -p 8080:8080 \
  -e DATABASE_HOST=host \
  -e DATABASE_PORT=5432 \
  -e DATABASE_USER=relaydeck \
  -e DATABASE_PASSWORD="$DATABASE_PASSWORD" \
  -e DATABASE_DBNAME=relaydeck \
  -e DATABASE_SSLMODE=disable \
  -e REDIS_HOST=host \
  -e REDIS_PORT=6379 \
  -e REDIS_PASSWORD="${REDIS_PASSWORD:-}" \
  -e REDIS_DB=0 \
  jnyroad/relaydeck:latest
```

## Docker Compose

```yaml
version: '3.8'

services:
  relaydeck:
    image: jnyroad/relaydeck:latest
    ports:
      - "8080:8080"
    environment:
      - DATABASE_HOST=db
      - DATABASE_PORT=5432
      - DATABASE_USER=relaydeck
      - DATABASE_PASSWORD=${POSTGRES_PASSWORD:?set POSTGRES_PASSWORD in .env or the shell}
      - DATABASE_DBNAME=relaydeck
      - DATABASE_SSLMODE=disable
      - REDIS_HOST=redis
      - REDIS_PORT=6379
      - REDIS_PASSWORD=${REDIS_PASSWORD:-}
      - REDIS_DB=0
    depends_on:
      - db
      - redis

  db:
    image: postgres:15-alpine
    environment:
      - POSTGRES_USER=relaydeck
      - POSTGRES_PASSWORD=${POSTGRES_PASSWORD:?set POSTGRES_PASSWORD in .env or the shell}
      - POSTGRES_DB=relaydeck
    volumes:
      - postgres_data:/var/lib/postgresql/data

  redis:
    image: redis:7-alpine
    volumes:
      - redis_data:/data

volumes:
  postgres_data:
  redis_data:
```

Set `POSTGRES_PASSWORD` in a local `.env` file or the shell before running this
example. Do not commit that secret.

## Upgrade from a legacy `sub2api` database

`POSTGRES_DB` only initializes an empty PostgreSQL data volume; changing it does
not rename an existing database. For a deployment whose retained volume contains
`sub2api`, back it up and restore it into `relaydeck` before changing the
application database name. Replace the role placeholders with the existing
database role and an administrative role that may create databases:

```bash
docker compose stop relaydeck
docker compose exec db pg_dump -U <existing-db-role> -Fc sub2api > sub2api.backup
docker compose exec db createdb -U <existing-admin-role> -O <existing-db-role> relaydeck
docker compose exec -T db pg_restore -U <existing-db-role> -d relaydeck --clean --if-exists < sub2api.backup
```

After validating the restored data, set the application `DATABASE_DBNAME` (and
the Compose `POSTGRES_DB` value for future empty-volume initialization) to
`relaydeck`, then start the application again with `docker compose up -d relaydeck`.

## Startup and Database Recovery

RelayDeck runs database migrations while starting. PostgreSQL may still be
recovering briefly after a host or Docker daemon restart. The application
retries transient PostgreSQL startup and connection errors with bounded
exponential backoff, then continues startup when the database is ready.
Permanent errors such as invalid credentials, migration checksum mismatches,
SQL errors, and incompatible data fail immediately.

The Compose deployment also checks PostgreSQL readiness with both `pg_isready`
and a simple SQL query. `depends_on: condition: service_healthy` helps order a
fresh Compose start, but application-level retries are still required when
Docker restores existing containers after a host restart.

## Environment Variables

| Variable | Description | Required | Default |
|----------|-------------|----------|---------|
| `DATABASE_HOST` / `DATABASE_PORT` | PostgreSQL host and port | Yes | `localhost` / `5432` |
| `DATABASE_USER` / `DATABASE_PASSWORD` | PostgreSQL role and secret | Yes | `postgres` / *(empty)* |
| `DATABASE_DBNAME` / `DATABASE_SSLMODE` | PostgreSQL database and SSL mode | No | `relaydeck` / `disable` |
| `REDIS_HOST` / `REDIS_PORT` | Redis host and port | Yes | `localhost` / `6379` |
| `REDIS_USERNAME` / `REDIS_PASSWORD` / `REDIS_DB` | Redis authentication and database | No | *(empty)* / *(empty)* / `0` |
| `SERVER_PORT` / `SERVER_MODE` | HTTP server port and Gin mode | No | `8080` / `release` |

## Supported Architectures

- `linux/amd64`
- `linux/arm64`

## Tags

- `latest` - Latest stable release
- `x.y.z` - Specific version
- `x.y` - Latest patch of minor version
- `x` - Latest minor of major version

## Links

- [GitHub Repository](https://github.com/JnyRoad/RelayDeck)
- [Documentation](https://github.com/JnyRoad/RelayDeck#readme)
