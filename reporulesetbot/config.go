package reporulesetbot

import (
	"fmt"
	"os"

	"github.com/palantir/go-githubapp/githubapp"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

// Config represents the configuration of the application.
type Config struct {
	Server HTTPConfig       `yaml:"server"`
	Github githubapp.Config `yaml:"github"`
}

// HTTPConfig represents the configuration of the HTTP server.
type HTTPConfig struct {
	Address string `yaml:"address"`
	Port    int    `yaml:"port"`
}

// ReadConfig reads and parses the configuration file.
func ReadConfig(path string) (*Config, error) {
	var config Config

	// Read the file
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to read configuration file: %s", path)
	}

	// Unmarshal the YAML content
	if err := yaml.UnmarshalStrict(bytes, &config); err != nil {
		return nil, errors.Wrap(err, "Failed to parse configuration file")
	}

	// Validate the configuration
	if err := validateConfig(&config); err != nil {
		return nil, errors.Wrap(err, "Invalid configuration")
	}

	return &config, nil
}

// validateConfig validates the configuration fields.
func validateConfig(config *Config) error {
	requiredFields := map[string]interface{}{
		"Server Address":            config.Server.Address,
		"Server Port":               config.Server.Port,
		"GitHub App ID":             config.Github.App.IntegrationID,
		"GitHub App private key":    config.Github.App.PrivateKey,
		"GitHub App webhook secret": config.Github.App.WebhookSecret,
		"GitHub v3 API URL":         config.Github.V3APIURL,
	}

	for field, value := range requiredFields {
		if isEmpty(value) {
			return errors.New(fmt.Sprintf("%s field is required to be set in the config.yml file.", field))
		}
	}

	return nil
}

// isEmpty checks if a value is considered empty.
func isEmpty(value interface{}) bool {
	switch v := value.(type) {
	case string:
		return v == ""
	case int:
		return v == 0
	case []string:
		return len(v) == 0
	default:
		return value == nil
	}
}
