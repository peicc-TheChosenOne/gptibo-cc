package integration_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gptibo/storefront/internal/auth"
	"gptibo/storefront/internal/fault"
	"gptibo/storefront/internal/httpapi"
	"gptibo/storefront/internal/security"
)

func TestTemporaryAdminPasswordLoginAndRollback(t *testing.T) {
	_, app := fixture(t, true)
	password := "fixture-password-for-tests-only"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		t.Fatal(err)
	}
	cfg := auth.Config{Secret: strings.Repeat("a1", 32), AdminUsername: "fixture-admin", AdminEmail: "admin@example.test", AdminTOTP: testTOTP, TemporaryPasswordLogin: true, AdminPasswordHash: string(hash)}
	a, err := auth.New(app, cfg)
	if err != nil {
		t.Fatal(err)
	}
	h := httpapi.Router(app, "fixture-worker", slog.New(slog.NewJSONHandler(io.Discard, nil)), httpapi.Options{Origin: testOrigin, Auth: a})
	session := object(t, request(h, "GET", "/api/admin/session", nil, nil), 200)
	if session["temporaryPasswordLogin"] != true || session["mailReady"] != false {
		t.Fatal("wrong temporary mode advertised")
	}
	in := auth.VerifyInput{Username: "fixture-admin", Password: password, Code: security.TOTP(testTOTP, time.Now().Unix()/30)}
	for _, wrong := range []auth.VerifyInput{
		{Username: in.Username, Password: "wrong-password", Code: in.Code},
		{Username: "wrong-admin", Password: password, Code: in.Code},
		{Username: in.Username, Password: password},
	} {
		object(t, request(h, "POST", "/api/admin/login", wrong, nil), 401)
	}
	for _, role := range []string{"partner", "customer"} {
		if _, err = a.Verify(context.Background(), role, in, "other-role"); fault.Status(err) != 503 {
			t.Fatal("temporary administrator mode leaked to another role", role, err)
		}
	}
	var wg sync.WaitGroup
	results := make(chan *http.Cookie, 2)
	statuses := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := request(h, "POST", "/api/admin/login", in, nil)
			statuses <- w.Code
			if w.Code == 200 {
				results <- w.Result().Cookies()[0]
			}
		}()
	}
	wg.Wait()
	close(statuses)
	close(results)
	success := 0
	for status := range statuses {
		if status == 200 {
			success++
		} else if status != 401 {
			t.Fatal("unexpected concurrent login status", status)
		}
	}
	if success != 1 {
		t.Fatal("TOTP replay protection failed", success)
	}
	cookie := <-results
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Name != "__Host-sf_admin" {
		t.Fatal("unsafe temporary session cookie")
	}
	object(t, request(h, "GET", "/api/admin/plans", nil, cookie), 200)
	rotatedHash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AdminPasswordHash = string(rotatedHash)
	rotated, err := auth.New(app, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = rotated.Require(context.Background(), app.DB, "admin", cookie.Value); fault.Status(err) != 401 {
		t.Fatal("password change did not revoke session", err)
	}
	cfg.AdminPasswordHash = string(hash)
	cfg.TemporaryPasswordLogin = false
	restored, err := auth.New(app, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restored.Require(context.Background(), app.DB, "admin", cookie.Value); fault.Status(err) != 401 {
		t.Fatal("temporary session survived rollback", err)
	}
	if _, err = restored.Verify(context.Background(), "admin", in, "default-mode"); fault.Status(err) != 503 {
		t.Fatal("password accepted when temporary flag disabled", err)
	}
	object(t, request(h, "POST", "/api/admin/logout", map[string]any{}, cookie), 200)
	object(t, request(h, "GET", "/api/admin/plans", nil, cookie), 401)
}

func TestTemporaryAdminPasswordLoginRateLimit(t *testing.T) {
	_, app := fixture(t, true)
	hash, err := bcrypt.GenerateFromPassword([]byte("fixture-password-for-tests-only"), 12)
	if err != nil {
		t.Fatal(err)
	}
	a, err := auth.New(app, auth.Config{Secret: strings.Repeat("a1", 32), AdminUsername: "fixture-admin", AdminEmail: "admin@example.test", AdminTOTP: testTOTP, TemporaryPasswordLogin: true, AdminPasswordHash: string(hash)})
	if err != nil {
		t.Fatal(err)
	}
	in := auth.VerifyInput{Username: "fixture-admin", Password: strings.Repeat("x", 73), Code: "000000"}
	for i := 0; i < 20; i++ {
		if _, err = a.Verify(context.Background(), "admin", in, "limited-client"); fault.Status(err) != 401 {
			t.Fatal("unexpected failure status", err)
		}
	}
	if _, err = a.Verify(context.Background(), "admin", in, "limited-client"); fault.Status(err) != 429 {
		t.Fatal("password login rate limit not enforced", err)
	}
}
