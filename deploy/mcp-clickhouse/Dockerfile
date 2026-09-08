# mcp-clickhouse image for the Ajilamu editor agent read path.
#
# No published image exists, and Docker Hub carries no clickhouse/mcp-clickhouse
# repository. The upstream project ships a Dockerfile that builds from source.
# This file pins the PyPI release instead, which keeps the build small and repeatable.
#
# The server binds loopback and the container publishes no port.
# It reaches ClickHouse as the read-only database user named in CLICKHOUSE_USER.
FROM python:3.13-slim-bookworm

# Non-secret defaults. The transport must be set, because the server defaults
# to stdio. Write access stays off as an accident guard, never as the boundary.
ENV PIP_NO_CACHE_DIR=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    CLICKHOUSE_MCP_SERVER_TRANSPORT=http \
    CLICKHOUSE_MCP_BIND_HOST=127.0.0.1 \
    CLICKHOUSE_MCP_BIND_PORT=8000 \
    CLICKHOUSE_ALLOW_WRITE_ACCESS=false

# Pin the release. Install as root, then drop to the unprivileged user.
RUN pip install --no-cache-dir mcp-clickhouse==0.6.0 \
    && useradd --create-home --uid 10001 mcp_readonly

USER mcp_readonly
WORKDIR /home/mcp_readonly

EXPOSE 8000

HEALTHCHECK --interval=30s --timeout=3s --start-period=15s --retries=3 \
    CMD python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8000/health', timeout=2)"

CMD ["python", "-m", "mcp_clickhouse.main"]
