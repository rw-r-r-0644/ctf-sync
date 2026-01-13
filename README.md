# ctf-sync

[![Go Reference](https://pkg.go.dev/badge/github.com/rw-r-r-0644/ctf-sync.svg)](https://pkg.go.dev/github.com/rw-r-r-0644/ctf-sync)
[![CI](https://github.com/rw-r-r-0644/ctf-sync/actions/workflows/ci.yml/badge.svg)](https://github.com/rw-r-r-0644/ctf-sync/actions/workflows/ci.yml)

go library to interact with ctf frameworks (ctfd, rctf, etc.)

## install

```bash
go get github.com/rw-r-r-0644/ctf-sync
```

## usage

```go
import "github.com/rw-r-r-0644/ctf-sync/jeopardy"

// list backends
for _, b := range jeopardy.Backends() {
    fmt.Println(b.ID, b.Name)
}

// build one
client, _ := jeopardy.Build("ctfd_token", map[string]string{
    "base_url": "https://ctf.example.com",
    "token":    "ctfd_abc123",
})

// fetch challenges
challenges, _ := client.Fetch(ctx)

// submit flag
result, _ := client.Submit(ctx, "42", "FLAG{...}")

// get solves
solves, _ := client.Solves(ctx)

// download files
info, _ := challenges[0].Files[0].DownloadURL(ctx)
// info.URL, info.Headers
```

## backends

| id | settings |
|----|----------|
| `ctfd_token` | `base_url`, `token` |
| `ctfd_cookie` | `base_url`, `cookie` |
| `rctf` | `base_url`, `team_token` |

## script backend

there's also a script backend that executes external commands. since this runs arbitrary commands, it's in a separate package that you must explicitly import:

```go
import (
    "github.com/rw-r-r-0644/ctf-sync/jeopardy"
    _ "github.com/rw-r-r-0644/ctf-sync/jeopardy/script"
)

client, _ := jeopardy.Build("script", map[string]string{
    "command": "python3 my_sync.py",
})
```

the script gets json on stdin, outputs json to stdout. see [examples/script_backend.py](examples/script_backend.py).

### protocol

fetch:
```json
{"action": "fetch"}
```

response:
```json
{
  "challenges": [{
    "id": "1",
    "name": "challenge name",
    "category": "web",
    "description": "...",
    "points": 100,
    "files": [{"name": "file.zip", "url": "https://..."}]
  }]
}
```

submit:
```json
{"action": "submit", "challenge_id": "1", "flag": "FLAG{...}"}
```

response:
```json
{"status": "accepted", "message": "..."}
```

status: `accepted`, `rejected`, `duplicate`, `rate_limited`, `pending`, `error`

solves:
```json
{"action": "solves"}
```

response:
```json
{"solves": [{"challenge_id": "1", "solved_at": "2025-01-01T12:00:00Z"}]}
```

## custom backends

```go
func init() {
    jeopardy.Register(jeopardy.BackendDef{
        ID:   "mybackend",
        Name: "whatever",
        Settings: []jeopardy.SettingDef{
            {ID: "api_key", Name: "API Key", Required: true},
        },
        Build: func(s map[string]string) (jeopardy.Backend, error) {
            return NewMyBackend(s["api_key"])
        },
    })
}
```

## license

mit
