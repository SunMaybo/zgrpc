package config

import "github.com/SunMaybo/zero/common/zcfg"

type Config struct {
	Zero    zcfg.ZeroConfig `yaml:"zero"`
	Target  string          `yaml:"target"`
	Feature string          `yaml:"feature"`
}
