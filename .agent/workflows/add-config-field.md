---
description: How to add a new HCL configuration field
---

# Adding a New Configuration Field

Follow these steps to add a new configurable setting to Glacic.

## 1. Add Field to Config Struct

Edit `internal/config/config.go`:

```go
type Config struct {
    // ... existing fields ...
    
    // MyNewSetting enables the new feature
    MyNewSetting bool `hcl:"my_new_setting,optional"`
}
```

### HCL Tags

- `hcl:"field_name"` - Required field
- `hcl:"field_name,optional"` - Optional field
- `hcl:"field_name,block"` - Nested block
- `hcl:"field_name,label"` - Block label

## 2. Add Validation (if needed)

Edit `internal/config/validate.go`:

```go
func (c *Config) Validate() error {
    // ... existing validation ...
    
    if c.MyNewSetting && c.SomeOtherField == "" {
        return fmt.Errorf("my_new_setting requires some_other_field")
    }
    
    return nil
}
```

## 3. Add Default Value (if needed)

In `internal/config/loader.go` or during struct initialization:

```go
func applyDefaults(cfg *Config) {
    if cfg.MyNewSetting {
        // Apply any dependent defaults
    }
}
```

## 4. Use the Setting

### In Control Plane (ctlplane/server.go)

```go
func (s *Server) reloadConfigInternal(cfg *config.Config) error {
    if cfg.MyNewSetting {
        // Enable feature
    }
    return nil
}
```

### In Firewall (firewall/script_builder.go)

```go
func (sb *ScriptBuilder) AddMyFeatureRules(cfg *Config) {
    if cfg.MyNewSetting {
        sb.AddRule("input", "...")
    }
}
```

## 5. Document the Setting

### In Example Config

Edit example config files (`glacic.hcl`, etc.):

```hcl
# Enable the new feature
my_new_setting = true
```

### In ARCHITECTURE.md

Add to Configuration Schema section if it's a major feature.

## 6. Add Test

### Unit Test

In `internal/config/config_test.go`:

```go
func TestMyNewSetting(t *testing.T) {
    hcl := `
        schema_version = "1.0"
        my_new_setting = true
    `
    cfg, err := config.LoadHCL([]byte(hcl), "test.hcl")
    require.NoError(t, err)
    assert.True(t, cfg.MyNewSetting)
}
```

### Integration Test

In `t/70-system/config_test.sh`:

```bash
@test "my_new_setting is parsed correctly" {
    # Add HCL, apply, verify
}
```

## 7. Consider Schema Migration

If changing existing field semantics, add migration in `internal/config/migrations.go`:

```go
func migrateV1ToV2(cfg *Config) error {
    // Handle old format
    return nil
}
```

## Common Field Types

| Go Type | HCL Example |
|---------|-------------|
| `bool` | `enabled = true` |
| `string` | `name = "value"` |
| `int` | `port = 8080` |
| `[]string` | `interfaces = ["eth0", "eth1"]` |
| `map[string]string` | `labels = { key = "value" }` |
| Nested struct | `block { ... }` |
