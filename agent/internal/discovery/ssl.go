package discovery

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"time"

	"github.com/frix-me/pulse/agent/internal/model"
)

// SSLDetector reads TLS certificate files referenced by reverse proxies and
// reports validity and expiry. Read-only: it opens certificate files (never
// private keys) and parses them locally.
type SSLDetector struct{}

func (SSLDetector) ID() string      { return "ssl" }
func (SSLDetector) Name() string    { return "SSL Detector" }
func (SSLDetector) Version() string { return "1.0" }

var certGlobs = []string{
	"/etc/letsencrypt/live/*/fullchain.pem",
	"/etc/nginx/ssl/*.crt",
	"/etc/ssl/certs/*.pem",
}

func (SSLDetector) Available(context.Context) model.Availability {
	return model.Availability{Available: true}
}

func (SSLDetector) Detect(context.Context) ([]model.Resource, error) {
	var out []model.Resource
	seen := map[string]bool{}
	for _, g := range certGlobs {
		matches, _ := filepath.Glob(g)
		for _, path := range matches {
			cert := firstCert(path)
			if cert == nil {
				continue
			}
			subject := cert.Subject.CommonName
			if subject == "" && len(cert.DNSNames) > 0 {
				subject = cert.DNSNames[0]
			}
			if subject == "" || seen[subject] {
				continue
			}
			seen[subject] = true

			daysLeft := int(time.Until(cert.NotAfter).Hours() / 24)
			health := model.StatusHealthy
			if daysLeft < 0 {
				health = model.StatusDown
			} else if daysLeft < 7 {
				health = model.StatusDown
			} else if daysLeft < 30 {
				health = model.StatusDegraded
			}
			out = append(out, model.Resource{
				Type:       "tls_certificate",
				ID:         "tls:" + subject,
				Name:       subject,
				Health:     health,
				DetectedBy: "ssl",
				DetectedAt: time.Now().UTC(),
				Attributes: map[string]any{
					"common_name": subject,
					"dns_names":   cert.DNSNames,
					"issuer":      cert.Issuer.CommonName,
					"not_after":   cert.NotAfter.UTC().Format(time.RFC3339),
					"days_left":   daysLeft,
					"cert_file":   path,
				},
			})
		}
	}
	return out, nil
}

func (SSLDetector) Health(context.Context) model.HealthReport {
	return model.HealthReport{Status: model.StatusHealthy}
}

// firstCert parses the first certificate from a PEM file (the leaf/fullchain
// head). Returns nil on any error.
func firstCert(path string) *x509.Certificate {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	for {
		block, rest := pem.Decode(b)
		if block == nil {
			return nil
		}
		if block.Type == "CERTIFICATE" {
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil
			}
			return cert
		}
		b = rest
	}
}
