package config

import "github.com/spf13/viper"

func ConfigDefaults() {
	viper.SetDefault("istioSystemNamespace", "istio-system")

}
