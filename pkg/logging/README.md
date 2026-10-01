# pkg/logging

Structured logging package for Ivaldi. Provides a backend-agnostic `Logger` interface with a zap implementation, file rotation via lumberjack, and a testify-based mock.

## Usage

### Console logger

```go
import "github.com/ivaldibuild/ivaldi/pkg/logging"

logger, err := logging.NewLogger(
    logging.NewLoggerOptions().
        SetLogLevel("info"),
)
if err != nil {
    log.Fatal(err)
}
defer logger.Sync()

logger.Info("server started", logging.F("port", 8080))
```

### JSON file logger

```go
logger, err := logging.NewLogger(
    logging.NewLoggerOptions().
        SetLogLevel("debug").
        SetLogOutput("file").
        SetLogFormat("json").
        SetLogFilePrefix("ivaldi").
        SetLogDirectory("/var/log/ivaldi").
        SetMaxFileSize(100). // MB
        SetMaxAge(14),       // days
)
```

### Structured fields

Use `logging.F(key, value)` to attach structured fields to log entries:

```go
logger.Info("request completed",
    logging.F("method", "GET"),
    logging.F("path", "/api/deploy"),
    logging.F("status", 200),
    logging.F("duration_ms", 42),
)
```

### Testing

```go
mock := logging.NewMockLogger()
mock.On("Info", "deploying", logging.F("app", "api")).Return()
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
| `LogFilePrefix` | `ivaldi` | Filename prefix for log files |
| `LogDirectory` | `logs` | Directory for log files |
| `MaxFileSize` | `50` | Max size per log file in MB |
| `MaxAge` | `7` | Max age for rotated files in days |
