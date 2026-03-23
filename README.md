# gRPC Streaming — Complete Guide

A production-style Go project demonstrating all four gRPC communication
patterns using a wearable health device as the real-world scenario.

---

## Table of Contents

- [Why gRPC Streaming?](#why-grpc-streaming)
- [Architecture](#architecture)
- [The Four Streaming Types](#the-four-streaming-types)
  - [Type 1 — Unary](#type-1--unary)
  - [Type 2 — Server Streaming](#type-2--server-streaming)
  - [Type 3 — Client Streaming](#type-3--client-streaming)
  - [Type 4 — Bidirectional Streaming](#type-4--bidirectional-streaming)
- [Project Structure](#project-structure)
- [Proto File](#proto-file)
- [Code Generation](#code-generation)
- [Prerequisites](#prerequisites)
- [Running the Project](#running-the-project)
- [Testing with curl](#testing-with-curl)

---

## Why gRPC Streaming?

The old REST polling approach:

```
Client                    Server
  │── GET /bpm ──────────►│   open connection
  │◄─ { "bpm": 72 } ──────│   close connection
  │
  │   wait 5 seconds...
  │
  │── GET /bpm ──────────►│   open connection again
  │◄─ { "bpm": 75 } ──────│   close connection again
```

Problems: new TCP connection every call, data missed between polls,
server does work even when nothing changed, 5-second lag is not real-time.

The gRPC streaming approach:

```
Client                    Server
  │── BeatsPerMinute ─────►│   ONE connection, stays open
  │◄─ { bpm: 72 } ─────────│   server pushes at t=1s
  │◄─ { bpm: 74 } ─────────│   server pushes at t=2s
  │◄─ { bpm: 71 } ─────────│   server pushes at t=3s
  │   ...forever...         │
  │── CANCEL ──────────────►│   client disconnects cleanly
```

One connection. Server pushes exactly when data is ready. Zero wasted requests.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        SERVER PROCESS                        │
│                                                             │
│   ┌──────────────────────────────────────────────────────┐  │
│   │              wearableServer (your logic)             │  │
│   │  GetDeviceInfo │ BeatsPerMinute │ UploadHealthData   │  │
│   │                        LiveMonitor                   │  │
│   └────────────────────────┬─────────────────────────────┘  │
│                            │                                 │
│              ┌─────────────▼──────────────┐                 │
│              │      gRPC Server :9879      │                 │
│              │    (binary / HTTP/2)        │                 │
│              └─────────────▲──────────────┘                 │
│                            │ proxies to                      │
│              ┌─────────────┴──────────────┐                 │
│              │    HTTP Gateway :8080       │                 │
│              │    (JSON / REST)            │                 │
│              └────────────────────────────┘                 │
└─────────────────────────────────────────────────────────────┘
         ▲                          ▲
         │ gRPC (protobuf)          │ HTTP/JSON
         │                          │
  ┌──────┴───────┐          ┌───────┴────────┐
  │  cmd/client  │          │ cmd/stream-    │
  │  native gRPC │          │ client         │
  │  all 4 types │          │ HTTP gateway   │
  └──────────────┘          └────────────────┘
```

The **gRPC Gateway** is a reverse proxy that sits in front of your gRPC server.
It reads the `option (google.api.http)` annotations in your proto file and
auto-generates HTTP/JSON handlers — you write zero HTTP code.

```
curl GET /v1/wearable/abc/info
        │
        ▼ gateway translates
grpc: GetDeviceInfo(uuid: "abc")
        │
        ▼ gateway translates back
{ "uuid": "abc", "name": "FitTracker Pro", ... }
```

---

## The Four Streaming Types

```
                      CLIENT sends
                 ┌──────────────────────────────┐
                 │  ONE request  │ MANY requests │
    ┌────────────┼───────────────┼───────────────┤
    │ ONE        │               │               │
 S  │ response   │     Unary     │    Client     │
 E  │            │  GetDevice    │   Streaming   │
 R  │            │    Info()     │  UploadHealth │
 V  ├────────────┼───────────────┤     Data()    │
 E  │ MANY       │    Server     ├───────────────┤
 R  │ responses  │   Streaming   │ Bidirectional │
    │            │  BeatsPerMin  │   Streaming   │
    │            │    ute()      │ LiveMonitor() │
    └────────────┴───────────────┴───────────────┘
```

---

### Type 1 — Unary

**One request → One response**

```
Client                   Server
  │── GetDeviceInfo ─────►│
  │◄─ DeviceInfoResponse ─│
  │        (done)          │
```

**Use when:** fetching data once, authenticating a user, placing an order —
any operation that has a clear start and end.

**Server signature:**
```go
func (w *wearableServer) GetDeviceInfo(
    ctx context.Context,
    req *wearablepb.GetDeviceInfoRequest,
) (*wearablepb.GetDeviceInfoResponse, error)
```

**Client call:**
```go
resp, err := client.GetDeviceInfo(ctx, &wearablepb.GetDeviceInfoRequest{
    Uuid: "device-abc-123",
})
```

---

### Type 2 — Server Streaming

**One request → Stream of responses**

```
Client                        Server
  │── BeatsPerMinute(uuid) ───►│
  │◄─ { value:72, minute:14 } ─│   pushed at t=1s
  │◄─ { value:75, minute:14 } ─│   pushed at t=2s
  │◄─ { value:71, minute:14 } ─│   pushed at t=3s
  │   ...until client cancels  │
```

**Use when:** live sensor data, stock price feeds, log tailing, progress
updates for long-running jobs — server has data to push continuously.

**Server signature:**
```go
func (w *wearableServer) BeatsPerMinute(
    req *wearablepb.BeatsPerMinuteRequest,
    stream wearablepb.WearableService_BeatsPerMinuteServer,
) error
```
The function never returns until the client disconnects. Every `stream.Send()`
call pushes one message to the client immediately.

**Client:**
```go
stream, _ := client.BeatsPerMinute(ctx, req)
for {
    resp, err := stream.Recv() // blocks until next message arrives
    if err == io.EOF { break } // server closed the stream
}
```

**Critical:** Always check `stream.Context().Done()` in the server loop.
Without it, the goroutine leaks forever after the client disconnects.

---

### Type 3 — Client Streaming

**Stream of requests → One response**

```
Client                              Server
  │── HealthDataPoint{bpm:72} ─────►│
  │── HealthDataPoint{bpm:75} ─────►│  accumulates...
  │── HealthDataPoint{bpm:68} ─────►│
  │── HealthDataPoint{bpm:80} ─────►│
  │── (CloseSend) ─────────────────►│
  │◄─ HealthDataSummary ────────────│  one response
  │           (done)                │
```

**Use when:** uploading a file in chunks, bulk inserting records, sending
offline-collected sensor readings — client has a batch to send, server
processes it all and summarises.

**Server signature:**
```go
func (w *wearableServer) UploadHealthData(
    stream wearablepb.WearableService_UploadHealthDataServer,
) error
```
`stream.Recv()` blocks for the next client message. Returns `io.EOF` when
client calls `CloseSend()`. Then call `stream.SendAndClose()` to send the
single response and close the stream atomically.

**Client:**
```go
stream, _ := client.UploadHealthData(ctx)
for _, bpm := range dataPoints {
    stream.Send(&wearablepb.HealthDataPoint{Bpm: bpm})
}
summary, err := stream.CloseAndRecv() // closes send side, waits for response
```

---

### Type 4 — Bidirectional Streaming

**Stream of requests ↔ Stream of responses**

```
Client                              Server
  │── START ──────────────────────►│
  │◄─ { status:MONITORING, bpm:72 }─│
  │── PAUSE ──────────────────────►│
  │◄─ { status:PAUSED } ───────────│
  │── START ──────────────────────►│
  │◄─ { status:MONITORING, bpm:76 }─│
  │── STOP ───────────────────────►│
  │◄─ { status:STOPPED } ──────────│
  │            (done)              │
```

**Use when:** chat applications, live collaborative editing, interactive
games, real-time control systems — both sides need to send and receive
independently at any time.

**Server signature:**
```go
func (w *wearableServer) LiveMonitor(
    stream wearablepb.WearableService_LiveMonitorServer,
) error
```
Both `stream.Recv()` and `stream.Send()` are available. There is no
fixed pairing — you can send 3 responses for 1 request or none at all.

**Client — two goroutines, one for each direction:**
```go
stream, _ := client.LiveMonitor(ctx)

// goroutine: send commands independently
go func() {
    stream.Send(&wearablepb.LiveMonitorRequest{Command: START})
    time.Sleep(2 * time.Second)
    stream.Send(&wearablepb.LiveMonitorRequest{Command: STOP})
    stream.CloseSend()
}()

// main: receive responses independently
for {
    resp, err := stream.Recv()
    if err == io.EOF { break }
}
```

---

## Project Structure

```
gRPC-streaming/
│
├── api/                          # proto source files (source of truth)
│   └── wearable/
│       └── v1/
│           └── wearable_service.proto
│
├── pkg/                          # generated Go code (do not edit manually)
│   └── api/
│       └── wearable/
│           └── v1/
│               ├── wearable_service.pb.go       # message structs
│               ├── wearable_service_grpc.pb.go  # server/client interfaces
│               └── wearable_service.pb.gw.go    # HTTP gateway handlers
│
├── cmd/
│   ├── server/
│   │   ├── main.go              # starts gRPC :9879 + HTTP gateway :8080
│   │   └── wearable_service.go  # your business logic (4 RPC implementations)
│   │
│   ├── client/
│   │   └── main.go              # native gRPC client, all 4 streaming types
│   │
│   └── stream-client/
│       └── main.go              # HTTP/JSON client via gateway
│
├── vendor.protogen/              # vendored third-party proto dependencies
│   └── google/api/
│       ├── annotations.proto    # enables (google.api.http) option
│       └── http.proto           # imported by annotations.proto
│
├── Makefile
├── go.mod
└── go.sum
```

---

## Proto File

`api/wearable/v1/wearable_service.proto` is the **single source of truth**
for the entire project. Everything — Go structs, server interfaces, HTTP
routes — is generated from it.

```proto
option go_package = "github.com/hypergadam/grpc-streaming/pkg/api/wearable/v1;wearablepb";
```

| Part | Value | Meaning |
|------|-------|---------|
| Import path | `github.com/hypergadam/grpc-streaming/pkg/api/wearable/v1` | Where Go code imports this from |
| Package alias | `wearablepb` | The `package` name inside generated `.go` files |

```proto
import "google/api/annotations.proto";
```
Enables the `option (google.api.http)` annotation that tells the gateway
which HTTP method and URL path maps to which RPC.

---

## Code Generation

```
proto file  ──►  protoc  ──►  3 generated files
                    │
           ┌────────┼──────────────────────┐
           │        │                      │
    protoc-gen-go  protoc-gen-go-grpc  protoc-gen-grpc-gateway
           │        │                      │
    *.pb.go    *_grpc.pb.go         *.pb.gw.go
   (structs)  (interfaces)       (HTTP handlers)
```

### How the output path is determined

```
-I api                        strips "api/" prefix from input path
--go_out=pkg/api              base output directory
--go_opt=paths=source_relative  output = base + (path after -I strips)

api/wearable/v1/foo.proto
  → strip "api/"  → wearable/v1/foo.proto
  → prepend pkg/api/ → pkg/api/wearable/v1/foo.pb.go
```

### Vendoring proto dependencies

`vendor.protogen/` holds `google/api/annotations.proto` and `http.proto`
so the project has zero external path dependencies at generation time.
`-I vendor.protogen` lets protoc find them when resolving imports.

### Make targets

```bash
# vendor googleapis protos into vendor.protogen/
make vendor-proto

# generate all Go code from the proto (runs vendor-proto first)
make generate-wearable-proto
```

---

## Prerequisites

```bash
# Protocol Buffer compiler
sudo apt install -y protobuf-compiler     # Ubuntu/Debian
brew install protobuf                     # macOS

# Go plugins
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@latest

# Verify
protoc --version
protoc-gen-go --version
protoc-gen-go-grpc --version
protoc-gen-grpc-gateway --version
```

---

## Running the Project

**Terminal 1 — start the server**
```bash
go run ./cmd/server/...
# gRPC server listening on localhost:9879
# HTTP gateway listening on localhost:8080
```

**Terminal 2 — native gRPC client (all 4 streaming types)**
```bash
go run ./cmd/client/...
# [UNARY]         Name: FitTracker Pro | Battery: 85% | Firmware: v2.4.1
# [SERVER STREAM] BPM: 72 at minute: 14
# [SERVER STREAM] BPM: 75 at minute: 14
# [CLIENT STREAM] sent BPM: 72
# ...
# [CLIENT STREAM] total=10 avg=74 min=68 max=82
# [BIDI]          status: MONITORING | bpm: 77 | msg: Live monitoring started
# [BIDI]          status: PAUSED     | bpm: 0  | msg: Monitoring paused
# [BIDI]          status: STOPPED    | bpm: 0  | msg: Session ended
```

**Terminal 3 — HTTP client via gateway**
```bash
go run ./cmd/stream-client/...
# [HTTP UNARY]   {"uuid":"device-abc-123","name":"FitTracker Pro",...}
# [HTTP STREAM]  map[result:map[minute:14 value:72]]
# [HTTP STREAM]  map[result:map[minute:14 value:75]]
```

---

## Testing with curl

```bash
# Unary — get device info
curl http://localhost:8080/v1/wearable/device-abc-123/info

# Server streaming — live heart rate (Ctrl+C to stop)
curl http://localhost:8080/v1/wearable/device-abc-123/beats-per-minute
```

> Client streaming and bidirectional streaming are not available via HTTP
> gateway — they require a native gRPC client because HTTP/1.1 cannot
> express a stream of request bodies.
