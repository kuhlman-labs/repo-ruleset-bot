package main

import (
	"os"

	"github.com/palantir/go-githubapp/githubapp"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

type Config struct {
	Server HTTPConfig       `yaml:"server"`
	Github githubapp.Config `yaml:"github"`

	AppConfig MyApplicationConfig `yaml:"app_configuration"`
	RuleSet   string              `yaml:"ruleset"`
}

type HTTPConfig struct {
	Address string `yaml:"address"`
	Port    int    `yaml:"port"`
}

type MyApplicationConfig struct {
	PullRequestPreamble string `yaml:"pull_request_preamble"`
}

func ReadConfig(path string) (*Config, error) {
	var c Config

	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "failed reading server config file: %s", path)
	}

	if err := yaml.UnmarshalStrict(bytes, &c); err != nil {
		return nil, errors.Wrap(err, "failed parsing configuration file")
	}

	return &c, nil
}
