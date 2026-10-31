package main

import "os"

type Config struct {
	EngineFeed string
	GatewayID  string
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func LoadConfig() Config {
	return Config{
		EngineFeed: env("MDG_ENGINE_FEED", "127.0.0.1:50061"),
		GatewayID:  env("MDG_GATEWAY_ID", "md-1"),
	}
}
