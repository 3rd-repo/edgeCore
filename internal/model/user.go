package model

// ServerConfig represents server configuration
type ServerConfig struct {
	Port     int    `json:"port"`
	LogLevel string `json:"logLevel"`
}

// UserConfig represents a user configuration
type UserConfig struct {
	Username string `json:"username"`
	Password string `json:"password"` // Plain text (or hashed, but simple for now)
	Role     string `json:"role"`
}
