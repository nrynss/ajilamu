---
title: Deployment & Production
description: Reproducible single-host provisioning, Secret Manager tmpfs mounting, and Caddy TLS.
template: doc
---

import Mermaid from '../../../components/Mermaid.astro'

Ajilamu automates full host deployment from an empty Google Cloud project. The shell scripts inside `deploy/` construct and configure the production machine reproducibly.

---

## Architecture Topology

The production host isolates sensitive application processes behind a Caddy reverse proxy:

<Mermaid code={`
flowchart TD
    Client[Web Browser] -->|HTTPS Port 443| Caddy[Caddy Reverse Proxy\nAutomatic TLS]
    Caddy -->|HTTP Loopback| Go[Go Application\n127.0.0.1:8080]
    Go -->|Local SSD| Disk[(Storage\n/data/storage)]
    Go -->|Database Reads/Writes| CH[(ClickHouse Cloud)]
    Go -->|Subprocess / Docker| MCP[mcp-clickhouse\n127.0.0.1:8123]

    classDef dark fill:#090d15,stroke:#334155,color:#f1f5f9
    classDef highlight fill:#1e293b,stroke:#f59e0b,color:#f8fafc
    classDef cyan fill:#082f49,stroke:#06b6d4,color:#38bdf8
    class Client,Caddy dark
    class Go,Disk highlight
    class CH,MCP cyan
`} />

Only Caddy binds public network interfaces. The Go server and the mcp-clickhouse service bind exclusively to loopback addresses.

---

## Secret Injection via tmpfs

Production hosts avoid storing static `.env` configuration files on persistent disks.

The `systemd` service runs an `ExecStartPre` script to fetch credentials from Google Cloud Secret Manager. The script mounts an in-memory `tmpfs` volume at `/run/ajilamu/env` with `0600` file permissions.

`GOOGLE_APPLICATION_CREDENTIALS` remains unset. The Google Cloud SDK reads service account identity directly from the Compute Engine metadata server.

---

## Deployment Scripts

The `deploy/` directory provides four automated lifecycle scripts:

### 1. Provisioning (`provision.sh`)
The provisioning script creates the Compute Engine VM instance, attaches a 50 GB persistent SSD at `/data/storage`, and provisions Cloud Storage backup buckets.

### 2. Bundling (`build.sh`)
The build script cross-compiles the Go server binary and bundles the compiled Svelte 5 frontend assets into a release tarball.

### 3. Shipping (`deploy.sh`)
The deployment script copies release archives to the remote host, unpacks binaries into `/opt/ajilamu`, and reloads systemd daemon units.

### 4. Verification (`verify.sh`)
The verification script sends HTTP probe requests against public endpoints to confirm TLS termination, video range requests, and database connectivity.

---

## Fast Video Scrubbing

The Go server serves media files using HTTP range requests (`Accept-Ranges: bytes`). When a user drags the timeline scrubber, the browser requests specific byte intervals. Scrubbing remains instantaneous regardless of total video length.
