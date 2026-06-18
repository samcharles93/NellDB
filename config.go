package nell

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Port      int    `yaml:"port"`
		DataDir   string `yaml:"data_dir"`
		MaxSkewMS int    `yaml:"max_skew_ms"`
		TLSCert   string `yaml:"tls_cert"`
		TLSKey    string `yaml:"tls_key"`
	} `yaml:"server"`
	Web struct {
		Enabled bool `yaml:"enabled"`
	} `yaml:"web"`
	Auth struct {
		JWKSURL                    string `yaml:"jwks_url"`
		JWKSRefreshIntervalSeconds int    `yaml:"jwks_refresh_interval_seconds"`
		Secret                     string `yaml:"secret"`
	} `yaml:"auth"`
	Sync struct {
		MaxBatchSize          int `yaml:"max_batch_size"`
		StalenessEvictionDays int `yaml:"staleness_eviction_days"`
	} `yaml:"sync"`
	Discovery struct {
		Enabled bool `yaml:"enabled"`
	} `yaml:"discovery"`
	Compaction struct {
		IntervalMinutes  int `yaml:"interval_minutes"`
		TombstoneTTLHours int `yaml:"tombstone_ttl_hours"`
	} `yaml:"compaction"`
	Storage struct {
		// FlushIntervalMs controls group-commit batching for the logstore.
		// 0 (default) flushes after every write (process-crash safe, one
		// syscall per write).  >0 flushes on a background ticker, trading
		// up to that many ms of writes on a crash for higher throughput.
		FlushIntervalMs int `yaml:"flush_interval_ms"`
		// CompressionLevel sets the Zstd encoder level: "fastest",
		// "default", "better", or "best".  Defaults to "default".
		CompressionLevel string `yaml:"compression_level"`
	} `yaml:"storage"`
	Peers []string `yaml:"peers"`
	Vector struct {
		EnableHNSW                bool `yaml:"enable_hnsw"`
		PCADimensions             int  `yaml:"pca_dimensions"`
		TrainingSampleSize        int  `yaml:"training_sample_size"`
		RetrainingInsertThreshold int  `yaml:"retraining_insert_threshold"`
		PQSubspaces               int  `yaml:"pq_subspaces"`
		PQCentroids               int  `yaml:"pq_centroids"`
	} `yaml:"vector"`
}

func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	expanded := os.ExpandEnv(string(raw))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func DefaultConfig() *Config {
	cfg := &Config{}
	cfg.Server.Port = 9342
	cfg.Server.DataDir = "."
	cfg.Server.MaxSkewMS = 500
	cfg.Web.Enabled = false
	cfg.Sync.MaxBatchSize = 1000
	cfg.Sync.StalenessEvictionDays = 14
	cfg.Compaction.IntervalMinutes = 60
	cfg.Compaction.TombstoneTTLHours = 168
	cfg.Vector.EnableHNSW = true
	cfg.Vector.PCADimensions = 128
	cfg.Vector.TrainingSampleSize = 5000
	cfg.Vector.RetrainingInsertThreshold = 50000
	cfg.Vector.PQSubspaces = 16
	cfg.Vector.PQCentroids = 256
	return cfg
}
