package integration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"ehr-backend/internal/server"
)

const nilUUID = "00000000-0000-4000-8000-000000000000"

// adminUser signs up a user and grants practice_administrator in SQL (the
// operator bootstrap does the same through cmd/bootstrap-admin).
func adminUser(t testing.TB) client { return newUser(t, "practice_administrator") }

func fillPath(pattern string) (method, path string) {
	method, path, _ = strings.Cut(pattern, " ")

	for strings.Contains(path, "{") {
		start := strings.Index(path, "{")
		end := strings.Index(path, "}")
		path = path[:start] + nilUUID + path[end+1:]
	}

	return method, path
}

func status(t testing.TB, c client, method, path string) int {
	t.Helper()

	var body any
	if method == "POST" || method == "PUT" || method == "PATCH" {
		body = map[string]any{}
	}

	return c.do(t, method, path, body).Status
}

// Every registered route is protected the way its access class says, and no
// handler crashes on an empty request.
func TestRouteSecurityMatrix(t *testing.T) {
	needDB(t)

	noRole := newUser(t)
	intern := newUser(t, "intern_assistant_associate")

	if len(router.Routes) < 100 {
		t.Fatalf("route registry looks incomplete: %d routes", len(router.Routes))
	}

	for _, rt := range router.Routes {
		method, path := fillPath(rt.Pattern)

		switch rt.Access {
		case server.AccessPublic:
			continue

		case server.AccessSession:
			if got := status(t, client{}, method, path); got != http.StatusUnauthorized {
				t.Errorf("%s anonymous: %d, want 401", rt.Pattern, got)
			}
			if got := status(t, noRole, method, path); got == http.StatusUnauthorized || got == http.StatusForbidden {
				t.Errorf("%s with a role-less session: %d", rt.Pattern, got)
			}

		case server.AccessAuth:
			if got := status(t, client{}, method, path); got != http.StatusUnauthorized {
				t.Errorf("%s anonymous: %d, want 401", rt.Pattern, got)
			}
			if got := status(t, noRole, method, path); got != http.StatusForbidden {
				t.Errorf("%s with no role: %d, want 403 (self sign-up must not see practice data)", rt.Pattern, got)
			}
			if got := status(t, intern, method, path); got == http.StatusUnauthorized || got == http.StatusForbidden || got >= 500 {
				t.Errorf("%s as staff: %d", rt.Pattern, got)
			}

		case server.AccessRoles, server.AccessSelfOrRoles:
			if len(rt.Roles) == 0 {
				t.Errorf("%s requires roles but lists none", rt.Pattern)
				continue
			}

			if got := status(t, client{}, method, path); got != http.StatusUnauthorized {
				t.Errorf("%s anonymous: %d, want 401", rt.Pattern, got)
			}
			if got := status(t, noRole, method, path); got != http.StatusForbidden {
				t.Errorf("%s with no role: %d, want 403", rt.Pattern, got)
			}

			// A role that is not on the list must be refused.
			outsider := intern
			if containsString(rt.Roles, "intern_assistant_associate") {
				t.Fatalf("test assumption broken: %s lists the intern role", rt.Pattern)
			}
			if got := status(t, outsider, method, path); got != http.StatusForbidden {
				t.Errorf("%s with an unrelated role: %d, want 403", rt.Pattern, got)
			}

			// A listed role gets through to the handler (which may then 400/404).
			allowed := newUser(t, rt.Roles[0])
			if got := status(t, allowed, method, path); got == http.StatusUnauthorized || got == http.StatusForbidden || got >= 500 {
				t.Errorf("%s with %s: %d", rt.Pattern, rt.Roles[0], got)
			}

		default:
			t.Errorf("%s has unknown access class %q", rt.Pattern, rt.Access)
		}
	}
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}

	return false
}

// A mutation must deliberately name its roles. The only bare-authentication
// POST is a read-only price calculation.
func TestEveryMutationNamesItsRoles(t *testing.T) {
	needDB(t)

	allowedWithoutRoles := map[string]bool{
		"POST /api/billing/rate-preview": true, // calculates a price; stores nothing
		"POST /api/auth/signup":          true,
		"POST /api/auth/login":           true,
	}

	for _, rt := range router.Routes {
		if strings.HasPrefix(rt.Pattern, "GET ") {
			continue
		}

		if rt.Access == server.AccessRoles || rt.Access == server.AccessSelfOrRoles {
			continue
		}

		if !allowedWithoutRoles[rt.Pattern] {
			t.Errorf("%s is a mutation protected only by %q; give it explicit roles", rt.Pattern, rt.Access)
		}
	}
}

func TestOnlyAdministratorsAssignRoles(t *testing.T) {
	needDB(t)

	// An ordinary user - even a biller - cannot grant themselves roles.
	biller := newUser(t, "practice_biller")
	res := biller.put(t, "/api/users/"+biller.userID+"/roles", map[string]any{"roles": []string{"practice_administrator"}})
	if res.Status != http.StatusForbidden {
		t.Fatalf("self-escalation: %d %s", res.Status, string(res.Raw))
	}

	// A self sign-up with no roles cannot either.
	stranger := newUser(t)
	if res := stranger.put(t, "/api/users/"+stranger.userID+"/roles", map[string]any{"roles": []string{"practice_administrator"}}); res.Status != http.StatusForbidden {
		t.Fatalf("self-escalation by a new account: %d", res.Status)
	}

	var count int
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM user_roles ur JOIN roles r ON r.id = ur.role_id
		WHERE ur.user_id IN ($1, $2) AND r.key = 'practice_administrator'`, biller.userID, stranger.userID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("%d accounts escalated themselves", count)
	}

	// An administrator can, and the change is visible.
	admin := adminUser(t)
	admin.put(t, "/api/users/"+stranger.userID+"/roles", map[string]any{"roles": []string{"practice_scheduler"}}).mustStatus(t, http.StatusOK)

	roles := admin.get(t, "/api/users/"+stranger.userID+"/roles").mustStatus(t, http.StatusOK)
	if roles.Len("") != 1 && roles.Str("0.key") != "practice_scheduler" {
		t.Fatalf("roles after assignment: %s", string(roles.Raw))
	}

	// Invalid roles, bad ids and oversized lists are rejected.
	admin.put(t, "/api/users/"+stranger.userID+"/roles", map[string]any{"roles": []string{"god_mode"}}).mustStatus(t, http.StatusBadRequest)
	admin.put(t, "/api/users/not-a-uuid/roles", map[string]any{"roles": []string{}}).mustStatus(t, http.StatusNotFound)
	admin.put(t, "/api/users/"+nilUUID+"/roles", map[string]any{"roles": []string{"clinician"}}).mustStatus(t, http.StatusNotFound)

	tooMany := make([]string, 25)
	for i := range tooMany {
		tooMany[i] = "clinician"
	}
	admin.put(t, "/api/users/"+stranger.userID+"/roles", map[string]any{"roles": tooMany}).mustStatus(t, http.StatusBadRequest)
}

func TestLastAdministratorCannotBeRemoved(t *testing.T) {
	needDB(t)

	// Make this administrator the only one (other tests created others).
	if _, err := pool.Exec(context.Background(), `DELETE FROM user_roles WHERE role_id = (SELECT id FROM roles WHERE key = 'practice_administrator')`); err != nil {
		t.Fatal(err)
	}

	only := adminUser(t)

	res := only.put(t, "/api/users/"+only.userID+"/roles", map[string]any{"roles": []string{"practice_biller"}})
	if res.Status != http.StatusConflict {
		t.Fatalf("removing the last administrator: %d %s", res.Status, string(res.Raw))
	}

	// With a second administrator the first may step down.
	second := adminUser(t)
	only.put(t, "/api/users/"+only.userID+"/roles", map[string]any{"roles": []string{"practice_biller"}}).mustStatus(t, http.StatusOK)

	// ...and the second is now the last one.
	if res := second.put(t, "/api/users/"+second.userID+"/roles", map[string]any{"roles": []string{}}); res.Status != http.StatusConflict {
		t.Fatalf("demoting the new last administrator: %d", res.Status)
	}
}

func TestUserAccountRules(t *testing.T) {
	needDB(t)

	admin := adminUser(t)
	other := newUser(t, "practice_scheduler")
	me := newUser(t, "practice_biller")

	profile := func(email string) map[string]any {
		return map[string]any{"first_name": "Changed", "last_name": "Name", "username": unique("name"), "email": email}
	}

	// Creating accounts is administrator-only.
	create := map[string]any{"first_name": "New", "last_name": "Hire", "username": unique("hire"), "email": unique("hire") + "@example.test", "password": "long-enough-pass"}
	if res := me.post(t, "/api/users", create); res.Status != http.StatusForbidden {
		t.Fatalf("creating a user as a biller: %d", res.Status)
	}
	admin.post(t, "/api/users", create).mustStatus(t, http.StatusCreated)

	// You can edit yourself but not a colleague.
	me.put(t, "/api/users/"+me.userID, profile(unique("me")+"@example.test")).mustStatus(t, http.StatusOK)
	if res := me.put(t, "/api/users/"+other.userID, profile(unique("x")+"@example.test")); res.Status != http.StatusForbidden {
		t.Fatalf("editing someone else: %d", res.Status)
	}
	admin.put(t, "/api/users/"+other.userID, profile(unique("y")+"@example.test")).mustStatus(t, http.StatusOK)

	// Personal records are readable by their owner and administrators only.
	me.get(t, "/api/users/"+me.userID).mustStatus(t, http.StatusOK)
	if res := me.get(t, "/api/users/"+other.userID); res.Status != http.StatusForbidden {
		t.Fatalf("reading a colleague's record: %d", res.Status)
	}
	admin.get(t, "/api/users/"+other.userID).mustStatus(t, http.StatusOK)

	// Staff can still pick colleagues in selectors.
	me.get(t, "/api/users").mustStatus(t, http.StatusOK)
	me.get(t, "/api/clinicians").mustStatus(t, http.StatusOK)

	// Patients are maintained only by clinical and scheduling staff.
	if res := me.put(t, "/api/patients/"+nilUUID, map[string]any{"last_name": "X"}); res.Status != http.StatusForbidden {
		t.Fatalf("a biller editing a patient: %d", res.Status)
	}
}

func TestSignedUpAccountSeesNoPracticeData(t *testing.T) {
	needDB(t)

	stranger := newUser(t)

	for _, path := range []string{"/api/patients", "/api/payers", "/api/claims", "/api/billing/dashboard", "/api/insurance-payments", "/api/users"} {
		if res := stranger.get(t, path); res.Status != http.StatusForbidden {
			t.Errorf("%s as a role-less account: %d, want 403", path, res.Status)
		}
	}

	// It can still see who it is.
	stranger.get(t, "/api/auth/me").mustStatus(t, http.StatusOK)
}

func TestTokenValidation(t *testing.T) {
	needDB(t)

	user := newUser(t, "practice_biller")
	secret := []byte(testJWTSecret)

	sign := func(method jwt.SigningMethod, claims jwt.MapClaims, key any) string {
		token, err := jwt.NewWithClaims(method, claims).SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}

	valid := jwt.MapClaims{"sub": user.userID, "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix()}

	cases := map[string]string{
		"expired":        sign(jwt.SigningMethodHS256, jwt.MapClaims{"sub": user.userID, "exp": time.Now().Add(-time.Hour).Unix()}, secret),
		"no expiry":      sign(jwt.SigningMethodHS256, jwt.MapClaims{"sub": user.userID}, secret),
		"wrong secret":   sign(jwt.SigningMethodHS256, valid, []byte("another-secret-another-secret-another")),
		"other user":     sign(jwt.SigningMethodHS256, jwt.MapClaims{"sub": nilUUID, "exp": time.Now().Add(time.Hour).Unix()}, secret),
		"garbage":        "not.a.token",
		"alg none":       base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`)) + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"`+user.userID+`","exp":9999999999}`)) + ".",
		"different alg":  sign(jwt.SigningMethodHS512, valid, secret),
		"no subject":     sign(jwt.SigningMethodHS256, jwt.MapClaims{"exp": time.Now().Add(time.Hour).Unix()}, secret),
		"numeric sub":    sign(jwt.SigningMethodHS256, jwt.MapClaims{"sub": 123, "exp": time.Now().Add(time.Hour).Unix()}, secret),
		"empty bearer":   "",
		"sql in subject": sign(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "' OR 1=1 --", "exp": time.Now().Add(time.Hour).Unix()}, secret),
	}

	for name, token := range cases {
		if res := (client{token: token}).get(t, "/api/payers"); res.Status != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", name, res.Status)
		}
	}

	// The valid one works.
	(client{token: sign(jwt.SigningMethodHS256, valid, secret)}).get(t, "/api/payers").mustStatus(t, http.StatusOK)

	// A deactivated account loses access immediately, token and all.
	if _, err := pool.Exec(context.Background(), `UPDATE users SET is_active = FALSE WHERE id = $1`, user.userID); err != nil {
		t.Fatal(err)
	}
	if res := user.get(t, "/api/payers"); res.Status != http.StatusUnauthorized {
		t.Fatalf("deactivated account: %d", res.Status)
	}
}

func TestLoginThrottleAndMessages(t *testing.T) {
	needDB(t)

	name := unique("throttle")
	(client{}).post(t, "/api/auth/signup", map[string]string{
		"first_name": "T", "last_name": "User", "username": name, "email": name + "@example.test", "password": "correct-horse-battery",
	}).mustStatus(t, http.StatusCreated)

	wrong := map[string]string{"email": name + "@example.test", "password": "wrong-password"}

	for i := 0; i < 8; i++ {
		res := (client{}).post(t, "/api/auth/login", wrong)
		if res.Status != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i+1, res.Status)
		}
		// The same message for a wrong password and an unknown email.
		if res.Error() != "invalid email or password" {
			t.Fatalf("login failure leaks detail: %q", res.Error())
		}
	}

	// Even the right password is refused while locked out.
	if res := (client{}).post(t, "/api/auth/login", map[string]string{"email": name + "@example.test", "password": "correct-horse-battery"}); res.Status != http.StatusTooManyRequests {
		t.Fatalf("after 8 failures: %d", res.Status)
	}

	// Other accounts are unaffected.
	other := unique("fine")
	(client{}).post(t, "/api/auth/signup", map[string]string{
		"first_name": "F", "last_name": "User", "username": other, "email": other + "@example.test", "password": "correct-horse-battery",
	}).mustStatus(t, http.StatusCreated)
	(client{}).post(t, "/api/auth/login", map[string]string{"email": other + "@example.test", "password": "correct-horse-battery"}).mustStatus(t, http.StatusOK)

	// Unknown emails look identical to wrong passwords.
	res := (client{}).post(t, "/api/auth/login", map[string]string{"email": "nobody-" + name + "@example.test", "password": "whatever-1"})
	if res.Status != http.StatusUnauthorized || res.Error() != "invalid email or password" {
		t.Fatalf("unknown email: %d %q", res.Status, res.Error())
	}
}

func TestRequestLimitsAndHeaders(t *testing.T) {
	needDB(t)

	biller := newUser(t, "practice_biller")

	// A huge body is refused instead of read into memory.
	huge := strings.Repeat("a", 2<<20)
	res := biller.post(t, "/api/service-codes", map[string]any{"code": "BIG", "description": huge})
	if res.Status != http.StatusBadRequest && res.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body: %d", res.Status)
	}

	// API responses are never cacheable and sniffing is off.
	req, _ := http.NewRequest("GET", baseURL+"/api/payers", nil)
	req.Header.Set("Authorization", "Bearer "+biller.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers: %v", resp.Header)
	}

	// Errors do not leak SQL or internals.
	bad := biller.get(t, "/api/claims?status="+"%27%3B%20DROP%20TABLE%20claims%3B--")
	if bad.Status != http.StatusBadRequest || strings.Contains(strings.ToLower(string(bad.Raw)), "sql") || strings.Contains(string(bad.Raw), "pgx") {
		t.Fatalf("injection attempt: %d %s", bad.Status, string(bad.Raw))
	}
	var parsed map[string]any
	_ = json.Unmarshal(bad.Raw, &parsed)
	if _, ok := parsed["error"]; !ok {
		t.Fatalf("error shape: %s", string(bad.Raw))
	}
}

// docs/ROUTE_SECURITY_MATRIX.md is generated; it must match the router.
func TestRouteMatrixDocumentIsCurrent(t *testing.T) {
	needDB(t)

	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "ROUTE_SECURITY_MATRIX.md"))
	if err != nil {
		t.Fatal(err)
	}

	normalize := func(s string) string {
		s = strings.TrimPrefix(s, bomMark)
		s = strings.ReplaceAll(s, "\r\n", "\n")

		return strings.TrimSpace(s)
	}

	if normalize(string(raw)) != normalize(router.Markdown()) {
		t.Fatal("docs/ROUTE_SECURITY_MATRIX.md is out of date: run `go run ./cmd/route-matrix > ../docs/ROUTE_SECURITY_MATRIX.md` from backend/")
	}
}

// bomMark is the UTF-8 byte order mark (some editors / shells add one).
const bomMark = "\xef\xbb\xbf"
