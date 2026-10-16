package integration_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
	"gptibo/storefront/internal/auth"
	"gptibo/storefront/internal/fault"
	"gptibo/storefront/internal/httpapi"
)

func TestTemporaryAdminPasswordOnlyAndRestore(t *testing.T) {
	_, app := fixture(t, true)
	password := "fixture-password-for-tests-only"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		t.Fatal(err)
	}
	cfg := auth.Config{Secret: strings.Repeat("a1", 32), AdminUsername: "fixture-admin", AdminEmail: "admin@example.test", AdminTOTP: testTOTP, TemporaryPasswordLogin: true, TemporaryPasswordOnly: true, AdminPasswordHash: string(hash)}
	a, err := auth.New(app, cfg)
	if err != nil {
		t.Fatal(err)
	}
	h := httpapi.Router(app, "fixture-worker", slog.New(slog.NewJSONHandler(io.Discard, nil)), httpapi.Options{Origin: testOrigin, Auth: a})
	session := object(t, request(h, "GET", "/api/admin/session", nil, nil), 200)
	if session["temporaryPasswordOnly"] != true || session["mailReady"] != false {
		t.Fatal("incorrect login mode")
	}
	object(t, request(h, "GET", "/api/admin/plans", nil, nil), 401)
	for _, wrong := range []auth.VerifyInput{{Username: cfg.AdminUsername, Password: "wrong-password"}, {Username: cfg.AdminUsername}, {Username: "wrong-admin", Password: password}} {
		object(t, request(h, "POST", "/api/admin/login", wrong, nil), 401)
	}
	in := auth.VerifyInput{Username: cfg.AdminUsername, Password: password}
	w := request(h, "POST", "/api/admin/login", in, nil)
	object(t, w, 200)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatal("missing secure session")
	}
	cookie := cookies[0]
	object(t, request(h, "GET", "/api/admin/plans", nil, cookie), 200)
	for _, role := range []string{"partner", "customer"} {
		if _, err = a.Verify(context.Background(), role, in, "other-role"); fault.Status(err) != 503 {
			t.Fatal("password-only leaked to other roles", role, err)
		}
	}
	cfg.TemporaryPasswordOnly = false
	restored, err := auth.New(app, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restored.Require(context.Background(), app.DB, "admin", cookie.Value); fault.Status(err) != 401 {
		t.Fatal("password-only session survived TOTP restoration", err)
	}
	if _, err = restored.Verify(context.Background(), "admin", in, "restored"); fault.Status(err) != 401 {
		t.Fatal("restored mode accepted missing TOTP", err)
	}
	object(t, request(h, "POST", "/api/admin/logout", map[string]any{}, cookie), 200)
	object(t, request(h, "GET", "/api/admin/plans", nil, cookie), 401)
	for i := 0; i < 20; i++ {
		_, _ = a.Verify(context.Background(), "admin", auth.VerifyInput{Password: strings.Repeat("x", 73)}, "limited-password-only")
	}
	if _, err = a.Verify(context.Background(), "admin", in, "limited-password-only"); fault.Status(err) != 429 {
		t.Fatal("password-only mode bypassed throttling", err)
	}
}
