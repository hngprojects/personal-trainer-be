# Redis Setup

This project uses Redis as a JWT blocklist store for logout functionality.

## Requirements
- Docker and Docker Compose

## Running Redis locally

```bash
# Start Redis
make docker-up

# Stop Redis
make docker-down
```

## Environment variable

Add this to your `.env` file:

```bash
REDIS_URL=redis://localhost:6379
```

## How it works
- On logout, the refresh token's `jti` is stored in Redis with a 7-day TTL
- The auth middleware checks Redis on every protected request
- If the `jti` is found in Redis, the token is rejected with 401