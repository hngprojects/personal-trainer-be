# Observability Setup

This backend is instrumented to work with the separate bare-metal observability
and reliability platform. The observability stack can scrape metrics from this
service, receive traces from this service, and correlate JSON logs with trace
IDs when logs are shipped from the backend host.

## What Was Implemented

- Prometheus metrics endpoint at `/metrics`.
- Request counter metric: `http_requests_total`.
- Request latency histogram: `http_request_duration_seconds`.
- OpenTelemetry tracing initialization on server startup.
- Gin OpenTelemetry middleware for request traces.
- `trace_id` and `span_id` fields in request logs.
- Environment-based observability configuration.

## Runtime Configuration

Set these environment variables for the backend service:

```env
SERVICE_NAME=personal-trainer-be
OTEL_ENABLED=true
OTEL_EXPORTER_OTLP_ENDPOINT=<observability-server-private-ip-or-dns>:4317
LOG_FORMAT=json
```

Recommended production values:

- `SERVICE_NAME`: keep this aligned with the service name configured in the
  observability platform, usually `personal-trainer-be`.
- `OTEL_ENABLED`: set to `true` to export traces.
- `OTEL_EXPORTER_OTLP_ENDPOINT`: point this to the observability server's
  OpenTelemetry Collector gRPC endpoint.
- `LOG_FORMAT`: set to `json` so Loki/Grafana can parse log fields reliably.

To disable trace export without removing the instrumentation:

```env
OTEL_ENABLED=false
```

## Metrics

The backend exposes Prometheus metrics at:

```text
GET /metrics
```

The observability platform should scrape:

```hcl
monitored_service_metrics_target = "<be-server-private-ip-or-dns>:8080"
```

Metrics added by the backend:

```text
http_requests_total{service,method,route,status}
http_request_duration_seconds_bucket{service,method,route,status,le}
http_request_duration_seconds_sum{service,method,route,status}
http_request_duration_seconds_count{service,method,route,status}
```

The `/metrics` endpoint also includes Go runtime and process metrics from the
Prometheus Go client.

## Health Checks

The existing health endpoint remains:

```text
GET /api/v1/health
```

The observability platform should probe:

```hcl
monitored_service_health_url = "http://<be-server-private-ip-or-dns>:8080/api/v1/health"
```

## Traces

When `OTEL_ENABLED=true`, the server initializes an OpenTelemetry tracer
provider on startup and exports spans with OTLP/gRPC.

The Gin router uses OpenTelemetry middleware, so incoming HTTP requests are
traced automatically. The service name and deployment environment are attached
as resource attributes.

Required network path:

```text
BE server -> observability server:4317
```

The central OpenTelemetry Collector receives these traces and forwards them to
Tempo.

## Logs

Request logs now include:

```text
trace_id
span_id
```

Use `LOG_FORMAT=json` in deployed environments so these fields can be parsed by
the log pipeline.

Because the BE and observability stack run on different servers, the central
collector cannot directly read this server's systemd journal. To send BE logs to
Loki, run a lightweight OpenTelemetry Collector agent on the BE server that
reads `personal-trainer-be.service` from journald and exports logs to the
central collector.

## Observability Platform Values

On the observability server, configure the platform's `terraform/terraform.tfvars`
with the deployed BE details:

```hcl
monitored_service_name = "personal-trainer-be"
monitored_service_metrics_target = "<be-server-private-ip-or-dns>:8080"
monitored_service_health_url = "http://<be-server-private-ip-or-dns>:8080/api/v1/health"
monitored_service_systemd_unit = "personal-trainer-be.service"
collect_local_monitored_service_logs = false
```

Use `collect_local_monitored_service_logs = false` when the BE is on a different
server from the observability stack.

## Quick Verification

From the observability server:

```bash
curl http://<be-server-private-ip-or-dns>:8080/api/v1/health
curl http://<be-server-private-ip-or-dns>:8080/metrics
```

From the BE server:

```bash
curl http://<observability-server-private-ip-or-dns>:4317
```

The `curl` to port `4317` may not return an HTTP success response because it is
an OTLP/gRPC port, but it should confirm whether the network path is reachable.

Run the backend test suite:

```bash
go test ./...
```
