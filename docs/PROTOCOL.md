# Protocol and architecture

## Data path

```text
LizzieYzy Next or a compatible client
        |  WSS + private path token
        v
Cloudflare Quick Tunnel
        |  HTTP/WebSocket on localhost only
        v
katago-remote bridge
        |  line-delimited JSON over stdin/stdout
        v
KataGo Analysis Engine
```

The bridge transports KataGo's Analysis Engine JSON protocol, not raw GTP. A normal analysis request has no `action`; control requests use the upstream `terminate`, `terminate_all`, `query_version`, `query_models`, or `clear_cache` actions.

## Session isolation

- External query IDs are replaced with unique internal IDs before they reach KataGo.
- Results are mapped back to the originating ID before they reach the client.
- Disconnecting a client sends termination requests for its active analyses.
- Late results from a closed session are dropped.
- Only one WebSocket client can own the engine at a time.

This prevents a reconnecting client from receiving results that belong to the previous connection.

## Safety boundaries

- The HTTP listener must resolve to `127.0.0.1` or `localhost`.
- The WebSocket endpoint requires a random token in its path.
- Token and runtime state files are created with user-only permissions.
- Messages are size-limited, and `maxVisits` is capped by server configuration.
- The bridge launches KataGo directly without a shell and accepts no remote command-line input.
- The full private URL is printed only on explicit `start`, `show`, or `reset-link` commands and is omitted from service logs.

Cloudflare terminates public TLS for the temporary tunnel. Quick Tunnels are suitable for personal and temporary sessions, not an SLA-backed multi-user service. Stop the service when it is not needed.

## Compatibility

Compatibility requires a client that sends one KataGo Analysis Engine JSON object per WebSocket message or line. KaTrain-compatible WebSocket clients follow this transport convention. A generic GTP WebSocket client is not compatible without a separate GTP adapter.

Upstream reference: [KataGo Analysis Engine](https://github.com/lightvector/KataGo/blob/master/docs/Analysis_Engine.md).
