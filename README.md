# loki-ingest

A high-performance Go microservice that receives JSON logs via WebSocket and forwards them to Grafana Loki.

## Features

- WebSocket log ingestion with 500+ concurrent connections
- Intelligent batching and retry logic with exponential backoff
- Automatic label extraction from log fields
- Health and readiness endpoints
- Graceful shutdown with zero log loss
- Docker-ready with optimized binary (7MB)

## Quick Start

### Docker

```bash
docker run -d -p 8080:8080 \
  -e LOKI_URL=http://loki:3100 \
  loki-ingest:latest
```

### Local Development

```bash
make build    # Build optimized binary
make run      # Run locally
make test     # Run test suite (88%+ coverage)
```

## Configuration

Environment variables:

| Variable | Default | Description |
| --- | --- | --- |
| `SERVER_PORT` | `8080` | HTTP server port |
| `LOG_LEVEL` | `info` | Log level (debug/info/warn/error) |
| `MAX_CONNECTIONS` | `500` | Maximum concurrent WebSocket connections |
| `LOKI_URL` | `http://localhost:3100` | Loki server URL |
| `LOKI_BATCH_SIZE` | `100` | Logs per batch |
| `LOKI_BATCH_WAIT` | `5s` | Max wait before sending batch |
| `LOKI_TIMEOUT` | `10s` | HTTP request timeout |
| `LOKI_RETRY_ATTEMPTS` | `3` | Number of retry attempts |
| `LOKI_TENANT_ID` | `` | Multi-tenant Loki org ID |
| `LOKI_BASIC_AUTH_USER` | `` | Basic auth username |
| `LOKI_BASIC_AUTH_PASS` | `` | Basic auth password |

See `.env.example` for complete configuration options.

## API Endpoints

**WebSocket**: `ws://localhost:8080/ws` - Send JSON logs

**Health**: `GET /health` - Returns 200 OK

**Readiness**: `GET /ready` - Returns connection status:

```json
{
  "ready": true,
  "connections": 42,
  "max_connections": 500
}
```

## Log Format

Send JSON logs via WebSocket. Common fields are automatically extracted:

**Timestamp**: `timestamp`, `ts`, `time`, `@timestamp`  
**Level**: `level`, `severity`, `loglevel`, `log_level`  
**Message**: `message`, `msg`, `log`, `text`  
**App**: `app`, `application`, `service`, `service_name`  
**Environment**: `environment`, `env`  
**Host**: `host`, `hostname`, `node`  
**Container**: `container`, `container_id`, `container_name`

### Control System Logs

For control system processors (NetLinx, Crestron, etc.), additional fields are supported:

**ID**: `id` (log entry UUID, included in message not labels)  
**Client**: `client` (control system client identifier)  
**Room**: `roomName` (room or space identifier)  
**System Type**: `systemType` (e.g., NetLinx, Crestron)  
**Firmware**: `firmwareVersion` (control system firmware version)  
**IP Address**: `ipAddress` (control system IP address)

### Examples

Standard log:

```json
{
  "timestamp": "2026-01-26T10:30:00Z",
  "level": "error",
  "message": "Database connection failed",
  "app": "api-service",
  "environment": "production",
  "host": "server-01"
}
```

Control system log:

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "timestamp": "2026-01-26T10:30:00Z",
  "level": "warn",
  "message": "Panel disconnected",
  "client": "ExampleCustomer",
  "hostname": "av-processor-01",
  "roomName": "Conference Room A",
  "systemType": "NetLinx",
  "firmwareVersion": "1.8.205",
  "ipAddress": "192.168.1.100"
}
```

## License

[MIT](./LICENSE)
