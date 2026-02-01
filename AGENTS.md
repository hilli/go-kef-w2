# AGENTS.md - Development Guide for AI Coding Agents

This document provides guidelines for AI coding agents working on the go-kef-w2 project.

## Project Overview

go-kef-w2 is a Go CLI tool, library, and planned apps for controlling KEF's W2 platform speakers over the network. The project uses Cobra for CLI commands, Viper for configuration, and provides both a CLI tool (`kefw2`) and a reusable library package (`kefw2`).

## Build, Test, and Lint Commands

### Task Runner (Preferred)

This project uses [Task](https://taskfile.dev) as its build tool. Install with `brew install go-task` or `go install github.com/go-task/task/v3/cmd/task@latest`.

**Available tasks:**
- `task build` or `task b` - Build the kefw2 binary to `bin/kefw2`
- `task run` or `task r` - Run kefw2 directly (pass args: `task r -- status`)
- `task complete` or `task c` - Generate shell completions
- `task all` or `task a` - Build all (build + completions)
- `task clean` or `task x` - Clean build artifacts
- `task release-test` - Test release build with goreleaser

See `Taskfile.yaml` for full task definitions and dependencies.

### Standard Go Commands

**Build:**
```bash
# Build with version info (recommended)
task build

# Simple build
go build -o bin/kefw2 cmd/kefw2/kefw2.go
```

**Run:**
```bash
# Run directly
go run cmd/kefw2/kefw2.go [args]

# Or with task
task run -- [args]
```

**Tests:**
```bash
# Run all tests
go test ./...

# Run tests in a specific package
go test ./kefw2

# Run a single test
go test ./kefw2 -run TestAirableClient_GetRows

# Run tests with verbose output
go test -v ./kefw2

# Run tests with coverage
go test -cover ./kefw2
```

**Lint:**
```bash
# Format code
go fmt ./...
gofmt -s -w .

# Vet code
go vet ./...

# If golangci-lint is installed
golangci-lint run
```

## Project Structure

```
.
├── cmd/kefw2/          # CLI application entry point
│   ├── kefw2.go       # Main entry point
│   └── cmd/           # Cobra command implementations
├── kefw2/             # Core library package
│   ├── kefw2.go       # Speaker struct and main API
│   ├── http.go        # HTTP client methods
│   ├── events.go      # Event streaming/subscription
│   ├── airable.go     # Airable service integration
│   └── *_test.go      # Unit tests
├── examples/          # Example usage
├── docs/              # Documentation
└── Taskfile.yaml      # Task build definitions
```

## Code Style Guidelines

### Imports

**Order:** Standard library, external packages, then local packages. Group with blank lines.

```go
import (
    "encoding/json"
    "fmt"
    "strings"

    log "github.com/sirupsen/logrus"
    "github.com/spf13/cobra"
    "github.com/spf13/viper"

    "github.com/hilli/go-kef-w2/kefw2"
)
```

**Use aliased imports for logrus:**
```go
log "github.com/sirupsen/logrus"
```

### Formatting

- **Always run `go fmt` before committing**
- Use tabs for indentation (Go standard)
- Keep lines under 120 characters where reasonable
- Use `gofmt -s` for simplification

### Types and Structs

**Struct tags:** Use mapstructure, json, and yaml tags for config structs:
```go
type KEFSpeaker struct {
    IPAddress       string `mapstructure:"ip_address" json:"ip_address" yaml:"ip_address"`
    Name            string `mapstructure:"name" json:"name" yaml:"name"`
    Model           string `mapstructure:"model" json:"model" yaml:"model"`
}
```

**Exported vs unexported:**
- Public API: PascalCase (e.g., `KEFSpeaker`, `NewSpeaker`)
- Internal helpers: camelCase (e.g., `getData`, `getMACAddress`)

### Naming Conventions

**Variables:**
- Short, descriptive names for local variables: `err`, `resp`, `data`
- Descriptive names for package-level variables: `DefaultEventSubscriptions`

**Functions/Methods:**
- Receiver methods: `(s KEFSpeaker) GetVolume()` or `(s *KEFSpeaker) UpdateInfo()`
- Use pointer receivers when modifying the struct
- Use value receivers for read-only operations
- Constructors: `NewSpeaker(ipAddress string)`

**Constants:**
- Use PascalCase for exported: `SourceWiFi`, `SpeakerStatusOn`
- Use camelCase or UPPER_CASE for unexported

### Error Handling

**Always check errors explicitly:**
```go
data, err := s.getData(path)
if err != nil {
    return err
}
```

**Wrap errors with context using `fmt.Errorf` with `%w`:**
```go
err = s.getId()
if err != nil {
    return fmt.Errorf("failed to get speaker IDs: %w", err)
}
```

**Return early on errors:**
```go
if IPAddress == "" {
    return KEFSpeaker{}, fmt.Errorf("KEF Speaker IP is empty")
}
```

**Handle connection errors with user-friendly messages:**
```go
func (s KEFSpeaker) handleConnectionError(err error) error {
    if err == nil {
        return nil
    }
    
    if strings.Contains(err.Error(), "connection refused") {
        return fmt.Errorf("Unable to connect to speaker at %s...", s.IPAddress)
    }
    return fmt.Errorf("Connection error: %v", err)
}
```

### HTTP Requests

**Timeouts:** Always set client timeouts (currently 1 second):
```go
client := &http.Client{}
client.Timeout = 1.0 * time.Second
```

**Defer close:** Always defer body close after error check:
```go
resp, err := client.Do(req)
if err != nil {
    return nil, s.handleConnectionError(err)
}
defer resp.Body.Close()
```

### Cobra Commands

**Command structure:**
```go
var someCmd = &cobra.Command{
    Use:     "command",
    Aliases: []string{"cmd"},
    Short:   "Brief description",
    Long:    `Longer description`,
    Args:    cobra.MaximumNArgs(1),
    Run: func(cmd *cobra.Command, args []string) {
        // Implementation
    },
}
```

**Use custom color printers** for output (defined in `cmd/kefw2/cmd/color_print.go`):
```go
headerPrinter.Print("Volume: ")
contentPrinter.Printf("%d\n", volume)
errorPrinter.Println("Error message")
taskConpletedPrinter.Printf("Success message\n")
```

### Testing

**Test file naming:** `*_test.go` in the same package

**Test function naming:**
```go
func TestFunctionName(t *testing.T) { }
func TestStructName_MethodName(t *testing.T) { }
```

**Use httptest for mocking:**
```go
mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    // Mock response
    json.NewEncoder(w).Encode(response)
}))
defer mockServer.Close()
```

## Additional Notes

- **Go version:** 1.24.0 (see go.mod)
- **License:** MIT (include license header in new files)
- **Configuration:** Uses Viper with YAML config at `~/.config/kefw2/kefw2.yaml`
- **Environment variables:** Prefix with `KEFW2_` (e.g., `KEFW2_SPEAKER`)
- **Main package imports:** `github.com/hilli/go-kef-w2/kefw2` for library usage
