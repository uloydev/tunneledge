package config

type options struct {
	configPath string
	configName string
}

type Option func(*options)

func WithConfigPath(path string) Option {
	return func(o *options) {
		o.configPath = path
	}
}

func WithConfigName(name string) Option {
	return func(o *options) {
		o.configName = name
	}
}
