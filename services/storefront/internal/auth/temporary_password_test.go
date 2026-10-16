package auth

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestTemporaryPasswordModeRequiresExplicitCompleteConfiguration(t *testing.T) {
	cfg := Config{Secret: strings.Repeat("a1", 32), AdminUsername: "fixture-admin", AdminEmail: "admin@example.test", AdminTOTP: "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"}
	a, err := New(nil, cfg)
	if err != nil || a.TemporaryPasswordLogin() || a.Ready() {
		t.Fatal("default login mode changed", err)
	}
	cfg.TemporaryPasswordLogin = true
	if _, err = New(nil, cfg); err == nil {
		t.Fatal("missing password hash accepted")
	}
	weak, _ := bcrypt.GenerateFromPassword([]byte("fixture-password-for-tests-only"), bcrypt.MinCost)
	cfg.AdminPasswordHash = string(weak)
	if _, err = New(nil, cfg); err == nil {
		t.Fatal("weak bcrypt cost accepted")
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("fixture-password-for-tests-only"), 12)
	cfg.AdminPasswordHash = string(hash)
	a, err = New(nil, cfg)
	if err != nil || !a.AdminConfigured() || !a.TemporaryPasswordLogin() || a.Ready() {
		t.Fatal("password mode did not remain independent of mail", err)
	}
	cfg.TemporaryPasswordOnly = true
	only, err := New(nil, cfg)
	if err != nil || !only.TemporaryPasswordOnly() || only.version("admin", cfg.AdminUsername, 0) == a.version("admin", cfg.AdminUsername, 0) {
		t.Fatal("password-only mode must have a separate credential version", err)
	}
	cfg.TemporaryPasswordOnly = false
	oldVersion := a.version("admin", cfg.AdminUsername, 0)
	cfg.TemporaryPasswordLogin = false
	restored, err := New(nil, cfg)
	if err != nil || restored.TemporaryPasswordLogin() || oldVersion == restored.version("admin", cfg.AdminUsername, 0) {
		t.Fatal("switching mode must invalidate temporary sessions", err)
	}
	cfg.TemporaryPasswordLogin = true
	cfg.AdminTOTP = ""
	if _, err = New(nil, cfg); err == nil {
		t.Fatal("password mode accepted missing TOTP")
	}
}

func TestPasswordOnlyCannotBypassTemporaryPasswordConfiguration(t *testing.T) {
	if _, err := New(nil, Config{TemporaryPasswordOnly: true}); err == nil {
		t.Fatal("password-only mode accepted without temporary password login")
	}
}
