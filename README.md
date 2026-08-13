# Viettel Infrastructure Monitoring Control Plane

A production-oriented infrastructure monitoring and security control plane built in Go as an extension of the upstream VMware Terraform Provider for VMware Cloud Director.

The project explores how a large infrastructure platform can securely manage, monitor, and report on thousands of server targets while remaining observable and resilient to downstream failures.

> **Project note:** This repository is an independent engineering project built on top of the open-source VMware Cloud Director Terraform provider. It is not proprietary Viettel source code and does not contain internal Viettel systems, credentials, or data.

---

## Overview

Large infrastructure environments need more than simple health checks.

A practical control plane needs to answer questions such as:

- Is a server currently reachable?
- How has its availability changed over time?
- Can thousands of targets be checked concurrently without unbounded goroutine creation?
- What happens when a monitoring dependency becomes unavailable?
- How should repeated failures be isolated?
- Which users are allowed to modify or inspect infrastructure?
- Can privileged operations be reconstructed later?
- How are internal service-to-service calls authenticated?
- Can operators observe latency, failures, and request volume?

This project implements those concerns as a set of Go services around an infrastructure monitoring API.

The system combines:

- Gin HTTP APIs
- OpenAPI
- JWT authentication
- Role-based access control
- PostgreSQL
- Redis
- Elasticsearch
- gRPC
- Protocol Buffers
- mutual TLS
- audit logging
- rate limiting
- retry with exponential backoff
- circuit breaking
- Prometheus metrics
- bounded-concurrency health checking
- Jenkins CI

The monitoring engine has been validated against workloads containing **10,000 simulated server targets**.

---

# Architecture

```text
                         Client / Operator
                                |
                                |
                         HTTP / JSON API
                                |
                                v
                  +---------------------------+
                  |     Gin Control Plane     |
                  |                           |
                  |  JWT Authentication       |
                  |  RBAC Authorization       |
                  |  Rate Limiting            |
                  |  Audit Logging            |
                  |  Prometheus Metrics       |
                  +-------------+-------------+
                                |
                         mTLS-secured gRPC
                         /              \
                        /                \
                       v                  v
          +---------------------+   +---------------------+
          | Monitoring Service  |   | Reporting Service   |
          |      :50051         |   |       :50052        |
          +----------+----------+   +----------+----------+
                     |                         |
                     |                         |
          Bounded Worker Pool             Uptime Queries
                     |
             Concurrent TCP Checks
                     |
              Server Infrastructure


              Shared Infrastructure Layer

       +----------------+  +---------------+
       |   PostgreSQL   |  |     Redis     |
       |                |  |               |
       | servers        |  | server cache  |
       | health_checks  |  | TTL caching   |
       | audit events   |  |               |
       +----------------+  +---------------+
                |
                |
       +--------------------+
       |   Elasticsearch    |
       |                    |
       | server documents   |
       | health-check docs  |
       +--------------------+

                    Observability
                         |
                         v
                  +-------------+
                  | Prometheus  |
                  |  /metrics   |
                  +-------------+
```

---

# Core Components

## 1. HTTP Control Plane

The main API is implemented using Gin.

The control plane provides endpoints for:

- health checks
- authentication
- server creation
- server listing
- individual server retrieval
- server updates
- health-check execution
- uptime reporting
- security audit retrieval
- Prometheus metrics

Example routes include:

```text
GET    /healthz
POST   /auth/token

POST   /v1/servers
GET    /v1/servers
GET    /v1/servers/:id
PATCH  /v1/servers/:id

POST   /v1/servers/:id/check
GET    /v1/servers/:id/report

GET    /v1/audit

GET    /metrics
```

The API communicates with internal monitoring and reporting services over gRPC rather than embedding all infrastructure logic directly in the HTTP layer.

This keeps transport, control-plane logic, monitoring, and reporting separated.

---

# Authentication and Authorization

## JWT Authentication

Protected API endpoints require a JWT bearer token.

Example:

```bash
curl -X POST http://localhost:8080/auth/token \
  -H "Content-Type: application/json" \
  -d '{"subject":"operator-user","role":"operator"}'
```

Authenticated requests use:

```text
Authorization: Bearer <token>
```

Tokens include identity and authorization information used by the control plane.

The authentication layer validates:

- token signature
- signing algorithm
- expiration
- claims
- assigned role

Invalid or expired credentials are rejected before protected handlers execute.

---

## Role-Based Access Control

The API supports role-aware authorization rather than treating every authenticated user as an administrator.

Roles are designed around infrastructure responsibilities such as:

```text
viewer
operator
admin
```

Conceptually:

```text
viewer
  |
  +-- inspect infrastructure
  +-- inspect health information


operator
  |
  +-- viewer permissions
  +-- trigger operational health checks
  +-- perform permitted infrastructure operations


admin
  |
  +-- operator permissions
  +-- privileged security operations
  +-- inspect audit information
```

Authorization checks are enforced at the API layer before privileged actions execute.

---

# Audit Logging

Infrastructure operations should be attributable to an actor.

The control plane therefore records security-sensitive operations in an audit trail.

An audit event can contain fields such as:

```json
{
  "actor": "nam",
  "role": "operator",
  "action": "server.check",
  "resource": "server",
  "resource_id": "67af77d9-793e-49ef-b926-e81ddcfe42d5",
  "success": true
}
```

This makes it possible to answer:

```text
Who performed the operation?
What role did they have?
What action was attempted?
Which infrastructure resource was affected?
Did the operation succeed?
When did it happen?
```

Audit records are persisted so they survive application restarts.

Privileged users can inspect audit history through the API.

Example:

```bash
curl http://localhost:8080/v1/audit \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

---

# Rate Limiting

The control plane includes request rate limiting to reduce abuse and protect expensive infrastructure operations.

This is especially useful for endpoints that may trigger:

- network probes
- database activity
- gRPC requests
- downstream service calls

Rate limiting occurs before expensive request processing where possible.

This helps prevent a single client from consuming disproportionate control-plane capacity.

---

# Secure Service-to-Service Communication

The HTTP API communicates with internal services using gRPC and Protocol Buffers.

Two internal services are separated from the main API:

```text
MonitoringService
ReportingService
```

The generated protobuf clients and servers live under:

```text
gen/monitoring/v1/
gen/reporting/v1/
```

Service definitions live under:

```text
proto/monitoring.proto
proto/reporting.proto
```

---

## Monitoring Service

The monitoring service exposes operations including individual and batch health checks.

Conceptually:

```protobuf
service MonitoringService {
    rpc Check(CheckRequest) returns (CheckResponse);

    rpc CheckBatch(stream CheckRequest)
        returns (stream CheckResponse);
}
```

The service performs network checks and records results such as:

```json
{
  "server_id": "67af77d9-793e-49ef-b926-e81ddcfe42d5",
  "up": true,
  "latency_ms": 1,
  "checked_at": "2026-08-13T13:09:32Z"
}
```

---

## Reporting Service

The reporting service calculates availability information over a requested time range.

Conceptually:

```protobuf
service ReportingService {
    rpc GetUptime(UptimeRequest)
        returns (UptimeResponse);
}
```

Example response:

```json
{
  "server_id": "67af77d9-793e-49ef-b926-e81ddcfe42d5",
  "checks": 2,
  "successful": 2,
  "uptime_percent": 100
}
```

The API uses a 24-hour reporting window by default while the underlying reporting interface supports configurable RFC3339 time ranges.

---

# Mutual TLS

Internal gRPC communication can be protected using mutual TLS.

Unlike ordinary TLS, where only the server presents a certificate, mTLS allows both sides of the connection to authenticate each other.

Conceptually:

```text
API
 |
 | presents API certificate
 |
 | verifies monitoring certificate
 v
Monitoring Service


API
 |
 | presents API certificate
 |
 | verifies reporting certificate
 v
Reporting Service
```

This provides an additional trust boundary between externally facing control-plane traffic and internal services.

Private keys and locally generated certificate material are intentionally excluded from version control.

Never commit:

```text
*.key
private certificates
production secrets
JWT secrets
```

---

# Resilience

Distributed systems need to tolerate dependency failures rather than assuming every service is permanently available.

The API therefore implements several resilience mechanisms.

## Retry with Exponential Backoff

Transient downstream failures are retried with increasing delays.

Conceptually:

```text
attempt 1
   |
 failure
   |
 50 ms
   |
attempt 2
   |
 failure
   |
100 ms
   |
attempt 3
```

Retries are bounded to prevent infinite retry loops.

Request cancellation is propagated through Go contexts.

---

## Circuit Breaker

Repeated dependency failures cause the circuit breaker to open.

The implemented flow is:

```text
             request
                |
                v
        +---------------+
        | Circuit closed|
        +-------+-------+
                |
             gRPC call
                |
          +-----+-----+
          |           |
       success      failure
          |           |
          |       failure count
          |           |
          |       threshold?
          |           |
          +-----------+
                |
                v
         Circuit opens
                |
      reject new requests
                |
          reset timeout
                |
                v
          allow recovery
```

The configured breaker opens after repeated failures and temporarily prevents additional calls to an unhealthy dependency.

This avoids continuously hammering a service that is already unavailable.

During validation, stopping the monitoring service produced the expected progression:

```text
Request 1 -> downstream gRPC failure
Request 2 -> downstream gRPC failure
Request 3 -> downstream gRPC failure
Request 4 -> circuit breaker open
Request 5 -> circuit breaker open
```

After the reset period and service recovery, requests could succeed again.

Circuit-open responses are surfaced separately from ordinary downstream gateway failures.

---

# Infrastructure Monitoring Engine

The monitoring engine performs concurrent TCP reachability checks.

A target contains:

```go
type Target struct {
    ID      string
    Address string
}
```

A health-check result contains information including:

```go
type Result struct {
    ServerID  string
    Up        bool
    LatencyMS int64
    CheckedAt time.Time
    Error     string
}
```

---

## Bounded Concurrency

The monitoring engine deliberately avoids spawning an unbounded number of goroutines.

Instead it uses a worker pool.

```text
                 10,000 targets
                       |
                       v
                +-------------+
                | jobs channel|
                +------+------+
                       |
       +---------------+---------------+
       |               |               |
       v               v               v
    worker 1         worker 2       worker N
       |               |               |
       +---------------+---------------+
                       |
                       v
                results channel
                       |
                       v
                  []Result
```

This provides bounded resource consumption while still allowing substantial concurrency.

The worker count is configurable and falls back to a safe default when an invalid value is supplied.

---

# 10,000-Target Validation

The monitoring implementation includes a load benchmark for validating behavior across a large target set.

Example:

```bash
go run ./bench
```

The benchmark constructs:

```text
10,000 server targets
```

and processes them using the bounded worker pool.

A representative unavailable-target run produced:

```text
Viettel Infrastructure Monitoring Load Benchmark
-----------------------------------------------
targets:       10000
workers:       128
up:            0
down:          10000
elapsed:       141.178583ms
throughput:    70832 checks/sec
```

This benchmark is intended as a local engineering/load validation, not as a production SLA or universal performance claim. Results depend heavily on machine, operating system, network behavior, target behavior, timeout configuration, and workload composition.

The repository also contains a test that exercises the checker across 10,000 targets.

---

# PostgreSQL Persistence

PostgreSQL is the source of truth for infrastructure state and health history.

Core data includes:

```text
servers
health_checks
audit events
```

The health-check schema supports querying server history efficiently, including an index ordered by server and check timestamp.

Conceptually:

```text
servers
--------------------------------
id
name
address
status
updated_at


health_checks
--------------------------------
server_id
up
latency
checked_at
error
```

Health-check records are associated with their server, and cleanup behavior is handled through database relationships.

---

# Redis Caching

Redis provides a low-latency caching layer for server information.

The application composes the persistent PostgreSQL store with Redis rather than replacing persistence with cache state.

```text
              GET server
                  |
                  v
             Redis cache
              /       \
           hit         miss
            |            |
            |       PostgreSQL
            |            |
            |       populate cache
            |            |
            +------------+
                  |
                  v
                API
```

Server cache entries use a bounded TTL.

If Redis becomes unavailable, the application can continue using PostgreSQL instead of treating cache failure as total system failure.

---

# Elasticsearch

Elasticsearch is used for indexing infrastructure and health-check documents.

Indexed data includes:

```text
servers
health-checks
```

For example, a health-check document can contain:

```json
{
  "server_id": "67af77d9-793e-49ef-b926-e81ddcfe42d5",
  "up": true,
  "latency_ms": 1,
  "checked_at": "2026-08-13T12:27:28Z"
}
```

Indexing is treated as best effort where appropriate so a temporary search-system failure does not necessarily prevent the primary infrastructure operation from completing.

---

# Observability

The control plane exposes Prometheus metrics through:

```text
GET /metrics
```

Metrics include HTTP traffic, latency, gRPC activity, circuit-breaker behavior, and security-related operational information.

Examples include:

```text
infra_http_requests_total
infra_http_request_duration_seconds
infra_grpc_calls_total
infra_grpc_call_duration_seconds
infra_circuit_open_total
infra_audit_events_total
```

Example output:

```text
infra_http_requests_total{
  method="GET",
  path="/healthz",
  status="200"
} 1

infra_http_requests_total{
  method="GET",
  path="/v1/servers",
  status="401"
} 1
```

HTTP latency is exposed as a Prometheus histogram:

```text
infra_http_request_duration_seconds_bucket
infra_http_request_duration_seconds_sum
infra_http_request_duration_seconds_count
```

This allows operators to derive metrics such as:

- request throughput
- error rates
- endpoint latency distributions
- authentication failures
- downstream gRPC failures
- circuit-breaker activations

---

# Graceful Degradation

Not every dependency is treated equally.

The control plane distinguishes between critical and optional infrastructure.

For example:

```text
PostgreSQL unavailable
        |
        v
core persistence unavailable
        |
service cannot safely continue


Redis unavailable
        |
        v
cache degraded
        |
fall back to PostgreSQL


Elasticsearch unavailable
        |
        v
search/indexing degraded
        |
primary operation can continue


Monitoring gRPC unavailable
        |
        v
retry
        |
repeated failures
        |
circuit breaker opens
```

This prevents non-critical dependencies from unnecessarily taking down the entire control plane.

---

# Structured Logging

Services use structured logging rather than relying entirely on arbitrary text output.

Operational events include information such as:

```text
timestamp
severity
service
resource
operation
error
```

This improves machine processing and makes logs easier to correlate with metrics and audit events.

---

# Local Development

## Requirements

Install:

```text
Go
Docker
Docker Compose
Protocol Buffers compiler
protoc-gen-go
protoc-gen-go-grpc
```

Verify:

```bash
go version
docker --version
docker compose version
protoc --version
protoc-gen-go --version
protoc-gen-go-grpc --version
```

---

## Start Infrastructure Dependencies

The repository provides Docker Compose services for:

```text
PostgreSQL
Redis
Elasticsearch
```

Start them with:

```bash
docker compose up -d
```

Check:

```bash
docker compose ps
```

---

## Run Monitoring Service

```bash
go run ./cmd/monitoring-grpc
```

The monitoring gRPC service listens on its configured address.

Local development uses:

```text
:50051
```

---

## Run Reporting Service

In another terminal:

```bash
go run ./cmd/reporting-grpc
```

Local development uses:

```text
:50052
```

---

## Run API

In another terminal:

```bash
go run ./cmd/api
```

Default HTTP address:

```text
:8080
```

Verify:

```bash
curl http://localhost:8080/healthz
```

Expected:

```json
{
  "status": "ok"
}
```

---

# Example Workflow

## 1. Obtain an Operator Token

```bash
export OPERATOR_TOKEN=$(
  curl -s -X POST http://localhost:8080/auth/token \
    -H "Content-Type: application/json" \
    -d '{"subject":"operator-user","role":"operator"}' \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["token"])'
)
```

---

## 2. Register a Server

```bash
curl -X POST http://localhost:8080/v1/servers \
  -H "Authorization: Bearer $OPERATOR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "local-postgres",
    "address": "localhost:5432"
  }'
```

Example response:

```json
{
  "id": "67af77d9-793e-49ef-b926-e81ddcfe42d5",
  "name": "local-postgres",
  "address": "localhost:5432",
  "status": "unknown"
}
```

---

## 3. Trigger a Health Check

```bash
export SERVER_ID="67af77d9-793e-49ef-b926-e81ddcfe42d5"

curl -X POST \
  http://localhost:8080/v1/servers/$SERVER_ID/check \
  -H "Authorization: Bearer $OPERATOR_TOKEN"
```

Example:

```json
{
  "server_id": "67af77d9-793e-49ef-b926-e81ddcfe42d5",
  "up": true,
  "latency_ms": 1
}
```

---

## 4. Request an Uptime Report

```bash
curl \
  http://localhost:8080/v1/servers/$SERVER_ID/report \
  -H "Authorization: Bearer $OPERATOR_TOKEN"
```

Example:

```json
{
  "server_id": "67af77d9-793e-49ef-b926-e81ddcfe42d5",
  "checks": 2,
  "successful": 2,
  "uptime_percent": 100
}
```

---

## 5. Inspect Metrics

```bash
curl -s http://localhost:8080/metrics | grep '^infra_'
```

Example:

```text
infra_http_requests_total{method="GET",path="/healthz",status="200"} 1
infra_http_requests_total{method="GET",path="/v1/servers",status="401"} 1
```

---

# Testing

Run the extension's packages:

```bash
go test ./gen/... ./cmd/... ./internal/...
```

Run monitoring tests directly:

```bash
go test ./internal/monitoring -v
```

Example test coverage includes:

```text
TestCheckerHealthyTarget
TestCheckerUnavailableTarget
TestCheckAllTenThousandTargets
```

Security and reliability packages include tests for authentication, rate limiting, and circuit-breaker/retry behavior.

---

## Race Detector

Concurrency-sensitive components can be validated with Go's race detector:

```bash
go test -race \
  ./internal/auth \
  ./internal/audit \
  ./internal/ratelimit \
  ./internal/resilience \
  ./internal/monitoring
```

---

## Static Analysis

Run:

```bash
go vet ./gen/... ./cmd/... ./internal/...
```

The extension packages are kept separate from unrelated legacy upstream-provider vet warnings.

---

# Failure Testing

The circuit breaker can be tested locally by deliberately stopping the monitoring service.

Stop:

```bash
lsof -ti :50051 | xargs kill -9
```

Repeatedly request:

```bash
for i in {1..5}; do
  curl -X POST \
    http://localhost:8080/v1/servers/$SERVER_ID/check \
    -H "Authorization: Bearer $OPERATOR_TOKEN"

  echo
done
```

The expected progression is:

```text
downstream unavailable
downstream unavailable
downstream unavailable
circuit breaker open
circuit breaker open
```

Restart the monitoring service and allow the breaker reset period to elapse before retrying.

---

# Project Structure

```text
.
├── api/
│   └── openapi.yaml
│
├── bench/
│   └── ten_k.go
│
├── cmd/
│   ├── api/
│   │   └── main.go
│   ├── monitoring-grpc/
│   │   └── main.go
│   └── reporting-grpc/
│       └── main.go
│
├── gen/
│   ├── monitoring/v1/
│   └── reporting/v1/
│
├── internal/
│   ├── audit/
│   ├── auth/
│   ├── cache/
│   ├── config/
│   ├── database/
│   ├── grpcmonitoring/
│   ├── grpcreporting/
│   ├── logging/
│   ├── metrics/
│   ├── monitoring/
│   ├── ratelimit/
│   ├── reporting/
│   ├── resilience/
│   ├── search/
│   ├── server/
│   └── tlsconfig/
│
├── proto/
│   ├── monitoring.proto
│   └── reporting.proto
│
├── Jenkinsfile
├── docker-compose.yml
├── go.mod
└── go.sum
```

---

# Design Decisions

## Why Go?

Infrastructure monitoring is primarily I/O-bound.

Go provides lightweight concurrency primitives that make bounded concurrent network checks straightforward while keeping the implementation relatively small.

Goroutines and channels are used together with an explicit worker limit instead of creating unrestricted concurrency.

---

## Why gRPC?

Monitoring and reporting are internal service operations with structured request/response contracts.

gRPC provides:

- strongly typed interfaces
- generated clients and servers
- Protocol Buffer schemas
- streaming support
- efficient service-to-service communication

The batch monitoring API also demonstrates bidirectional streaming.

---

## Why PostgreSQL + Redis?

They serve different purposes.

```text
PostgreSQL
    |
    +-- durable source of truth
    +-- server records
    +-- health history
    +-- audit information


Redis
    |
    +-- low-latency cache
    +-- reduced repeated database reads
    +-- bounded TTL
```

Redis failure therefore does not imply loss of durable state.

---

## Why Elasticsearch?

Infrastructure events are useful for more than transactional queries.

Elasticsearch provides a separate indexing layer for infrastructure and health-check documents without making the search engine the authoritative datastore.

---

## Why a Worker Pool?

Creating one goroutine per target is easy, but the architecture should explicitly bound concurrency.

A worker pool provides control over:

- concurrent network connections
- scheduler pressure
- file-descriptor usage
- downstream load
- resource predictability

This becomes increasingly important as the number of monitored targets grows.

---

## Why Retry + Circuit Breaking?

Retries and circuit breakers solve different problems.

Retries handle:

```text
temporary network interruption
short service disruption
transient transport failure
```

Circuit breakers handle:

```text
persistent dependency failure
```

Using only retries against a permanently unhealthy dependency can amplify load.

The circuit breaker prevents that behavior by temporarily rejecting calls once the configured failure threshold has been reached.

---

## Why mTLS?

JWT protects client-to-control-plane operations.

It does not automatically establish trust between internal services.

mTLS provides a separate authentication boundary for service-to-service communication:

```text
Client
   |
 JWT
   |
   v
API
   |
 mTLS
   |
   v
Internal gRPC Service
```

This separates user identity from workload identity.

---

# Security Model

The project applies multiple layers rather than relying on a single authentication mechanism.

```text
                         External request
                               |
                               v
                       Rate limiting
                               |
                               v
                       JWT validation
                               |
                               v
                      RBAC authorization
                               |
                               v
                        Audit logging
                               |
                               v
                      Application logic
                               |
                               v
                         Retry policy
                               |
                               v
                       Circuit breaker
                               |
                               v
                      mTLS-secured gRPC
                               |
                               v
                       Internal service
```

These layers address different failure and threat classes:

| Layer | Purpose |
|---|---|
| JWT | Authenticate API clients |
| RBAC | Restrict privileged operations |
| Rate limiting | Limit abusive request volume |
| Audit trail | Provide operation accountability |
| mTLS | Authenticate internal services |
| Retry | Handle transient dependency failures |
| Circuit breaker | Isolate persistent dependency failures |
| Prometheus | Surface operational behavior |
| Structured logging | Support debugging and investigation |

---

# Validation Summary

The project has been exercised across several levels.

### Functional

Verified:

```text
server creation
server retrieval
server health checking
uptime reporting
Redis caching
PostgreSQL persistence
Elasticsearch indexing
JWT authentication
role-based authorization
audit recording
gRPC communication
```

### Security

Verified:

```text
invalid-token rejection
role-aware API access
audit visibility
mTLS service communication
private key exclusion from Git
rate limiting
```

### Reliability

Verified:

```text
retry behavior
exponential backoff
circuit opening
circuit recovery
dependency failure handling
Redis degradation
bounded monitoring concurrency
```

### Observability

Verified:

```text
Prometheus endpoint
HTTP request counters
HTTP status labels
latency histograms
gRPC metrics
circuit-breaker metrics
```

### Scale

Validated monitoring behavior across:

```text
10,000 simulated server targets
```

with bounded concurrency rather than unrestricted goroutine creation.

---

# Future Work

Possible extensions include:

- Kubernetes deployment manifests
- Prometheus alert rules
- Grafana dashboards
- distributed tracing with OpenTelemetry
- certificate rotation
- secrets-manager integration
- policy-based authorization
- multi-region monitoring workers
- persistent monitoring schedules
- service discovery
- configurable SLO definitions
- failure-budget tracking
- distributed rate limiting
- additional integration and chaos testing

These are intentionally left outside the current implementation to keep the project focused on the core control-plane architecture.

---

# Upstream

This project is based on the open-source VMware Terraform Provider for VMware Cloud Director.

The infrastructure monitoring control plane, security hardening, reliability mechanisms, observability components, and associated extensions in this fork were developed as an independent engineering project.

The repository should not be interpreted as containing or reproducing proprietary Viettel infrastructure.

---

# License

This repository retains the licensing terms of the upstream project. See `LICENSE` for details.
