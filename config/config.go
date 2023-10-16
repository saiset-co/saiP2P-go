package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config model
type Config struct {
	P2P struct {
		Port string `yaml:"port"`
		Slot int    `yaml:"slot"`
	} `yaml:"p2p"`
	Http struct {
		Port string
	}
	Peers                     []string `yaml:"peers"`
	OnBroadcastMessageReceive []string
	OnDirectMessageReceive    []string
	DebugMode                 bool `yaml:"debug"`
}

// Get - parses config.yml, return config struct
func Get() (Config, error) {
	config := Config{}
	yamlData, err := os.ReadFile("config.yml")

	if err != nil {
		return config, fmt.Errorf("Readfile : %w", err)
	}

	err = yaml.Unmarshal(yamlData, &config)

	if err != nil {
		return config, fmt.Errorf("Unmarshal : %w", err)
	}
	return config, nil
}
