---
title: Immutable Ledger
description: ClickHouse append-only tables, time travel reconstruction, and isolated reader agents.
template: doc
---

import Mermaid from '../../../components/Mermaid.astro'

Ajilamu stores all project state inside ClickHouse. The system forbids in-place updates. Every mutation, take generation, and billing event appends a new row to the database.

---

## The Raw Schema

The schema defined in `sql/schema.sql` establishes five foundational raw tables:

| Table Name | Contents Stored |
| :--- | :--- |
| `takes_raw` | Slot duration, measured duration, signed delta, repair strategy, file path, and audio peak arrays. |
| `commits_raw` | Directed acyclic graph of edits, commit identifiers, parent identifiers, and sequence numbers. |
| `timeline_state_raw` | Segment boundaries, speaker assignments, and translated lines associated with each commit. |
| `actions_raw` | Event descriptions, actor provenance (`agent`, `command_bar`, `manual_ui`), and input commands. |
| `charges_raw` | Granular billing records for every individual API invocation. |

Each raw table pairs with a deduplicated view bearing the identical base name. These views collapse row versions to present current project state.

---

## Time Travel Replay

The ledger enables non-destructive time travel. The `timeline_at_commit` view accepts any historical commit identifier and reconstructs the timeline state as it existed at that instant.

Creators can branch alternative edits without duplicating underlying video assets. If an experimental edit proves unsuccessful, rolling back to an earlier commit requires zero file copying.

---

## Audio Peaks in the Database

The ledger stores waveform peaks directly in `takes_raw` as `Array(UInt8)`.

When the workspace loads, the frontend fetches amplitude peaks through SQL queries. The client draws visual waveforms instantly without downloading large audio files.

---

## Writer and Reader Isolation

Ajilamu enforces strict process separation between database writes and database reads:

<Mermaid code={`
flowchart LR
    subgraph Writers ["Single Durable Writer"]
        G[Go Backend\ninternal/ledger] -->|Direct Inserts| CH[(ClickHouse)]
    end

    subgraph Readers ["Read-Only Agents"]
        CH -->|Read-Only Port 8123| MCP[mcp-clickhouse\nDocker Container]
        MCP -->|Tool Calls| ADK[ADK Agent\ninternal/agent]
    end

    classDef dark fill:#090d15,stroke:#334155,color:#f1f5f9
    classDef highlight fill:#1e293b,stroke:#f59e0b,color:#f8fafc
    classDef cyan fill:#082f49,stroke:#06b6d4,color:#38bdf8
    class G,CH dark
    class MCP highlight
    class ADK cyan
`} />

The Go backend holds sole authority to insert rows. Writes travel through a durable channel queue in `internal/ledger`.

The ADK editor agent reads timeline context through a dedicated `mcp-clickhouse` container. The agent possesses no insertion credentials. If the MCP server encounters an outage, the agent loses read tooling while core video processing continues uninterrupted.
