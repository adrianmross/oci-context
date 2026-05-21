package daemon

import (
	"testing"
	"time"

	"github.com/adrianmross/oci-context/pkg/config"
)

func TestBuildValidateOCIArgsOmitsCompartmentFlag(t *testing.T) {
	ctx := config.Context{
		Profile:         "DEFAULT",
		AuthMethod:      config.AuthMethodSecurityToken,
		TenancyOCID:     "ocid1.tenancy.oc1..aaaa",
		CompartmentOCID: "ocid1.compartment.oc1..bbbb",
		Region:          "us-ashburn-1",
	}
	args := buildValidateOCIArgs(ctx, "/Users/test/.oci/config")

	contains := func(flag string) bool {
		for _, arg := range args {
			if arg == flag {
				return true
			}
		}
		return false
	}

	if contains("--compartment-id") || contains("-c") {
		t.Fatalf("validate args must not contain compartment flag: %v", args)
	}
	if !contains("--tenancy-id") || !contains("--profile") || !contains("--auth") || !contains("--region") {
		t.Fatalf("expected tenancy/profile/auth/region flags in validate args: %v", args)
	}
}

func TestBuildRefreshOCIArgs(t *testing.T) {
	ctx := config.Context{
		Profile:    "DEFAULT",
		Region:     "us-phoenix-1",
		AuthMethod: config.AuthMethodSecurityToken,
	}
	args := buildRefreshOCIArgs(ctx, "/Users/test/.oci/config")
	joined := ""
	for _, a := range args {
		joined += a + " "
	}
	for _, want := range []string{"session", "refresh", "--profile", "DEFAULT", "--config-file", "/Users/test/.oci/config", "--region", "us-phoenix-1"} {
		if !containsArg(args, want) {
			t.Fatalf("expected refresh args to contain %q, got %v", want, args)
		}
	}
	_ = joined
}

func TestFindHomeRegion(t *testing.T) {
	payload := []byte(`{"data":[{"is-home-region":false,"region-key":"PHX","region-name":"us-phoenix-1","status":"READY"},{"is-home-region":true,"region-key":"IAD","region-name":"us-ashburn-1","status":"READY"}]}`)
	region, err := findHomeRegion(payload)
	if err != nil {
		t.Fatalf("expected home region, got error %v", err)
	}
	if region.RegionName != "us-ashburn-1" || region.RegionKey != "IAD" || !region.IsHomeRegion {
		t.Fatalf("unexpected home region parsed: %+v", region)
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func TestBackoffDurationGrowthAndCap(t *testing.T) {
	if got := backoffDuration(1); got != backoffBase {
		t.Fatalf("expected first backoff %s, got %s", backoffBase, got)
	}
	if got := backoffDuration(2); got != backoffBase*2 {
		t.Fatalf("expected second backoff %s, got %s", backoffBase*2, got)
	}
	if got := backoffDuration(100); got != backoffMax {
		t.Fatalf("expected capped backoff %s, got %s", backoffMax, got)
	}
}

func TestAllowAttemptBlockedAfterFailure(t *testing.T) {
	svc := &Service{backoff: make(map[string]backoffState)}
	svc.recordFailure("ctx", "refresh", "boom")
	ok, _ := svc.allowAttempt("ctx", "refresh")
	if ok {
		t.Fatalf("expected allowAttempt to be false during backoff window")
	}
	// force expiration and ensure allowAttempt returns true
	svc.backoffMu.Lock()
	st := svc.backoff["ctx:refresh"]
	st.NextTry = time.Now().Add(-time.Second)
	svc.backoff["ctx:refresh"] = st
	svc.backoffMu.Unlock()
	ok, _ = svc.allowAttempt("ctx", "refresh")
	if !ok {
		t.Fatalf("expected allowAttempt to be true after backoff window")
	}
}

func TestMonitoredContextNamesFallbackAndDedup(t *testing.T) {
	cfg := config.Config{CurrentContext: "cur"}
	got := monitoredContextNames(cfg)
	if len(got) != 1 || got[0] != "cur" {
		t.Fatalf("expected fallback to current context, got %v", got)
	}

	cfg.Options.DaemonContexts = []string{"a", " ", "b", "a", "b", "c"}
	got = monitoredContextNames(cfg)
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestAuthStatusReadinessAllowsFailedRefreshWhenValidateOK(t *testing.T) {
	got := toAuthStatus(authStatusState{
		ContextName:      "dev",
		AuthMethod:       config.AuthMethodSecurityToken,
		LastValidatedAt:  time.Now(),
		LastValidateOK:   true,
		LastRefreshedAt:  time.Now(),
		LastRefreshOK:    false,
		LastError:        "refresh failed",
		Mode:             "managed-security-token",
		HomeRegionName:   "us-ashburn-1",
		HomeRegionKey:    "IAD",
		HomeRegionStatus: "READY",
	})

	if !got.Ready || got.ActionRequired || got.Action != "none" {
		t.Fatalf("expected validate-ok status to be ready without action, got %+v", got)
	}
	if got.Severity != "warning" {
		t.Fatalf("expected refresh failure to downgrade severity to warning, got %+v", got)
	}
}

func TestAuthStatusReadinessSecurityTokenFailureRequiresLogin(t *testing.T) {
	got := toAuthStatus(authStatusState{
		ContextName:     "dev",
		AuthMethod:      config.AuthMethodSecurityToken,
		LastValidatedAt: time.Now(),
		LastValidateOK:  false,
		LastError:       "expired token",
		Mode:            "managed-security-token",
	})

	if got.Ready || !got.ActionRequired || got.Action != "login" || got.Severity != "error" {
		t.Fatalf("expected security-token validation failure to require login, got %+v", got)
	}
}

func TestFinalizeAuthStatusBackfillsOlderDaemonStatus(t *testing.T) {
	got := FinalizeAuthStatus(AuthStatus{
		ContextName:     "dev",
		AuthMethod:      config.AuthMethodSecurityToken,
		LastValidatedAt: "2026-05-21T08:00:00Z",
		LastValidateOK:  true,
		LastRefreshedAt: "2026-05-21T07:55:00Z",
		LastRefreshOK:   false,
		Mode:            "managed-security-token",
	})

	if !got.Ready || got.ActionRequired || got.Action != "none" || got.Severity != "warning" {
		t.Fatalf("expected older validate-ok status to be finalized as ready warning, got %+v", got)
	}
}
