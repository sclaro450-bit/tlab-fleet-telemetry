package certificates

import "testing"

func clearACMEEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"ACME_DOMAIN", "ACME_EMAIL", "CLOUDFLARE_API_TOKEN", "ACME_STORAGE_PATH"} {
		t.Setenv(name, "")
	}
}

func TestACMEOptionsFromEnvironmentDisabled(t *testing.T) {
	clearACMEEnvironment(t)
	_, requested, err := ACMEOptionsFromEnvironment()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requested {
		t.Fatal("ACME should not be requested without environment variables")
	}
}

func TestACMEOptionsFromEnvironment(t *testing.T) {
	clearACMEEnvironment(t)
	t.Setenv("ACME_DOMAIN", "telemetry.tlabcontrol.com")
	t.Setenv("ACME_EMAIL", "owner@example.com")
	t.Setenv("CLOUDFLARE_API_TOKEN", "secret")

	options, requested, err := ACMEOptionsFromEnvironment()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !requested {
		t.Fatal("ACME should be requested")
	}
	if options.Domain != "telemetry.tlabcontrol.com" {
		t.Fatalf("unexpected domain: %s", options.Domain)
	}
	if options.StoragePath != defaultStoragePath {
		t.Fatalf("unexpected storage path: %s", options.StoragePath)
	}
}

func TestACMEOptionsFromEnvironmentRejectsPartialConfiguration(t *testing.T) {
	clearACMEEnvironment(t)
	t.Setenv("ACME_DOMAIN", "telemetry.tlabcontrol.com")

	_, requested, err := ACMEOptionsFromEnvironment()
	if !requested {
		t.Fatal("ACME should be requested")
	}
	if err == nil || err.Error() != "missing ACME_EMAIL" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestACMEOptionsFromEnvironmentRejectsURL(t *testing.T) {
	clearACMEEnvironment(t)
	t.Setenv("ACME_DOMAIN", "https://telemetry.tlabcontrol.com")
	t.Setenv("ACME_EMAIL", "owner@example.com")
	t.Setenv("CLOUDFLARE_API_TOKEN", "secret")

	_, _, err := ACMEOptionsFromEnvironment()
	if err == nil {
		t.Fatal("expected invalid domain error")
	}
}
