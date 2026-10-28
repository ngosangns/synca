// Package mcpregistry is the Official MCP Registry client. It reads server
// metadata from registry.modelcontextprotocol.io and does no file IO.
package mcpregistry

// Repository is the source repository of a server.
type Repository struct {
	URL       string `json:"url"`
	Source    string `json:"source"`
	Subfolder string `json:"subfolder,omitempty"`
}

// Server is one published MCP server version.
type Server struct {
	Name        string      `json:"name"`
	Title       string      `json:"title,omitempty"`
	Description string      `json:"description"`
	Version     string      `json:"version"`
	Repository  *Repository `json:"repository,omitempty"`
	WebsiteURL  string      `json:"websiteUrl,omitempty"`
	Packages    []Package   `json:"packages,omitempty"`
	Remotes     []Remote    `json:"remotes,omitempty"`
}

// ServerRecord is a server plus the registry metadata around it.
type ServerRecord struct {
	Server Server `json:"server"`
	Meta   Meta   `json:"_meta,omitempty"`
}

// Meta holds registry-managed metadata.
type Meta struct {
	Official *Official `json:"io.modelcontextprotocol.registry/official,omitempty"`
}

// Official is the official registry's status block.
type Official struct {
	Status          string `json:"status"`
	StatusChangedAt string `json:"statusChangedAt"`
	PublishedAt     string `json:"publishedAt"`
	UpdatedAt       string `json:"updatedAt"`
	IsLatest        bool   `json:"isLatest"`
}

// ListResponse is one page of servers.
type ListResponse struct {
	Servers  []ServerRecord `json:"servers"`
	Metadata PageMetadata   `json:"metadata"`
}

// PageMetadata is the cursor envelope.
type PageMetadata struct {
	Count      int    `json:"count"`
	NextCursor string `json:"nextCursor"`
}

// Package is a downloadable server package.
type Package struct {
	RegistryType         string           `json:"registryType"`
	RegistryBaseURL      string           `json:"registryBaseUrl,omitempty"`
	Identifier           string           `json:"identifier"`
	Version              string           `json:"version"`
	FileSHA256           string           `json:"fileSha256,omitempty"`
	RuntimeHint          string           `json:"runtimeHint,omitempty"`
	Transport            PackageTransport `json:"transport"`
	RuntimeArguments     []Argument       `json:"runtimeArguments,omitempty"`
	PackageArguments     []Argument       `json:"packageArguments,omitempty"`
	EnvironmentVariables []EnvVar         `json:"environmentVariables,omitempty"`
}

// PackageTransport is the local transport a package uses.
type PackageTransport struct {
	Type    string   `json:"type"`
	URL     string   `json:"url,omitempty"`
	Headers []Header `json:"headers,omitempty"`
}

// Argument is a positional or named package argument. Registry values may
// contain {variables}; they are resolved by the caller.
type Argument struct {
	Type        string `json:"type"`
	Name        string `json:"name,omitempty"`
	Value       string `json:"value,omitempty"`
	ValueHint   string `json:"valueHint,omitempty"`
	Default     string `json:"default,omitempty"`
	IsRequired  bool   `json:"isRequired,omitempty"`
	IsSecret    bool   `json:"isSecret,omitempty"`
	IsRepeated  bool   `json:"isRepeated,omitempty"`
	Description string `json:"description,omitempty"`
}

// EnvVar is one environment variable a package expects.
type EnvVar struct {
	Name        string   `json:"name"`
	Value       string   `json:"value,omitempty"`
	Default     string   `json:"default,omitempty"`
	IsRequired  bool     `json:"isRequired,omitempty"`
	IsSecret    bool     `json:"isSecret,omitempty"`
	Choices     []string `json:"choices,omitempty"`
	Description string   `json:"description,omitempty"`
}

// Remote is a hosted MCP endpoint.
type Remote struct {
	Type      string           `json:"type"`
	URL       string           `json:"url"`
	Headers   []Header         `json:"headers,omitempty"`
	Variables map[string]Input `json:"variables,omitempty"`
}

// Header is one HTTP header a remote expects.
type Header struct {
	Name        string `json:"name"`
	Value       string `json:"value,omitempty"`
	Default     string `json:"default,omitempty"`
	IsRequired  bool   `json:"isRequired,omitempty"`
	IsSecret    bool   `json:"isSecret,omitempty"`
	Description string `json:"description,omitempty"`
}

// Input is a remote URL variable.
type Input struct {
	Value       string   `json:"value,omitempty"`
	Default     string   `json:"default,omitempty"`
	IsRequired  bool     `json:"isRequired,omitempty"`
	IsSecret    bool     `json:"isSecret,omitempty"`
	Choices     []string `json:"choices,omitempty"`
	Description string   `json:"description,omitempty"`
}
