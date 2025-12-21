# Glacic Go Code Quality Standards

These guidelines prevent common anti-patterns that lead to unmaintainable code.

## Function Size
- **No God Functions**: Functions over 100 lines are a code smell
- Break into focused helpers with single responsibilities
- Entry points (like `RunAPI`) should orchestrate, not implement

## Constants and Configuration
- **No Magic Numbers**: Define named constants for file descriptors, retry counts, ports, timeouts
- **Configuration at Entry Points**: Parse environment variables and CLI args once at startup into a config struct
- Do NOT scatter `os.Getenv` calls throughout business logic

## IP Address Handling
- **No String IP Parsing**: Always use `net.ParseCIDR`, `net.ParseIP`, or `netip`
- Never slice strings to extract IPs from CIDRs

## Error Handling
- **Errors Over Exits**: Return `error` types instead of calling `os.Exit` deep in call stacks
- Let `main()` decide exit behavior
- Create custom error types for user-friendly messages

## Process Lifecycle
- **Parent/Child Separation**: When re-execing for namespaces, separate logic into distinct functions (`runParentProcess`, `runServerProcess`)
- Don't mix parent and child logic with massive if-statements
