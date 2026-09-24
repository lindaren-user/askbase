package config

import "testing"

func TestApplyDefaultsDoesNotCreateProductionAuthSecret(t *testing.T) {
	cfg := Config{Env: EnvProd}
	cfg.applyDefaults()
	if cfg.Auth.Secret != "" {
		t.Fatalf("production auth secret = %q, want empty", cfg.Auth.Secret)
	}
}

func TestApplyDefaultsCreatesDevelopmentAuthSecret(t *testing.T) {
	cfg := Config{Env: EnvDev}
	cfg.applyDefaults()
	if cfg.Auth.Secret == "" {
		t.Fatal("development auth secret is empty")
	}
}
