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
	} `yaml:"server"`
	Web struct {
		Enabled bool `yaml:"enabled"`
	} `yaml:"web"`
	Auth struct {
		JWKSURL                    string `yaml:"jwks_url"`
		JWKSRefreshIntervalSeconds int    `yaml:"jwks_refresh_interval_seconds"`
	} `yaml:"auth"`
	Sync struct {
		MaxBatchSize          int `yaml:"max_batch_size"`
		StalenessEvictionDays int `yaml:"staleness_eviction_days"`
	} `yaml:"sync"`
	Discovery struct {
		Enabled bool `yaml:"enabled"`
	} `yaml:"discovery"`
	Compaction struct {
		IntervalMinutes int `yaml:"interval_minutes"`
	} `yaml:"compaction"`
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
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg Config
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
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
	cfg.Vector.EnableHNSW = true
	cfg.Vector.PCADimensions = 128
	cfg.Vector.TrainingSampleSize = 5000
	cfg.Vector.RetrainingInsertThreshold = 50000
	cfg.Vector.PQSubspaces = 16
	cfg.Vector.PQCentroids = 256
	return cfg
}
