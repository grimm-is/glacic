package spec

import (
	"reflect"
	"strings"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/ctlplane"
)

// OpenAPI Root Object
type OpenAPI struct {
	OpenAPI    string              `json:"openapi"`
	Info       Info                `json:"info"`
	Paths      map[string]PathItem `json:"paths"`
	Components Components          `json:"components"`
}

type Info struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Version     string `json:"version"`
}

type PathItem struct {
	Get    *Operation `json:"get,omitempty"`
	Post   *Operation `json:"post,omitempty"`
	Put    *Operation `json:"put,omitempty"`
	Delete *Operation `json:"delete,omitempty"`
}

type Operation struct {
	Summary     string              `json:"summary"`
	Description string              `json:"description,omitempty"`
	Tags        []string            `json:"tags,omitempty"`
	RequestBody *RequestBody        `json:"requestBody,omitempty"`
	Responses   map[string]Response `json:"responses"`
}

type RequestBody struct {
	Content map[string]MediaType `json:"content"`
}

type Response struct {
	Description string               `json:"description"`
	Content     map[string]MediaType `json:"content,omitempty"`
}

type MediaType struct {
	Schema *Schema `json:"schema,omitempty"`
}

type Components struct {
	Schemas map[string]Schema `json:"schemas"`
}

type Schema struct {
	Type        string            `json:"type,omitempty"`
	Properties  map[string]Schema `json:"properties,omitempty"`
	Items       *Schema           `json:"items,omitempty"`
	Ref         string            `json:"$ref,omitempty"`
	Description string            `json:"description,omitempty"`
	Format      string            `json:"format,omitempty"`
}

// GenerateSpec builds the OpenAPI specification
func GenerateSpec() (*OpenAPI, error) {
	spec := &OpenAPI{
		OpenAPI: "3.0.0",
		Info: Info{
			Title:       "Glacic API",
			Description: "Glacic Network Appliance Control Plane API",
			Version:     "1.0.0",
		},
		Paths: make(map[string]PathItem),
		Components: Components{
			Schemas: make(map[string]Schema),
		},
	}

	// 1. Generate Schemas from Config Structs
	registerSchema(spec, "Config", config.Config{})
	registerSchema(spec, "Interface", config.Interface{})
	registerSchema(spec, "Zone", config.Zone{})
	registerSchema(spec, "Policy", config.Policy{})
	registerSchema(spec, "NATRule", config.NATRule{})
	registerSchema(spec, "DHCPServer", config.DHCPServer{})
	// config.ProtocolConfig might not exist or be named differently, skip for now
	registerSchema(spec, "Status", ctlplane.Status{})

	// 2. Define API Paths (Manual Mapping for now)
	// Status
	addPath(spec, "/api/status", "GET", "Get System Status", "Status", "System")

	// Config
	addPath(spec, "/api/config", "GET", "Get Full Config", "Config", "Configuration")

	// Interfaces
	addPath(spec, "/api/interfaces", "GET", "Get Interfaces", "Interface", "Network", true)

	// Policies
	addPath(spec, "/api/config/policies", "GET", "Get Firewall Policies", "Policy", "Firewall", true)

	return spec, nil
}

func registerSchema(spec *OpenAPI, name string, t interface{}) {
	schema := reflectSchema(reflect.TypeOf(t))
	spec.Components.Schemas[name] = schema
}

func reflectSchema(t reflect.Type) Schema {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.Struct:
		props := make(map[string]Schema)
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			jsonTag := field.Tag.Get("json")
			if jsonTag == "-" || jsonTag == "" {
				continue
			}
			name := strings.Split(jsonTag, ",")[0]

			// Detect nested structs (naive)
			if field.Type.Kind() == reflect.Slice {
				elem := field.Type.Elem()
				if elem.Kind() == reflect.Struct {
					// Use specific type if possible, else simplified array
					props[name] = Schema{Type: "array", Items: &Schema{Type: "object", Description: elem.Name()}}
					continue
				}
			}

			props[name] = reflectSchema(field.Type)
		}
		return Schema{Type: "object", Properties: props}
	case reflect.Slice:
		return Schema{Type: "array", Items: &Schema{Type: "string"}} // Simplify for now
	case reflect.Bool:
		return Schema{Type: "boolean"}
	case reflect.Int, reflect.Int64:
		return Schema{Type: "integer"}
	case reflect.Float64:
		return Schema{Type: "number"}
	default:
		return Schema{Type: "string"}
	}
}

func addPath(spec *OpenAPI, path, method, summary, responseRef, tag string, isArray ...bool) {
	op := &Operation{
		Summary: summary,
		Tags:    []string{tag},
		Responses: map[string]Response{
			"200": {
				Description: "Successful operation",
				Content: map[string]MediaType{
					"application/json": {
						Schema: &Schema{},
					},
				},
			},
		},
	}

	schema := Schema{Ref: "#/components/schemas/" + responseRef}
	if len(isArray) > 0 && isArray[0] {
		op.Responses["200"].Content["application/json"].Schema.Type = "array"
		op.Responses["200"].Content["application/json"].Schema.Items = &schema
	} else {
		op.Responses["200"].Content["application/json"].Schema.Ref = schema.Ref
	}

	item := spec.Paths[path]
	switch method {
	case "GET":
		item.Get = op
	case "POST":
		item.Post = op
	}
	spec.Paths[path] = item
}
