package config

import (
	"fmt"

	"github.com/spf13/viper"
)

type FortsaConfig struct {
	// don't restart pods, only report what would be done
	DryRun bool

	// rate-limit to this many restarts per minute
	RestartsPerMinute float32

	// rate-limit to this many simultanous active restarts
	ActiveRestartLimit int
}

func GetConfig() (FortsaConfig, error) {

	viper.SetDefault("DryRun", false)
	viper.SetDefault("RestartsPerMinute", 5.0)
	viper.SetDefault("ActiveRestartLimit", 5)

	viper.SetEnvPrefix("FORTSA")
	viper.AutomaticEnv()

	var cfg FortsaConfig
	err := viper.Unmarshal(&cfg)
	if err != nil {
		return cfg, err
	}

	if cfg.DryRun {
		fmt.Println("DRY RUN MODE ACTIVE")
	}
	fmt.Printf("RestartsPerMinute: %v\n", cfg.RestartsPerMinute)
	fmt.Printf("ActiveRestartLimit: %v\n", cfg.ActiveRestartLimit)

	return cfg, nil
}
