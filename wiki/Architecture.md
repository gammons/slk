# Architecture

Service-oriented, four layers:

```
UI Layer (bubbletea)   workspace rail · sidebar · messages · thread · compose · status bar
Service Layer          WorkspaceManager · MessageService · ConnectionManager
Client Layer           Slack Web API + browser-protocol WebSocket
Data Layer             SQLite cache · TOML config · token storage
```

- ~9,300 lines of Go across 31 source files and 24 test files
- SQLite is a cache, not the source of truth — Slack remains authoritative
- Render cache + bubbles/viewport for snappy scrolling
- muesli/reflow everywhere for ANSI-correct wrapping and truncation

## Further reading

- Design specs: [`docs/superpowers/specs/`](https://github.com/gammons/slk/tree/main/docs/superpowers/specs/)
- Live implementation status: [`docs/STATUS.md`](https://github.com/gammons/slk/blob/main/docs/STATUS.md)
