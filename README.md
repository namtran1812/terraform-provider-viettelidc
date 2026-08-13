# Viettel Infrastructure Monitoring & Security Control Plane

A production-oriented infrastructure monitoring and security extension for the VMware Cloud Director Terraform provider.

The project extends the existing provider with infrastructure discovery, health monitoring, reporting, authentication and authorization, audit logging, service resilience, observability, and provider-to-monitoring inventory synchronization.

Rather than operating as an isolated monitoring application, the control plane integrates with the provider's existing authenticated VMware Cloud Director client and VM discovery path, allowing provider-managed infrastructure to become part of the monitoring inventory.

---

## Overview

Infrastructure providers manage the lifecycle of resources, but operating those resources also requires visibility into their health, availability, access patterns, and service dependencies.

This project adds that operational layer to the existing Terraform provider.

The system provides:

- automatic discovery of deployed VMware Cloud Director VMs
- provider-to-monitoring inventory synchronization
- concurrent infrastructure health checks
- uptime and availability reporting
- REST and gRPC service interfaces
- PostgreSQL-backed persistent state
- Redis caching
- Elasticsearch indexing
- JWT authentication and role-based authorization
- security audit logging
- rate limiting
- circuit breakers for downstream service failures
- TLS/mTLS support
- Prometheus metrics
- structured logging
- graceful service shutdown
- load validation across 10,000 monitoring targets

The result is a small infrastructure control plane built around resources already managed through the provider.

---

## Architecture

```text
                    Existing Terraform Provider
                              |
                              v
                       vcd.Config.Client()
                              |
                              v
                    Authenticated VCDClient
                              |
                              v
                GetOrgAndVdc / QueryVmList
                              |
                              v
                   Deployed VCD VM Records
                              |
                              v
                    VCDInventorySource
                              |
                              v
                  providerinventory.Sync()
                              |
                 +------------+------------+
                 |                         |
                 v                         v
             PostgreSQL                  Redis
          persistent state                cache
                 |
                 v
        Infrastructure Control Plane
                 |
        +--------+---------+
        |                  |
        v                  v
   Monitoring gRPC    Reporting gRPC
        |                  |
        +--------+---------+
                 |
                 v
       Health / Availability Data
                 |
        +--------+---------+
        |                  |
        v                  v
 Elasticsearch        Prometheus
    indexing             metrics
```

The HTTP control plane sits in front of these services and applies authentication, authorization, rate limiting, audit logging, resilience controls, and observability.

---

## Provider Integration

A central goal of the project is to integrate monitoring with the existing provider rather than maintain a separate manually configured infrastructure inventory.

The integration reuses the provider's existing VMware Cloud Director connection path:

```text
Provider configuration
        ↓
vcd.Config.Client()
        ↓
authenticated VCDClient
        ↓
GetOrgAndVdc()
        ↓
QueryVmList(types.VmQueryFilterOnlyDeployed)
        ↓
VCDInventorySource
        ↓
providerinventory.Sync()
        ↓
Monitoring inventory
```

### VCD inventory adapter

`vcd/monitoring_inventory.go` implements the provider inventory adapter.

It queries deployed VMs through the same VCD client used by the existing Terraform provider and converts VM records into a provider-independent representation:

```go
type Resource struct {
    ID      string
    Name    string
    Address string
    Type    string
}
```

This keeps provider-specific discovery separate from monitoring logic.

### Inventory synchronization

`internal/providerinventory` owns synchronization between provider-discovered resources and the monitoring inventory.

For every discovered resource:

- new resources are registered with an initial `unknown` health state
- existing resources have provider-controlled metadata updated
- existing monitoring health state is preserved
- malformed resources are rejected and counted as synchronization failures

The synchronization operation returns:

```go
type SyncResult struct {
    Discovered int
    Created    int
    Updated    int
    Failed     int
}
```

This makes provider synchronization measurable and suitable for automation.

### Provider sync command

`cmd/provider-sync` provides an executable synchronization path.

It:

1. loads VCD credentials and configuration
2. authenticates using the existing `vcd.Config.Client()` implementation
3. discovers deployed VMs
4. converts them into monitoring resources
5. synchronizes them into the PostgreSQL/Redis-backed inventory

This allows monitoring inventory to follow infrastructure managed by the provider instead of requiring every server to be registered manually.

---

## Monitoring Engine

The monitoring engine is implemented in:

```text
internal/monitoring/
```

It performs TCP-based health checks against infrastructure targets and records:

- server ID
- availability state
- check latency
- timestamp
- connection errors

Example result:

```json
{
  "server_id": "67af77d9-793e-49ef-b926-e81ddcfe42d5",
  "up": true,
  "latency_ms": 1,
  "checked_at": "2026-08-13T13:01:52Z"
}
```

### Bounded concurrency

Health checks use a bounded worker pool rather than spawning an unbounded goroutine for every target.

```text
Targets
   |
   v
 Job Queue
   |
   +-----------------------------+
   |       Worker Pool           |
   |                             |
   | W1  W2  W3 ... W128         |
   +-----------------------------+
                 |
                 v
              Results
```

This provides controlled concurrency while supporting large inventories.

The implementation was load-tested with 10,000 simulated targets.

A local unavailable-target workload produced approximately:

```text
targets:       10000
workers:       128
elapsed:       141 ms
throughput:    ~70,000 checks/sec
```

These numbers represent a local synthetic benchmark and should not be interpreted as production network throughput.

---

## REST API

The Gin-based API exposes the control plane over HTTP.

### Health

```http
GET /healthz
```

Example:

```json
{
  "status": "ok"
}
```

### Authentication

```http
POST /auth/token
```

Example request:

```json
{
  "subject": "operator-user",
  "role": "operator"
}
```

The returned JWT is used as a bearer token for protected endpoints.

### Infrastructure

```http
POST  /v1/servers
GET   /v1/servers
GET   /v1/servers/:id
PATCH /v1/servers/:id
```

### Health checks

```http
POST /v1/servers/:id/check
```

### Availability reports

```http
GET /v1/servers/:id/report
```

### Audit events

```http
GET /v1/audit
```

Administrative access is required for security audit retrieval.

---

## gRPC Services

Monitoring and reporting are separated into gRPC services.

Protocol definitions live under:

```text
proto/
├── monitoring.proto
└── reporting.proto
```

Generated Go bindings live under:

```text
gen/
├── monitoring/v1/
└── reporting/v1/
```

### Monitoring service

```protobuf
service MonitoringService {
  rpc Check(CheckRequest) returns (CheckResponse);
  rpc CheckBatch(stream CheckRequest)
      returns (stream CheckResponse);
}
```

`CheckBatch` provides bidirectional streaming for larger monitoring workloads.

### Reporting service

```protobuf
service ReportingService {
  rpc GetUptime(UptimeRequest)
      returns (UptimeResponse);
}
```

The API gateway communicates with these services rather than embedding all monitoring and reporting behavior directly into HTTP handlers.

---

## Security

Security controls are implemented as first-class infrastructure components.

### JWT authentication

Protected API requests require:

```http
Authorization: Bearer <token>
```

Tokens contain identity and role information and are cryptographically validated before requests reach protected handlers.

### Role-based access control

The system distinguishes between roles such as:

```text
viewer
operator
admin
```

This allows read operations, infrastructure actions, and security administration to have different permissions.

### Audit logging

Security-sensitive operations generate audit records containing information such as:

```text
actor
role
action
resource
resource_id
success
timestamp
```

Example:

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

Audit records make privileged control-plane actions traceable.

### Rate limiting

Request rate limiting protects control-plane endpoints from excessive or abusive traffic.

The limiter is implemented independently under:

```text
internal/ratelimit/
```

### TLS and mTLS

TLS configuration is isolated under:

```text
internal/tlsconfig/
```

The design supports encrypted service communication and mutual authentication between internal services.

Private keys and locally generated certificates are intentionally excluded from version control.

---

## Service Resilience

Infrastructure control planes must continue behaving predictably when downstream services fail.

The project therefore includes circuit-breaker protection around downstream service calls.

For example, if the monitoring gRPC service becomes unavailable, repeated failures transition the circuit into an open state.

Instead of continuously attempting a failing dependency:

```text
Request
   |
   v
Circuit Breaker
   |
   +--- CLOSED ---> Monitoring Service
   |
   +--- OPEN ----> Fail Fast
```

Observed behavior during local failure testing:

```text
Request 1 -> monitoring connection refused
Request 2 -> monitoring connection refused
Request 3 -> monitoring connection refused
Request 4 -> circuit breaker open
Request 5 -> circuit breaker open
```

This prevents repeated downstream failures from unnecessarily consuming resources and increasing request latency.

---

## Persistence and Caching

### PostgreSQL

PostgreSQL stores durable infrastructure state and monitoring metadata.

The database layer lives under:

```text
internal/database/
```

### Redis

Redis provides a caching layer in front of persistent storage.

```text
Request
   |
   v
 Redis
   |
   +--- hit ---> return resource
   |
   +--- miss
         |
         v
     PostgreSQL
```

If Redis becomes unavailable, requests can continue using PostgreSQL.

This avoids making the cache a hard dependency for core control-plane functionality.

---

## Elasticsearch

Infrastructure and health-related records can be indexed into Elasticsearch for search and operational analysis.

The integration is implemented under:

```text
internal/search/
```

Indexing is performed on a best-effort basis so an Elasticsearch outage does not prevent core infrastructure operations from completing.

---

## Observability

The API exports Prometheus-compatible metrics:

```http
GET /metrics
```

Metrics include request counts, latency histograms, and resilience information.

Example:

```text
infra_http_requests_total{
  method="GET",
  path="/healthz",
  status="200"
} 1
```

Request latency is exported as a histogram:

```text
infra_http_request_duration_seconds_bucket
infra_http_request_duration_seconds_sum
infra_http_request_duration_seconds_count
```

Circuit-breaker activity is also observable:

```text
infra_circuit_open_total
```

Together with structured logging, these metrics provide visibility into both infrastructure health and the health of the control plane itself.

---

## Repository Structure

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
│   ├── monitoring-grpc/
│   ├── provider-sync/
│   └── reporting-grpc/
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
│   ├── providerinventory/
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
├── vcd/
│   ├── monitoring_inventory.go
│   ├── monitoring_inventory_test.go
│   └── ...
│
├── docker-compose.yml
├── Jenkinsfile
└── go.mod
```

---

## Running Locally

### Requirements

Install:

- Go
- Docker
- Docker Compose
- Protocol Buffers compiler
- `protoc-gen-go`
- `protoc-gen-go-grpc`

### Start infrastructure dependencies

```bash
docker compose up -d
```

Verify:

```bash
docker compose ps
```

The local development stack includes:

```text
PostgreSQL
Redis
Elasticsearch
```

### Start monitoring service

```bash
go run ./cmd/monitoring-grpc
```

### Start reporting service

In another terminal:

```bash
go run ./cmd/reporting-grpc
```

### Start API

```bash
go run ./cmd/api
```

Verify:

```bash
curl http://localhost:8080/healthz
```

Expected:

```json
{"status":"ok"}
```

---

## Provider Inventory Synchronization

Live provider synchronization requires access to a VMware Cloud Director environment.

Configure the appropriate VCD environment variables, for example:

```bash
export VCD_URL="https://vcd.example.com/api"
export VCD_ORG="example-org"
export VCD_VDC="example-vdc"
export VCD_USER="user"
export VCD_PASSWORD="password"
```

Then run:

```bash
go run ./cmd/provider-sync
```

The command authenticates through the provider's existing client implementation, discovers deployed VMs, and synchronizes them into the monitoring inventory.

Authentication options supported by the underlying provider client can also be used where configured appropriately.

> Live VCD discovery requires a valid VMware Cloud Director endpoint and credentials. The adapter, normalization logic, synchronization behavior, and provider integration path are covered by local tests, but live infrastructure discovery depends on an external VCD environment.

---

## Testing

### Monitoring and internal services

```bash
go test ./gen/... ./cmd/... ./internal/...
```

### Provider inventory synchronization

```bash
go test ./internal/providerinventory -v
```

### VCD inventory adapter

```bash
go test ./vcd \
  -run 'TestMonitoringResourceFromVM' \
  -vet=off \
  -count=1
```

### Race detection

```bash
go test -race \
  ./internal/auth \
  ./internal/audit \
  ./internal/ratelimit \
  ./internal/resilience \
  ./internal/monitoring
```

### Static analysis

```bash
go vet ./gen/... ./cmd/... ./internal/...
```

The upstream provider contains some pre-existing vet diagnostics outside the monitoring extension, so extension-specific validation can also be run independently.

---

## Load Testing

A synthetic 10,000-target workload is available under:

```text
bench/ten_k.go
```

Run:

```bash
go run ./bench
```

The benchmark validates the bounded-concurrency monitoring path under a large local target set.

Benchmark results depend heavily on operating-system networking behavior, socket state, target behavior, timeout configuration, and worker count. They should therefore be treated as local engineering measurements rather than production capacity guarantees.

---

## Design Principles

### Reuse the provider connection

Infrastructure discovery uses the existing provider authentication and VCD client rather than maintaining a separate Cloud Director integration.

### Separate discovery from monitoring

Provider-specific infrastructure is translated into a small provider-independent resource model before entering the monitoring system.

This keeps monitoring logic independent of VMware-specific API types.

### Bound concurrency

Monitoring uses worker pools so target count does not translate directly into unbounded goroutine or connection creation.

### Degrade gracefully

Redis and Elasticsearch are useful operational dependencies, but failures in these systems should not unnecessarily stop core infrastructure operations.

### Fail fast when dependencies fail

Circuit breakers prevent repeated calls to unavailable downstream services.

### Make privileged operations observable

Authentication, authorization, audit events, metrics, and structured logs are part of the control plane rather than afterthoughts.

---

## Current Validation

The project has been locally validated for:

- provider inventory creation and update behavior
- preservation of existing monitoring health state during provider synchronization
- rejection of malformed provider resources
- VCD VM-to-monitoring-resource conversion
- 10,000-target bounded-concurrency monitoring
- JWT authentication
- role-based authorization
- audit event generation
- rate limiting
- circuit-breaker behavior
- Prometheus request metrics
- race detection across concurrency-sensitive packages
- PostgreSQL persistence
- Redis caching
- Elasticsearch indexing
- REST-to-gRPC monitoring and reporting flows

Live VMware Cloud Director discovery remains environment-dependent and requires access to an actual VCD deployment.

---

## Technology

**Language:** Go

**API:** Gin, OpenAPI

**RPC:** gRPC, Protocol Buffers

**Infrastructure:** VMware Cloud Director, Terraform Provider SDK

**Storage:** PostgreSQL

**Caching:** Redis

**Search:** Elasticsearch

**Observability:** Prometheus, structured logging

**Security:** JWT, RBAC, TLS/mTLS, audit logging, rate limiting

**Resilience:** circuit breakers, bounded concurrency, graceful degradation

**Testing:** Go testing framework, race detector, synthetic load testing

---

## Summary

This project extends an existing Terraform provider beyond infrastructure provisioning into infrastructure operations.

Provider-managed VMs can flow from the existing authenticated VMware Cloud Director client into a synchronized monitoring inventory, where they can be health-checked, reported on, audited, secured, and observed through the control plane.

The architecture intentionally separates:

```text
Infrastructure provisioning
        ↓
Provider discovery
        ↓
Inventory synchronization
        ↓
Monitoring
        ↓
Reporting
        ↓
Security + Observability
```

That separation allows the monitoring system to build on the existing provider without coupling its operational logic directly to Terraform resource implementations.
