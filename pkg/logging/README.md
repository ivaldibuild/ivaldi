# surv

Structured logging package for Dispatch. Provides a backend-agnostic `Logger` interface with a zap implementation, file rotation via lumberjack, and a testify-based mock.

## Usage

### Console logger

```go
logger, err := surv.NewLogger(
    surv.NewLoggerOptions().
        SetLogLevel("info"),
)
if err != nil {
    log.Fatal(err)
}
defer logger.Sync()

logger.Info("server started", surv.F("port", 8080))
```

### JSON file logger

```go
logger, err := surv.NewLogger(
    surv.NewLoggerOptions().
        SetLogLevel("debug").
        SetLogOutput("file").
        SetLogFormat("json").
        SetLogFilePrefix("dispatch").
        SetLogDirectory("/var/log/dispatch").
        SetMaxFileSize(100). // MB
        SetMaxAge(14),       // days
)
```

### Structured fields

Use `surv.F(key, value)` to attach structured fields to log entries:

```go
logger.Info("request completed",
    surv.F("method", "GET"),
    surv.F("path", "/api/deploy"),
    surv.F("status", 200),
    surv.F("duration_ms", 42),
)
```

### Testing

```go
mock := surv.NewMockLogger()
mock.On("Info", "deploying", surv.F("app", "api")).Return()
mock.On("Sync").Return(nil)

// pass mock to code under test
svc := NewService(mock)
svc.Deploy("api")

mock.AssertExpectations(t)
```

## Options

| Option | Default | Description |
|--------|---------|-------------|
| `LogLevel` | *(required)* | `debug`, `info`, `warn`, `error`, `fatal` |
| `LogOutput` | `console` | `console` or `file` |
| `LogFormat` | `text` | `text` or `json` |
| `LogFilePrefix` | `dispatch` | Filename prefix for log files |
| `LogDirectory` | `logs` | Directory for log files |
| `MaxFileSize` | `50` | Max size per log file in MB |
| `MaxAge` | `7` | Max age for rotated files in days |
