package odoo

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/spf13/viper"
)

// validate is shared so the validator's parsed-struct cache is effective.
var validate = validator.New(validator.WithRequiredStructEnabled())

// ClientConfig is one Odoo connection section of the YAML configuration.
type ClientConfig struct {
	BaseURL string `mapstructure:"base_url" validate:"required,http_url"`
	APIKey  string `mapstructure:"api_key"  validate:"required"`
	DB      string `mapstructure:"db"`
	// MaxRetries is the number of extra attempts for read-only calls; zero disables retries.
	MaxRetries int `mapstructure:"max_retries" validate:"gte=0,lte=10"`
}

// LoadClient reads the odoo section of a YAML configuration without logging it.
func LoadClient(path string) (*Client, error) {
	return LoadClientSection(path, "odoo")
}

// LoadClientSection reads one named top-level section of a YAML configuration without logging it.
func LoadClientSection(path, section string) (*Client, error) {
	var (
		data   []byte
		v      *viper.Viper
		config ClientConfig
		client *Client
		err    error
	)

	if section == "" || strings.Contains(section, ".") {
		return nil, fmt.Errorf("%w: config section must be a non-empty top-level key", ErrInvalidConfig)
	}
	if data, err = os.ReadFile(path); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	v = viper.New()
	v.SetConfigType("yaml")
	// Decoding errors can contain configuration values, including secrets, so they are not wrapped.
	if err = v.ReadConfig(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("%w: parse %q: invalid YAML", ErrInvalidConfig, path)
	}
	if section != "odoo" && !v.IsSet(section) {
		return nil, fmt.Errorf("%w: config section %q is missing in %q", ErrInvalidConfig, section, path)
	}
	if err = v.UnmarshalKey(section, &config); err != nil {
		return nil, fmt.Errorf("%w: parse %q: invalid field types", ErrInvalidConfig, path)
	}

	config.BaseURL = strings.TrimSpace(config.BaseURL)
	config.APIKey = strings.TrimSpace(config.APIKey)
	if err = validateClientConfig(config, section); err != nil {
		return nil, err
	}
	client = NewClient(config.BaseURL, config.APIKey, config.DB)
	client.Retry.MaxRetries = config.MaxRetries
	return client, nil
}

// validateClientConfig translates validator failures into messages that never include values.
func validateClientConfig(config ClientConfig, section string) error {
	var (
		errs validator.ValidationErrors
		e    validator.FieldError
		err  error
	)

	if err = validate.Struct(config); err == nil {
		return nil
	}
	if !errors.As(err, &errs) {
		return fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	for _, e = range errs {
		switch e.Field() {
		case "BaseURL":
			return fmt.Errorf("%w: %s.base_url must be an absolute HTTP or HTTPS URL", ErrInvalidConfig, section)
		case "APIKey":
			return fmt.Errorf("%w: %s.api_key is required", ErrInvalidConfig, section)
		case "MaxRetries":
			return fmt.Errorf("%w: %s.max_retries must be between 0 and 10", ErrInvalidConfig, section)
		}
	}
	return fmt.Errorf("%w: %w", ErrInvalidConfig, err)
}
