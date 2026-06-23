package config

type Config struct {
	Port string
}

func Default() Config {
	return Config{
		Port: "50051",
	}
}
