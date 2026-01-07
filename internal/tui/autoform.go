package tui

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/charmbracelet/huh"
)

// AutoForm generates a huh.Form from a struct pointer using reflection.
// It parses the `tui:"..."` tag to configure field properties.
func AutoForm(v any) *huh.Form {
	val := reflect.ValueOf(v)
	if val.Kind() != reflect.Ptr || val.Elem().Kind() != reflect.Struct {
		panic("AutoForm requires a pointer to a struct")
	}

	el := val.Elem()
	t := el.Type()
	var fields []huh.Field

	for i := 0; i < el.NumField(); i++ {
		field := el.Field(i)
		fieldType := t.Field(i)
		tag := fieldType.Tag.Get("tui")

		// Skip fields without the 'tui' tag
		if tag == "" {
			continue
		}

		// Parse tag key-values
		props := parseTag(tag)

		title := props["title"]
		if title == "" {
			title = fieldType.Name
		}

		desc := props["desc"]

		// Determine input type based on Go type + Tags
		switch field.Kind() {

		case reflect.String:
			// If "options" are present, make it a Select
			if optsStr, ok := props["options"]; ok {
				opts := strings.Split(optsStr, ",")
				var selectOpts []huh.Option[string]
				for _, o := range opts {
					// Format: "Label:Value" or just "Value"
					parts := strings.Split(o, ":")
					key := ""
					val := ""
					if len(parts) == 2 {
						key = strings.TrimSpace(parts[0])
						val = strings.TrimSpace(parts[1])
					} else {
						key = strings.TrimSpace(o)
						val = strings.TrimSpace(o)
					}
					selectOpts = append(selectOpts, huh.NewOption(key, val))
				}

				// Create the Select field
				sel := huh.NewSelect[string]().
					Title(title).
					Description(desc).
					Options(selectOpts...).
					Value(field.Addr().Interface().(*string))

				fields = append(fields, sel)

			} else {
				// Standard Text Input
				input := huh.NewInput().
					Title(title).
					Description(desc).
					Value(field.Addr().Interface().(*string))

				if props["type"] == "password" {
					input.EchoMode(huh.EchoModePassword)
				}

				// Add validation if requested
				if vKey, ok := props["validate"]; ok {
					if validator, exists := Validators[vKey]; exists {
						input.Validate(validator)
					}
				}

				fields = append(fields, input)
			}

		case reflect.Bool:
			// Boolean -> Confirm (Y/N)
			confirm := huh.NewConfirm().
				Title(title).
				Description(desc).
				Value(field.Addr().Interface().(*bool))

			fields = append(fields, confirm)

		case reflect.Int, reflect.Int64:
			// Integer input (using generic input with string conversion adapter would be better,
			// but for now simplistic approach or requiring string fields in config struct for strict typing)
			// For this MVP we'll skip direct int support or assume string backing for config forms
			// TODO: Add int adapter
		}
	}

	return huh.NewForm(
		huh.NewGroup(fields...),
	).WithTheme(huh.ThemeBase16())
}

// Helper to parse "key=val,key2=val2"
func parseTag(tag string) map[string]string {
	res := make(map[string]string)
	for _, part := range strings.Split(tag, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			res[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}
	return res
}

// Validator Registry
var Validators = map[string]func(string) error{
	"required": func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("this field is required")
		}
		return nil
	},
	"cidr": func(s string) error {
		// Mock CPU-cheap check for now; real impl can borrow from net package
		if !strings.Contains(s, "/") && s != "" {
			return fmt.Errorf("must be a valid CIDR (e.g. 192.168.1.1/24)")
		}
		return nil
	},
}
