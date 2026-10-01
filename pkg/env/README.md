# pkg/env

Configuration loader that reads from YAML files and environment variables. Environment variables always take precedence over YAML values.

Only third-party dependency is `gopkg.in/yaml.v3` for YAML parsing. All env parsing uses Go stdlib.

## Usage

```go
import "github.com/ivaldibuild/ivaldi/pkg/env"

var cfg Config
err := env.Load(&cfg, env.Options{YAMLPath: "config.yaml"})
```

`Load` reads the YAML file first, then overlays environment variables on top. Fields without a matching env var keep their YAML value.

You can also use `Parse` (env-only) or `LoadYAML`/`LoadYAMLFile` (YAML-only) independently:

```go
// Env vars only
err := env.Parse(&cfg)

// YAML only
err := env.LoadYAMLFile("config.yaml", &cfg)
```

## Struct Tags

```go
type Config struct {
    // Basic: read from APP_HOST env var
    Host string `yaml:"host" env:"APP_HOST"`

    // Required: error if env var is not set and no default
    Token string `env:"TOKEN,required"`

    // Not empty: error if env var is set but empty
    Secret string `env:"SECRET,notEmpty"`

    // Expand: expand ${VAR} references in the value
    URL string `env:"URL,expand"`

    // Default: fallback when env var is not set
    Port int `env:"PORT" envDefault:"8080"`

    // Pointer: nil when env var is not set
    Debug *bool `env:"DEBUG"`

    // Slice: comma-separated by default
    Hosts []string `env:"HOSTS"`

    // Nested struct: recurses into fields automatically
    Database Database `yaml:"database"`
}
```

## Options

```go
env.Options{
    // File path for YAML config (used by Load)
    YAMLPath: "config.yaml",

    // Custom env var lookup (defaults to os.LookupEnv)
    // Useful for testing without polluting the real environment
    Source: func(key string) (string, bool) { ... },

    // Prefix prepended to all env tag names before lookup
    // e.g. Prefix: "APP_" with env:"PORT" looks up "APP_PORT"
    Prefix: "APP_",

    // Struct tag key to read (default: "env")
    TagName: "env",

    // Struct tag key for defaults (default: "envDefault")
    DefaultTagName: "envDefault",

    // Delimiter for slice values (default: ",")
    SliceSeparator: ",",
}
```

## Supported Types

| Type | Example env value |
|------|-------------------|
| `string` | `hello` |
| `int`, `int8`..`int64` | `42` |
| `uint`, `uint8`..`uint64` | `100` |
| `float32`, `float64` | `3.14` |
| `bool` | `true`, `1`, `t`, `false`, `0`, `f` |
| `time.Duration` | `5m`, `1h30m`, `500ms` |
| `[]T` (slices) | `a,b,c` (comma-separated) |
| `*T` (pointers) | Same as `T`; nil when unset |
| Named types (e.g. `type Role string`) | Same as underlying type |

## Error Handling

Errors are aggregated — all fields are checked before returning, so you see every issue at once:

```
env: 2 error(s):
  env: required environment variable "TOKEN" is not set
  env: environment variable "SECRET" must not be empty
```

Error types for programmatic handling:
- `*env.RequiredError` — required env var not set
- `*env.NotEmptyError` — env var set but empty
- `*env.ParseError` — type conversion failure
- `*env.AggregateError` — wraps multiple errors

## Behavior Notes

- **`envDefault` applies only to zero-valued fields** — layering is default < YAML < env, so a value set from YAML (or by the caller) survives an absent env var, and a set env var overrides both
- **Unexported fields** are skipped
- **`interface{}`/`any` fields** are skipped (handle polymorphic config at application layer)
- **Nested structs** without an `env` tag are recursed into automatically
- **Pointer-to-struct fields** (e.g. `*Database`) are allocated when any inner env var is found
- **Empty env var for slices** produces an empty slice, not `[""]`
