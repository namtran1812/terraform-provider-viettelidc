package config

import "os"

type Config struct{ HTTPAddr, GRPCAddr, DatabaseURL, RedisAddr, ElasticURL, JWTSecret string }

func Load() Config {
	return Config{env("HTTP_ADDR", ":8080"), env("GRPC_ADDR", ":9090"), env("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/monitor?sslmode=disable"), env("REDIS_ADDR", "localhost:6379"), env("ELASTIC_URL", "http://localhost:9200"), env("JWT_SECRET", "dev-secret-change-me")}
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
