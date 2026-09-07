package logger

import (
	"errors"
)

var (
	// ErrEmptyEnv is returned when the env value is empty.
	ErrEmptyEnv = errors.New("env is required")
	// ErrInvalidEnv is returned when the env value is not a known environment.
	ErrInvalidEnv = errors.New("invalid env value")
)

// Env is the deployment environment the service runs in.
type Env string

// Known deployment environments.
const (
	EnvLocal Env = "local"
	EnvDev   Env = "dev"
	EnvProd  Env = "prod"
)

// NewEnv validates value and returns the matching Env.
func NewEnv(value string) (Env, error) {
	if value == "" {
		return "", ErrEmptyEnv
	}

	env := Env(value)
	switch env {
	case EnvLocal, EnvDev, EnvProd:
		return env, nil
	default:
		return "", ErrInvalidEnv
	}
}

// String returns the env as a string.
func (e Env) String() string {
	return string(e)
}
