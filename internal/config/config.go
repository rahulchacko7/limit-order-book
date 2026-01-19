package config

import (
	"os"
	"strconv"
)

// Config holds all configuration for the application.
type Config struct {
	// Server
	ServerPort string

	// PostgreSQL
	PostgresHost     string
	PostgresPort     int
	PostgresUser     string
	PostgresPassword string
	PostgresDB       string

	// Redis
	RedisHost     string
	RedisPort     int
	RedisPassword string
	RedisDB       int

	// RabbitMQ
	RabbitMQURL      string
	RabbitMQExchange string

	// WebSocket
	WSEnabled bool

	// Async Workers
	WorkerCount int
}

// Load reads configuration from environment variables.
func Load() *Config {
	return &Config{
		ServerPort: getEnv("SERVER_PORT", ":8080"),

		PostgresHost:     getEnv("POSTGRES_HOST", "localhost"),
		PostgresPort:     getEnvInt("POSTGRES_PORT", 5432),
		PostgresUser:     getEnv("POSTGRES_USER", "postgres"),
		PostgresPassword: getEnv("POSTGRES_PASSWORD", "postgres"),
		PostgresDB:       getEnv("POSTGRES_DB", "orderbook"),

		RedisHost:     getEnv("REDIS_HOST", "localhost"),
		RedisPort:     getEnvInt("REDIS_PORT", 6379),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       getEnvInt("REDIS_DB", 0),

		RabbitMQURL:      getEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
		RabbitMQExchange: getEnv("RABBITMQ_EXCHANGE", "orderbook.events"),

		WSEnabled: getEnvBool("WS_ENABLED", true),

		WorkerCount: getEnvInt("WORKER_COUNT", 4),
	}
}

// getEnv reads an environment variable with a default value.
func getEnv(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}

// getEnvInt reads an environment variable as int with a default value.
func getEnvInt(key string, defaultValue int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultValue
}

// getEnvBool reads an environment variable as bool with a default value.
func getEnvBool(key string, defaultValue bool) bool {
	if val := os.Getenv(key); val != "" {
		return val == "true" || val == "1"
	}
	return defaultValue
}

// GetPostgresDSN returns the PostgreSQL connection string.
func (c *Config) GetPostgresDSN() string {
	return "host=" + c.PostgresHost +
		" port=" + strconv.Itoa(c.PostgresPort) +
		" user=" + c.PostgresUser +
		" password=" + c.PostgresPassword +
		" dbname=" + c.PostgresDB +
		" sslmode=disable"
}

// GetRedisAddr returns the Redis address.
func (c *Config) GetRedisAddr() string {
	return c.RedisHost + ":" + strconv.Itoa(c.RedisPort)
}
