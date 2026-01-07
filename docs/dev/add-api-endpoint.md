---
description: How to add a new REST API endpoint
---

# Adding a New API Endpoint

Follow these steps to add a new REST API endpoint to Glacic.

## 1. Determine if Privileged Operation Needed

- **Privileged** (firewall, network, DHCP, DNS): Needs RPC through ctlplane
- **Unprivileged** (read-only status, auth): Can be handled directly in API

## 2. For Privileged Operations: Add RPC Types

Edit `internal/ctlplane/types.go`:

```go
// MyFeatureArgs is the request for MyFeature
type MyFeatureArgs struct {
    Name   string `json:"name"`
    Config string `json:"config"`
}

// MyFeatureReply is the response for MyFeature
type MyFeatureReply struct {
    Success bool   `json:"success"`
    Error   string `json:"error,omitempty"`
}
```

## 3. Add RPC Server Method

Edit `internal/ctlplane/server.go`:

```go
func (s *Server) MyFeature(args *MyFeatureArgs, reply *MyFeatureReply) error {
    // Implement privileged logic here
    reply.Success = true
    return nil
}
```

## 4. Add RPC Client Method

Edit `internal/ctlplane/client.go`:

```go
func (c *Client) MyFeature(name, config string) error {
    args := &MyFeatureArgs{Name: name, Config: config}
    reply := &MyFeatureReply{}
    err := c.call("Server.MyFeature", args, reply)
    if err != nil {
        return err
    }
    if !reply.Success {
        return fmt.Errorf("%s", reply.Error)
    }
    return nil
}
```

## 5. Add Interface Method

Edit `internal/ctlplane/client_interface.go`:

```go
type ControlPlaneClient interface {
    // ... existing methods ...
    MyFeature(name, config string) error
}
```

## 6. Add Mock Method

Edit `internal/ctlplane/client_mock.go`:

```go
func (m *MockControlPlaneClient) MyFeature(name, config string) error {
    return m.Called(name, config).Error(0)
}
```

## 7. Add API Handler

Edit `internal/api/server.go` or create new handler file:

```go
func (s *Server) handleMyFeature(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Name   string `json:"name"`
        Config string `json:"config"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        WriteError(w, http.StatusBadRequest, "Invalid request body")
        return
    }

    if err := s.client.MyFeature(req.Name, req.Config); err != nil {
        WriteError(w, http.StatusInternalServerError, err.Error())
        return
    }

    WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}
```

## 8. Register Route

In `internal/api/server.go` `initRoutes()`:

```go
r.HandleFunc("POST /api/myfeature", s.requireAuth(s.handleMyFeature))
```

## 9. Add Integration Test

Create `t/10-api/myfeature_test.sh`:

```bash
#!/bin/bash
source "$(dirname "$0")/../common.sh"

@test "MyFeature creates resource" {
    run curl -s -X POST "$API_URL/api/myfeature" \
        -H "Authorization: Bearer $API_KEY" \
        -H "Content-Type: application/json" \
        -d '{"name": "test", "config": "value"}'
    
    assert_success
    assert_output --partial '"success":true'
}
```

## 10. Build and Test

```bash
# Build
make build-go

# Run specific test
make test-int FILTER=myfeature
```
