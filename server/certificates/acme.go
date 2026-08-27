package certificates

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strings"

	"github.com/caddyserver/certmagic"
	"github.com/libdns/cloudflare"
)

const defaultStoragePath = "/data/certmagic"

// ACMEOptions contains the settings used to obtain and renew a publicly
// trusted server certificate using the DNS-01 challenge.
type ACMEOptions struct {
	Domain      string
	Email       string
	APIToken    string
	StoragePath string
}

// ACMEOptionsFromEnvironment returns requested=false when no ACME variables
// are set. A partial configuration fails with a clear startup error.
func ACMEOptionsFromEnvironment() (options ACMEOptions, requested bool, err error) {
	options = ACMEOptions{
		Domain:      strings.TrimSpace(os.Getenv("ACME_DOMAIN")),
		Email:       strings.TrimSpace(os.Getenv("ACME_EMAIL")),
		APIToken:    strings.TrimSpace(os.Getenv("CLOUDFLARE_API_TOKEN")),
		StoragePath: strings.TrimSpace(os.Getenv("ACME_STORAGE_PATH")),
	}
	requested = options.Domain != "" || options.Email != "" || options.APIToken != "" || options.StoragePath != ""
	if !requested {
		return ACMEOptions{}, false, nil
	}
	if options.Domain == "" {
		return ACMEOptions{}, true, errors.New("missing ACME_DOMAIN")
	}
	if strings.ContainsAny(options.Domain, "/:") || !strings.Contains(options.Domain, ".") {
		return ACMEOptions{}, true, fmt.Errorf("invalid ACME_DOMAIN %q: use a hostname without a scheme or port", options.Domain)
	}
	if options.Email == "" {
		return ACMEOptions{}, true, errors.New("missing ACME_EMAIL")
	}
	address, parseErr := mail.ParseAddress(options.Email)
	if parseErr != nil || address.Address != options.Email {
		return ACMEOptions{}, true, fmt.Errorf("invalid ACME_EMAIL %q", options.Email)
	}
	if options.APIToken == "" {
		return ACMEOptions{}, true, errors.New("missing CLOUDFLARE_API_TOKEN")
	}
	if options.StoragePath == "" {
		options.StoragePath = defaultStoragePath
	}
	if !filepath.IsAbs(options.StoragePath) {
		return ACMEOptions{}, true, errors.New("ACME_STORAGE_PATH must be an absolute path")
	}
	return options, true, nil
}

// Manage obtains the configured certificate before startup and keeps it
// managed for renewal. The returned callback preserves Tesla client mTLS.
func (options ACMEOptions) Manage(ctx context.Context) (*certmagic.Config, error) {
	if err := os.MkdirAll(options.StoragePath, 0700); err != nil {
		return nil, fmt.Errorf("create ACME storage: %w", err)
	}

	certmagic.Default.Storage = &certmagic.FileStorage{Path: options.StoragePath}
	certmagic.Default.DefaultServerName = options.Domain
	certmagic.DefaultACME.Email = options.Email
	certmagic.DefaultACME.Agreed = true
	certmagic.DefaultACME.DisableHTTPChallenge = true
	certmagic.DefaultACME.DisableTLSALPNChallenge = true
	certmagic.DefaultACME.DNS01Solver = &certmagic.DNS01Solver{
		DNSManager: certmagic.DNSManager{
			DNSProvider: &cloudflare.Provider{APIToken: options.APIToken},
		},
	}

	manager := certmagic.NewDefault()
	if err := manager.ManageSync(ctx, []string{options.Domain}); err != nil {
		return nil, fmt.Errorf("obtain certificate for %s using Cloudflare DNS-01: %w", options.Domain, err)
	}
	return manager, nil
}
