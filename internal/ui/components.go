package ui

// ComponentType identifies the type of UI component.
type ComponentType string

const (
	ComponentTable      ComponentType = "table"
	ComponentForm       ComponentType = "form"
	ComponentCard       ComponentType = "card"
	ComponentStats      ComponentType = "stats"
	ComponentList       ComponentType = "list"
	ComponentTabs       ComponentType = "tabs"
	ComponentAlert      ComponentType = "alert"
	ComponentChart      ComponentType = "chart"
	ComponentCodeEditor ComponentType = "code-editor"
	ComponentLogViewer  ComponentType = "log-viewer"
)

// Component is the base interface for all UI components.
type Component interface {
	Type() ComponentType
	ID() string
}

// TableColumn defines a column in a table.
type TableColumn struct {
	Key      string `json:"key"`      // Data field key
	Label    string `json:"label"`    // Display label
	Width    int    `json:"width"`    // Width (chars for TUI, pixels for web)
	Sortable bool   `json:"sortable"` // Can sort by this column
	Align    string `json:"align"`    // left, center, right
	Format   string `json:"format"`   // Format hint: "bytes", "duration", "ip", "date", etc.
	Truncate bool   `json:"truncate"` // Truncate long values
	Hidden   bool   `json:"hidden"`   // Hidden by default (can be shown)
}

// TableAction defines an action that can be performed on table rows.
type TableAction struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Icon        string `json:"icon"`
	Shortcut    string `json:"shortcut,omitempty"` // Keyboard shortcut (e.g., "e" for edit)
	Destructive bool   `json:"destructive"`        // Show warning before executing
	Bulk        bool   `json:"bulk"`               // Can apply to multiple rows
}

// Table defines a data table component.
type Table struct {
	ComponentID string        `json:"id"`
	Title       string        `json:"title"`
	Columns     []TableColumn `json:"columns"`
	Actions     []TableAction `json:"actions,omitempty"`
	Selectable  bool          `json:"selectable"` // Allow row selection
	Searchable  bool          `json:"searchable"` // Show search box
	Paginated   bool          `json:"paginated"`  // Enable pagination
	PageSize    int           `json:"pageSize"`   // Default page size
	EmptyText   string        `json:"emptyText"`  // Text when no data
	DataSource  string        `json:"dataSource"` // API endpoint for data
}

func (t Table) Type() ComponentType { return ComponentTable }
func (t Table) ID() string          { return t.ComponentID }

// FieldType defines the type of form field.
type FieldType string

const (
	FieldText        FieldType = "text"
	FieldTextarea    FieldType = "textarea"
	FieldNumber      FieldType = "number"
	FieldSelect      FieldType = "select"
	FieldMultiSelect FieldType = "multi-select"
	FieldCheckbox    FieldType = "checkbox"
	FieldToggle      FieldType = "toggle"
	FieldIP          FieldType = "ip"         // IP address input with validation
	FieldCIDR        FieldType = "cidr"       // CIDR notation input
	FieldMAC         FieldType = "mac"        // MAC address input
	FieldPort        FieldType = "port"       // Port number (1-65535)
	FieldPortRange   FieldType = "port-range" // Port range (e.g., "80-443")
	FieldPassword    FieldType = "password"
	FieldTags        FieldType = "tags"     // Tag/chip input
	FieldCode        FieldType = "code"     // Code editor (for HCL, etc.)
	FieldDuration    FieldType = "duration" // Duration input (e.g., "1h30m")
	FieldBytes       FieldType = "bytes"    // Byte size input (e.g., "100MB")
)

// SelectOption defines an option for select fields.
type SelectOption struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
	Group       string `json:"group,omitempty"` // For grouped options
}

// FieldValidation defines validation rules for a field.
type FieldValidation struct {
	Required  bool   `json:"required,omitempty"`
	MinLength int    `json:"minLength,omitempty"`
	MaxLength int    `json:"maxLength,omitempty"`
	Min       *int   `json:"min,omitempty"`     // For numbers
	Max       *int   `json:"max,omitempty"`     // For numbers
	Pattern   string `json:"pattern,omitempty"` // Regex pattern
	Message   string `json:"message,omitempty"` // Custom error message
}

// FormField defines a single field in a form.
type FormField struct {
	Key          string          `json:"key"`   // Field key in data
	Label        string          `json:"label"` // Display label
	Type         FieldType       `json:"type"`
	Placeholder  string          `json:"placeholder,omitempty"`
	HelpText     string          `json:"helpText,omitempty"`
	DefaultValue interface{}     `json:"defaultValue,omitempty"`
	Options      []SelectOption  `json:"options,omitempty"` // For select fields
	Validation   FieldValidation `json:"validation,omitempty"`
	Disabled     bool            `json:"disabled,omitempty"`
	ReadOnly     bool            `json:"readOnly,omitempty"`
	Condition    string          `json:"condition,omitempty"` // Show only if condition is true (e.g., "mode == 'static'")
	Width        string          `json:"width,omitempty"`     // "full", "half", "third"
}

// FormSection groups related fields together.
type FormSection struct {
	Title       string      `json:"title,omitempty"`
	Description string      `json:"description,omitempty"`
	Collapsible bool        `json:"collapsible,omitempty"`
	Collapsed   bool        `json:"collapsed,omitempty"` // Initially collapsed
	Fields      []FormField `json:"fields"`
}

// Form defines a form component.
type Form struct {
	ComponentID  string        `json:"id"`
	Title        string        `json:"title"`
	Description  string        `json:"description,omitempty"`
	Sections     []FormSection `json:"sections"`
	SubmitLabel  string        `json:"submitLabel"`  // e.g., "Save", "Create", "Apply"
	CancelLabel  string        `json:"cancelLabel"`  // e.g., "Cancel", "Back"
	DataSource   string        `json:"dataSource"`   // API endpoint for loading data
	SubmitAction string        `json:"submitAction"` // API endpoint for submitting
}

func (f Form) Type() ComponentType { return ComponentForm }
func (f Form) ID() string          { return f.ComponentID }

// StatValue defines a single statistic to display.
type StatValue struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Icon        string `json:"icon,omitempty"`
	Format      string `json:"format,omitempty"` // "number", "bytes", "percent", "duration"
	Color       string `json:"color,omitempty"`  // "green", "red", "yellow", "blue"
	Description string `json:"description,omitempty"`
}

// Stats defines a statistics/metrics display component.
type Stats struct {
	ComponentID string      `json:"id"`
	Title       string      `json:"title,omitempty"`
	Values      []StatValue `json:"values"`
	Columns     int         `json:"columns"` // Number of columns (1-4)
	DataSource  string      `json:"dataSource"`
}

func (s Stats) Type() ComponentType { return ComponentStats }
func (s Stats) ID() string          { return s.ComponentID }

// Card defines a card component for displaying grouped information.
type Card struct {
	ComponentID string      `json:"id"`
	Title       string      `json:"title"`
	Subtitle    string      `json:"subtitle,omitempty"`
	Icon        string      `json:"icon,omitempty"`
	Content     []Component `json:"content,omitempty"` // Nested components
	Actions     []Action    `json:"actions,omitempty"`
	Collapsible bool        `json:"collapsible,omitempty"`
}

func (c Card) Type() ComponentType { return ComponentCard }
func (c Card) ID() string          { return c.ComponentID }

// Action defines a button/action.
type Action struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Icon     string `json:"icon,omitempty"`
	Shortcut string `json:"shortcut,omitempty"` // Keyboard shortcut (e.g., "a", "n", "ctrl+s")
	Variant  string `json:"variant,omitempty"`  // "primary", "secondary", "danger"
	Disabled bool   `json:"disabled,omitempty"`
	Confirm  string `json:"confirm,omitempty"`  // Confirmation message
	Endpoint string `json:"endpoint,omitempty"` // API endpoint to call
}

// Alert defines an alert/notification component.
type Alert struct {
	ComponentID string `json:"id"`
	Title       string `json:"title"`
	Message     string `json:"message"`
	Severity    string `json:"severity"` // "info", "success", "warning", "error"
	Dismissible bool   `json:"dismissible,omitempty"`
}

func (a Alert) Type() ComponentType { return ComponentAlert }
func (a Alert) ID() string          { return a.ComponentID }

// Tab defines a single tab in a tabbed interface.
type Tab struct {
	ID      string      `json:"id"`
	Label   string      `json:"label"`
	Icon    string      `json:"icon,omitempty"`
	Content []Component `json:"content"`
	Badge   string      `json:"badge,omitempty"`
}

// Tabs defines a tabbed interface component.
type Tabs struct {
	ComponentID string `json:"id"`
	Tabs        []Tab  `json:"tabs"`
	DefaultTab  string `json:"defaultTab,omitempty"`
}

func (t Tabs) Type() ComponentType { return ComponentTabs }
func (t Tabs) ID() string          { return t.ComponentID }
