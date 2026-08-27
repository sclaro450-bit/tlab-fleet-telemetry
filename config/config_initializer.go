package config

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus/hooks/test"

	"github.com/teslamotors/fleet-telemetry/datastore/simple"
	logrus "github.com/teslamotors/fleet-telemetry/logger"
	"github.com/teslamotors/fleet-telemetry/metrics"
	"github.com/teslamotors/fleet-telemetry/telemetry"
)

var (
	maxVinsToTrack = 20
)

const (
	defaultHost       = "0.0.0.0"
	defaultPort       = 443
	defaultStatusPort = 8080
	defaultLogLevel   = "info"
	defaultNamespace  = "tesla_telemetry"
)

// LoadApplicationConfiguration loads the configuration from args and config files
func LoadApplicationConfiguration() (config *Config, logger *logrus.Logger, err error) {

	logger, err = logrus.NewBasicLogrusLogger("fleet-telemetry")
	if err != nil {
		return nil, nil, err
	}
	log.SetOutput(logger)

	configFilePath := loadConfigFlags()

	config, err = loadApplicationConfig(configFilePath)
	if err != nil {
		return nil, nil, err
	}
	config.configureLogger(logger)
	logger.ActivityLog("configuration_loaded", logrus.LogInfo{
		"config_file": configFilePath,
		"host":        config.Host,
		"port":        config.Port,
		"status_port": config.StatusPort,
		"namespace":   config.Namespace,
	})

	config.configureMetricsCollector(logger)
	return config, logger, nil
}

func loadApplicationConfig(configFilePath string) (*Config, error) {
	config := &Config{
		LoggerConfig: &simple.Config{},
	}

	configFile, err := os.Open(configFilePath)
	if err == nil {
		defer configFile.Close()
		if err = json.NewDecoder(configFile).Decode(config); err != nil {
			return nil, fmt.Errorf("decode config file %q: %w", configFilePath, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("open config file %q: %w", configFilePath, err)
	}

	applySafeDefaults(config)
	if err = applyEnvironmentOverrides(config); err != nil {
		return nil, err
	}
	if err = materializeInlineTLSSecrets(config); err != nil {
		return nil, err
	}

	log, _ := test.NewNullLogger()
	logger, err := logrus.NewLogrusLogger("null_logger", map[string]interface{}{}, log.WithField("context", "metrics"))
	if err != nil {
		return nil, err
	}

	if err := validateConfig(config); err != nil {
		return nil, err
	}
	config.MetricCollector = metrics.NewCollector(config.Monitoring, logger)
	config.AckChan = make(chan *telemetry.Record)
	return config, err
}

func validateConfig(config *Config) error {
	if strings.TrimSpace(config.Host) == "" {
		return errors.New("missing HOST")
	}
	if config.Port < 1 || config.Port > 65535 {
		return fmt.Errorf("invalid PORT %d: must be between 1 and 65535", config.Port)
	}
	if config.StatusPort < 0 || config.StatusPort > 65535 {
		return fmt.Errorf("invalid STATUS_PORT %d: must be between 0 and 65535", config.StatusPort)
	}
	if strings.TrimSpace(config.Namespace) == "" {
		return errors.New("missing TELEMETRY_NAMESPACE")
	}
	if len(config.Records) == 0 {
		return errors.New("missing records dispatcher configuration")
	}
	if len(config.VinsToTrack()) > maxVinsToTrack {
		return fmt.Errorf("set the value of `vins_signal_tracking_enabled` less than %d unique vins", maxVinsToTrack)
	}
	return nil
}

// ValidateRuntime verifies values that are mandatory only when the real Tesla
// listener starts. Unit tools may load partial configs without opening a port.
func (config *Config) ValidateRuntime() error {
	if config.TLS == nil {
		return errors.New("missing TLS configuration: Tesla Fleet Telemetry requires direct mTLS")
	}
	if strings.TrimSpace(config.TLS.ServerCert) == "" {
		return errors.New("missing TLS certificate: set TLS_CERT_PATH or TLS_CERT_PEM")
	}
	if strings.TrimSpace(config.TLS.ServerKey) == "" {
		return errors.New("missing TLS private key: set TLS_KEY_PATH or TLS_KEY_PEM")
	}
	return nil
}

func applySafeDefaults(config *Config) {
	if strings.TrimSpace(config.Host) == "" {
		config.Host = defaultHost
	}
	if config.Port == 0 {
		config.Port = defaultPort
	}
	if config.StatusPort == 0 {
		config.StatusPort = defaultStatusPort
	}
	if strings.TrimSpace(config.Namespace) == "" {
		config.Namespace = defaultNamespace
	}
	if strings.TrimSpace(config.LogLevel) == "" {
		config.LogLevel = defaultLogLevel
	}
	if len(config.Records) == 0 {
		config.Records = map[string][]telemetry.Dispatcher{
			"alerts":       {telemetry.Logger},
			"errors":       {telemetry.Logger},
			"V":            {telemetry.Logger},
			"connectivity": {telemetry.Logger},
		}
	}
}

func loadConfigFlags() string {
	applicationConfig := os.Getenv("CONFIG_FILE")
	if applicationConfig == "" {
		applicationConfig = "config.json"
	}
	flag.StringVar(&applicationConfig, "config", applicationConfig, "application configuration file")

	flag.Parse()
	return applicationConfig
}

func applyEnvironmentOverrides(config *Config) error {
	if value := strings.TrimSpace(os.Getenv("HOST")); value != "" {
		config.Host = value
	}
	if err := overrideInt("PORT", &config.Port); err != nil {
		return err
	}
	if err := overrideInt("STATUS_PORT", &config.StatusPort); err != nil {
		return err
	}
	if value := strings.TrimSpace(os.Getenv("LOG_LEVEL")); value != "" {
		config.LogLevel = value
	}
	if value := strings.TrimSpace(os.Getenv("TELEMETRY_NAMESPACE")); value != "" {
		config.Namespace = value
	}

	certPath := strings.TrimSpace(os.Getenv("TLS_CERT_PATH"))
	keyPath := strings.TrimSpace(os.Getenv("TLS_KEY_PATH"))
	caPath := strings.TrimSpace(os.Getenv("TLS_CA_PATH"))
	if certPath != "" || keyPath != "" || caPath != "" {
		if config.TLS == nil {
			config.TLS = &TLS{}
		}
		if certPath != "" {
			config.TLS.ServerCert = certPath
		}
		if keyPath != "" {
			config.TLS.ServerKey = keyPath
		}
		if caPath != "" {
			config.TLS.CAFile = caPath
		}
	}

	if value := strings.TrimSpace(os.Getenv("TLS_ENABLED")); value != "" {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid TLS_ENABLED %q: %w", value, err)
		}
		if !enabled {
			return errors.New("TLS_ENABLED=false is unsupported: Tesla vehicles require direct mTLS; deploy the listener on a persistent TCP service")
		}
	}

	return nil
}

func materializeInlineTLSSecrets(config *Config) error {
	certificatePEM := os.Getenv("TLS_CERT_PEM")
	privateKeyPEM := os.Getenv("TLS_KEY_PEM")
	if certificatePEM == "" && privateKeyPEM == "" {
		return nil
	}
	if certificatePEM == "" {
		return errors.New("TLS_CERT_PEM is required when TLS_KEY_PEM is set")
	}
	if privateKeyPEM == "" {
		return errors.New("TLS_KEY_PEM is required when TLS_CERT_PEM is set")
	}

	directory, err := os.MkdirTemp("", "fleet-telemetry-tls-")
	if err != nil {
		return fmt.Errorf("create TLS secret directory: %w", err)
	}
	certificatePath := directory + "/tls.crt"
	privateKeyPath := directory + "/tls.key"
	if err := os.WriteFile(certificatePath, []byte(certificatePEM), 0600); err != nil {
		return fmt.Errorf("write TLS certificate secret: %w", err)
	}
	if err := os.WriteFile(privateKeyPath, []byte(privateKeyPEM), 0600); err != nil {
		return fmt.Errorf("write TLS private key secret: %w", err)
	}
	if config.TLS == nil {
		config.TLS = &TLS{}
	}
	config.TLS.ServerCert = certificatePath
	config.TLS.ServerKey = privateKeyPath
	return nil
}

func overrideInt(name string, destination *int) error {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("invalid %s %q: %w", name, value, err)
	}
	*destination = parsed
	return nil
}
