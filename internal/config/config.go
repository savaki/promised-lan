// Package config loads and validates the promised-lan YAML configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the on-disk configuration for promised-lan.
type Config struct {
	UpstreamInterface     string `yaml:"upstream_interface"`
	InteriorInterface     string `yaml:"interior_interface"`
	HTTPPort              int    `yaml:"http_port"`
	ConnectionName        string `yaml:"connection_name"`
	ConnectTimeoutSeconds int    `yaml:"connect_timeout_seconds"`
}

var connectionNameRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// applyDefaults fills zero-valued fields with the documented defaults.
func (c *Config) applyDefaults() {
	if c.UpstreamInterface == "" {
		c.UpstreamInterface = "wlan1"
	}
	if c.InteriorInterface == "" {
		c.InteriorInterface = "wlan0"
	}
	if c.HTTPPort == 0 {
		c.HTTPPort = 8080
	}
	if c.ConnectionName == "" {
		c.ConnectionName = "promised-lan-upstream"
	}
	if c.ConnectTimeoutSeconds == 0 {
		c.ConnectTimeoutSeconds = 20
	}
}

// Validate returns all validation errors joined with errors.Join.
func (c Config) Validate() error {
	var errs []error
	if c.UpstreamInterface == c.InteriorInterface {
		errs = append(errs, errors.New("upstream_interface must not equal interior_interface"))
	}
	if c.HTTPPort < 1 || c.HTTPPort > 65535 {
		errs = append(errs, fmt.Errorf("http_port must be 1..65535, got %d", c.HTTPPort))
	}
	if c.ConnectTimeoutSeconds < 5 || c.ConnectTimeoutSeconds > 120 {
		errs = append(errs, fmt.Errorf("connect_timeout_seconds must be 5..120, got %d", c.ConnectTimeoutSeconds))
	}
	if !connectionNameRe.MatchString(c.ConnectionName) {
		errs = append(errs, fmt.Errorf("connection_name %q must match %s", c.ConnectionName, connectionNameRe.String()))
	}
	return errors.Join(errs...)
}

// Load reads a YAML config file, applies defaults, and validates.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return Config{}, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return c, nil
}

// Marshal returns the canonical YAML form of this config, suitable for writing
// as the default /etc/promised-lan/config.yaml.
func (c Config) Marshal() ([]byte, error) {
	var buf strings.Builder
	buf.WriteString("# promised-lan configuration\n")
	out, err := yaml.Marshal(c)
	if err != nil {
		return nil, err
	}
	buf.Write(out)
	return []byte(buf.String()), nil
}
