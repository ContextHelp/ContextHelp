package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
)

const (
	// DefaultConfigFileName is the default configuration file name
	DefaultConfigFileName = "config.yaml"
	// DefaultConfigDir is the default configuration directory
	DefaultConfigDir = ".config/contexthelp"
	// EnvConfigPath is the environment variable for config path override
	EnvConfigPath = "CTXT_CONFIG"
	// EnvDataDir is the environment variable for data directory override
	EnvDataDir = "CTXT_DATA_DIR"
	// EnvProfile is the environment variable for default focus profile
	EnvProfile = "CTXT_PROFILE"
	// EnvDPKMSDataDir is the environment variable for dPKMS data directory
	EnvDPKMSDataDir = "DPKMS_DATA_DIR"
	// EnvDPKMSWorkers is the environment variable for worker thread count
	EnvDPKMSWorkers = "DPKMS_WORKERS"
)

// DuplicatesConfig controls duplicate and near-duplicate detection behaviour at ingest time.
type DuplicatesConfig struct {
	// Policy determines what happens when a duplicate is found.
	// Valid values: "warn" (default), "drop", "keep".
	Policy string `mapstructure:"policy"`
	// SimilarityThreshold is the cosine similarity cutoff for near-duplicate detection.
	// Range: 0.0–1.0. Default: 0.95.
	SimilarityThreshold float64 `mapstructure:"similarity_threshold"`
	// CheckExact enables content-hash exact-match deduplication. Default: true.
	CheckExact bool `mapstructure:"check_exact"`
	// CheckSimilar enables vector-embedding near-duplicate detection. Default: false.
	// Requires embeddings to have been computed (pipeline embedding step must run first).
	CheckSimilar bool `mapstructure:"check_similar"`
}

// Config represents the application configuration
type Config struct {
	// Storage configuration
	Storage StorageConfig `mapstructure:"storage"`

	// Server configuration
	Server ServerConfig `mapstructure:"server"`

	// Profile configuration
	Profile ProfileConfig `mapstructure:"profile"`

	// Registries configuration
	Registries []RegistryConfig `mapstructure:"registries"`

	// Plugins configuration
	Plugins []PluginConfig `mapstructure:"plugins"`

	// I18n configuration
	I18n I18nConfig `mapstructure:"i18n"`

	// Providers configuration
	Providers ProvidersConfig `mapstructure:"providers"`

	// Retrieval configuration
	Retrieval RetrievalConfig `mapstructure:"retrieval"`

	// Duplicates controls duplicate detection policy.
	Duplicates DuplicatesConfig `mapstructure:"duplicates"`
}

// RetrievalConfig controls progressive retrieval behaviour.
type RetrievalConfig struct {
	Method                 string     `mapstructure:"method" yaml:"method" json:"method"` // "rag" or "llm"
	EnableSufficiencyCheck bool       `mapstructure:"enable_sufficiency_check" yaml:"enable_sufficiency_check" json:"enable_sufficiency_check"`
	LLMProfile             string     `mapstructure:"llm_profile" yaml:"llm_profile" json:"llm_profile"`
	Categories             TierConfig `mapstructure:"categories" yaml:"categories" json:"categories"`
	Items                  TierConfig `mapstructure:"items" yaml:"items" json:"items"`
	Resources              TierConfig `mapstructure:"resources" yaml:"resources" json:"resources"`
}

// TierConfig controls a single retrieval tier.
type TierConfig struct {
	Enabled bool `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	TopK    int  `mapstructure:"top_k" yaml:"top_k" json:"top_k"`
}

// DefaultRetrievalConfig returns sensible defaults for retrieval.
func DefaultRetrievalConfig() RetrievalConfig {
	return RetrievalConfig{
		Method:                 "rag",
		EnableSufficiencyCheck: true,
		LLMProfile:             "default",
		Categories:             TierConfig{Enabled: true, TopK: 10},
		Items:                  TierConfig{Enabled: true, TopK: 20},
		Resources:              TierConfig{Enabled: true, TopK: 5},
	}
}

// StorageConfig represents storage configuration
type StorageConfig struct {
	Type string     `mapstructure:"type"`
	Path string     `mapstructure:"path"`
	Blob BlobConfig `mapstructure:"blob"`
}

// BlobConfig holds configuration for the blob storage backend.
type BlobConfig struct {
	Backend   string          `mapstructure:"backend"`
	Threshold int64           `mapstructure:"threshold"`
	Local     BlobLocalConfig `mapstructure:"local"`
	S3        BlobS3Config    `mapstructure:"s3"`
}

// BlobLocalConfig configures the local filesystem blob backend.
type BlobLocalConfig struct {
	Path string `mapstructure:"path"`
}

// BlobS3Config configures the S3-compatible blob backend.
type BlobS3Config struct {
	Endpoint      string        `mapstructure:"endpoint"`
	Region        string        `mapstructure:"region"`
	Bucket        string        `mapstructure:"bucket"`
	Prefix        string        `mapstructure:"prefix"`
	AccessKey     string        `mapstructure:"access_key"`
	SecretKey     string        `mapstructure:"secret_key"`
	UsePathStyle  bool          `mapstructure:"use_path_style"`
	PresignExpiry time.Duration `mapstructure:"presign_expiry"`
	MaxRetries    int           `mapstructure:"max_retries"`
}

// ServerConfig represents server configuration
type ServerConfig struct {
	Port     int    `mapstructure:"port"`
	GRPCPort int    `mapstructure:"grpc_port"`
	Workers  int    `mapstructure:"workers"`
	Public   bool   `mapstructure:"public"`
}

// ProfileConfig represents profile configuration
type ProfileConfig struct {
	Default string `mapstructure:"default"`
}

// RegistryConfig represents a registry configuration
type RegistryConfig struct {
	Name string `mapstructure:"name"`
	URL  string `mapstructure:"url"`
}

// PluginConfig represents a plugin configuration
type PluginConfig struct {
	Type   string                 `mapstructure:"type"`
	Plugin string                 `mapstructure:"plugin"`
	Config map[string]interface{} `mapstructure:"config"`
}

// I18nConfig represents i18n configuration
type I18nConfig struct {
	Enabled            bool     `mapstructure:"enabled"`
	PreferredLanguages []string `mapstructure:"preferred_languages"`
	AutoTranslate      bool     `mapstructure:"auto_translate"`
	TranslateTags      bool     `mapstructure:"translate_tags"`
}

// ProvidersConfig controls backend selection for each provider type.
type ProvidersConfig struct {
	Video         ProviderBackendConfig `mapstructure:"video"`
	Document      ProviderBackendConfig `mapstructure:"document"`
	OCR           ProviderBackendConfig `mapstructure:"ocr"`
	Transcription ProviderBackendConfig `mapstructure:"transcription"`
	Vision        ProviderBackendConfig `mapstructure:"vision"`
	Diarization   ProviderBackendConfig `mapstructure:"diarization"`
	LLM           ProviderBackendConfig `mapstructure:"llm"`
	Embedding     ProviderBackendConfig `mapstructure:"embedding"`
}

// ProviderBackendConfig selects which backend to use for a provider.
type ProviderBackendConfig struct {
	Backend string `mapstructure:"backend"`
	// Model overrides the default model for LLM-based providers (vision, transcription via ollama).
	Model string `mapstructure:"model,omitempty"`
	// Endpoint overrides the default API endpoint (e.g. Ollama URL).
	Endpoint string `mapstructure:"endpoint,omitempty"`
	// Language sets a default language hint (e.g. for OCR, transcription).
	Language string `mapstructure:"language,omitempty"`
}

// Load loads the configuration from file and environment variables
func Load(cfgFile string) (*Config, error) {
	v := viper.New()

	// Set configuration file
	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		// Check environment variable
		if envConfig := os.Getenv(EnvConfigPath); envConfig != "" {
			v.SetConfigFile(envConfig)
		} else {
			// Use default location
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("failed to get home directory: %w", err)
			}

			configPath := filepath.Join(home, DefaultConfigDir)
			v.AddConfigPath(configPath)
			v.SetConfigName("config")
			v.SetConfigType("yaml")
		}
	}

	// Set defaults
	setDefaults(v)

	// Bind environment variables
	bindEnvVars(v)

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}
		// Config file not found is acceptable
	}

	// Unmarshal configuration
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &cfg, nil
}

// setDefaults sets default configuration values
func setDefaults(v *viper.Viper) {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".local", "share", "contexthelp")

	// Storage defaults
	v.SetDefault("storage.type", "sqlite")
	v.SetDefault("storage.path", filepath.Join(dataDir, "db.sqlite"))

	// Blob storage defaults
	v.SetDefault("storage.blob.backend", "local")
	v.SetDefault("storage.blob.threshold", int64(65536))
	v.SetDefault("storage.blob.local.path", filepath.Join(dataDir, "blobs"))
	v.SetDefault("storage.blob.s3.region", "us-east-1")
	v.SetDefault("storage.blob.s3.use_path_style", false)
	v.SetDefault("storage.blob.s3.presign_expiry", time.Hour)
	v.SetDefault("storage.blob.s3.max_retries", 3)

	// Server defaults
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.grpc_port", 9090)
	v.SetDefault("server.workers", 4)
	v.SetDefault("server.public", false)

	// Profile defaults
	v.SetDefault("profile.default", "")

	// I18n defaults
	v.SetDefault("i18n.enabled", false)
	v.SetDefault("i18n.preferred_languages", []string{"en"})
	v.SetDefault("i18n.auto_translate", false)
	v.SetDefault("i18n.translate_tags", false)

	// Duplicates defaults
	v.SetDefault("duplicates.policy", "warn")
	v.SetDefault("duplicates.similarity_threshold", 0.95)
	v.SetDefault("duplicates.check_exact", true)
	v.SetDefault("duplicates.check_similar", false)

	// Provider defaults — "auto" probes for tools, falls back to stub
	v.SetDefault("providers.video.backend", "auto")
	v.SetDefault("providers.document.backend", "auto")
	v.SetDefault("providers.ocr.backend", "auto")
	v.SetDefault("providers.transcription.backend", "auto")
	v.SetDefault("providers.vision.backend", "auto")
	v.SetDefault("providers.vision.endpoint", "http://localhost:11434")
	v.SetDefault("providers.vision.model", "llava")
	v.SetDefault("providers.diarization.backend", "auto")
	v.SetDefault("providers.llm.backend", "auto")
	v.SetDefault("providers.llm.endpoint", "http://localhost:11434")
	v.SetDefault("providers.llm.model", "")
}

// bindEnvVars binds environment variables to configuration keys
func bindEnvVars(v *viper.Viper) {
	v.SetEnvPrefix("CH")
	v.AutomaticEnv()

	// Explicit bindings for common overrides
	v.BindEnv("storage.type", "CH_STORAGE_TYPE")
	v.BindEnv("storage.path", EnvDataDir)

	// Blob storage env var bindings
	v.BindEnv("storage.blob.backend", "CTXT_BLOB_BACKEND")
	v.BindEnv("storage.blob.threshold", "CTXT_BLOB_THRESHOLD")
	v.BindEnv("storage.blob.s3.endpoint", "CTXT_BLOB_S3_ENDPOINT")
	v.BindEnv("storage.blob.s3.region", "CTXT_BLOB_S3_REGION")
	v.BindEnv("storage.blob.s3.bucket", "CTXT_BLOB_S3_BUCKET")
	v.BindEnv("storage.blob.s3.prefix", "CTXT_BLOB_S3_PREFIX")
	v.BindEnv("storage.blob.s3.access_key", "CTXT_BLOB_S3_ACCESS_KEY")
	v.BindEnv("storage.blob.s3.secret_key", "CTXT_BLOB_S3_SECRET_KEY")
	v.BindEnv("server.port", "CH_SERVER_PORT")
	v.BindEnv("server.grpc_port", "CH_GRPC_PORT")
	v.BindEnv("server.workers", EnvDPKMSWorkers)
	v.BindEnv("server.public", "CH_PUBLIC")
	v.BindEnv("profile.default", EnvProfile)
}

// GetConfigPath returns the configuration file path being used
func GetConfigPath() string {
	if cfgPath := os.Getenv(EnvConfigPath); cfgPath != "" {
		return cfgPath
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(home, DefaultConfigDir, DefaultConfigFileName)
}

// EnsureConfigDir ensures the configuration directory exists
func EnsureConfigDir() error {
	configPath := GetConfigPath()
	if configPath == "" {
		return fmt.Errorf("failed to determine config path")
	}

	configDir := filepath.Dir(configPath)
	return os.MkdirAll(configDir, 0755)
}

// EnsureDataDir ensures the data directory exists
func EnsureDataDir() error {
	dataDir := os.Getenv(EnvDataDir)
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		dataDir = filepath.Join(home, ".local", "share", "contexthelp")
	}

	return os.MkdirAll(dataDir, 0755)
}
