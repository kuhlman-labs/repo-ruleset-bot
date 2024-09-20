package reporulesetbot_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kuhlman-labs/repo-ruleset-bot/reporulesetbot"
	"github.com/stretchr/testify/assert"
)

func TestReadConfig_ValidConfig(t *testing.T) {
	// Create a temporary directory
	dir, err := os.MkdirTemp("", "config_test")
	assert.NoError(t, err)
	defer os.RemoveAll(dir)

	// Create a valid config file
	configContent := `
server:
  address: "127.0.0.1"
  port: 8080
github:
  app:
    integration_id: 12345
    private_key: "some_private_key"
    webhook_secret: "some_webhook_secret"
  v3_api_url: "https://api.github.com"
`
	configPath := filepath.Join(dir, "config.yml")
	err = os.WriteFile(configPath, []byte(configContent), 0644)
	assert.NoError(t, err)

	// Read the config
	config, err := reporulesetbot.ReadConfig(configPath)
	assert.NoError(t, err)
	assert.NotNil(t, config)
	assert.Equal(t, "127.0.0.1", config.Server.Address)
	assert.Equal(t, 8080, config.Server.Port)
	assert.Equal(t, int64(12345), config.Github.App.IntegrationID)
	assert.Equal(t, "some_private_key", config.Github.App.PrivateKey)
	assert.Equal(t, "some_webhook_secret", config.Github.App.WebhookSecret)
	assert.Equal(t, "https://api.github.com", config.Github.V3APIURL)
}

func TestReadConfig_NonExistentFile(t *testing.T) {
	// Read a non-existent config file
	config, err := reporulesetbot.ReadConfig("non_existent_config.yml")
	assert.Error(t, err)
	assert.Nil(t, config)
}

func TestReadConfig_InvalidYAML(t *testing.T) {
	// Create a temporary directory
	dir, err := os.MkdirTemp("", "config_test")
	assert.NoError(t, err)
	defer os.RemoveAll(dir)

	// Create an invalid YAML config file
	configContent := `
server:
  address: "127.0.0.1"
  port: "not_an_int"
github:
  app:
    integration_id: 12345
    private_key: "some_private_key"
    webhook_secret: "some_webhook_secret"
  v3_api_url: "https://api.github.com"
`
	configPath := filepath.Join(dir, "invalid_config.yml")
	err = os.WriteFile(configPath, []byte(configContent), 0644)
	assert.NoError(t, err)

	// Read the config
	config, err := reporulesetbot.ReadConfig(configPath)
	assert.Error(t, err)
	assert.Nil(t, config)
}

func TestReadConfig_MissingRequiredFields(t *testing.T) {
	// Create a temporary directory
	dir, err := os.MkdirTemp("", "config_test")
	assert.NoError(t, err)
	defer os.RemoveAll(dir)

	// Create a config file with missing required fields
	configContent := `
server:
  address: ""
  port: 8080
github:
  app:
    integration_id: 12345
    private_key: "some_private_key"
    webhook_secret: ""
  v3_api_url: "https://api.github.com"
`
	configPath := filepath.Join(dir, "missing_fields_config.yml")
	err = os.WriteFile(configPath, []byte(configContent), 0644)
	assert.NoError(t, err)

	// Read the config
	config, err := reporulesetbot.ReadConfig(configPath)
	assert.Error(t, err)
	assert.Nil(t, config)
}
