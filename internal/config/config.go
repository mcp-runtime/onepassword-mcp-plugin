// Package config loads and validates the plugin's startup configuration.
// Invalid configuration fails startup with a message that never includes a
// secret value. Secrets are referenced by mounted-file path, never inlined.
package config

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/mcp-runtime/onepassword-mcp-plugin/internal/headers"
	"github.com/mcp-runtime/onepassword-mcp-plugin/internal/secrets"
)

var serviceIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

type file struct {
	Listen struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	} `json:"listen"`
	AllowInsecureHTTP   bool `json:"allow_insecure_http"`
	AuthorizationServer struct {
		Issuer         string  `json:"issuer"`
		TokenEndpoint  string  `json:"token_endpoint"`
		JWKSURI        string  `json:"jwks_uri"`
		TimeoutSeconds float64 `json:"timeout_seconds"`
	} `json:"authorization_server"`
	PublicPathPrefix      string `json:"public_path_prefix"`
	SessionBindingKeyFile string `json:"session_binding_key_file"`
	MappingsFile          string `json:"mappings_file"`
	OnePassword           struct {
		ServiceAccountTokenFile string  `json:"service_account_token_file"`
		IntegrationName         string  `json:"integration_name"`
		TimeoutSeconds          float64 `json:"timeout_seconds"`
	} `json:"onepassword"`
	CredentialCache struct {
		TTLSeconds *float64 `json:"ttl_seconds"`
		MaxEntries int      `json:"max_entries"`
	} `json:"credential_cache"`
	Limits struct {
		MaxRequestBytes            int64   `json:"max_request_bytes"`
		UpstreamConnectTimeoutSecs float64 `json:"upstream_connect_timeout_seconds"`
		UpstreamHeaderTimeoutSecs  float64 `json:"upstream_response_header_timeout_seconds"`
	} `json:"limits"`
	Services []struct {
		ID             string   `json:"id"`
		ResourceURL    string   `json:"resource_url"`
		UpstreamURL    string   `json:"upstream_url"`
		RequiredScopes []string `json:"required_scopes"`
		ResourceClient struct {
			ClientID       string `json:"client_id"`
			KeyID          string `json:"key_id"`
			PrivateKeyFile string `json:"private_key_file"`
		} `json:"resource_client"`
		Headers []headers.Raw `json:"headers"`
	} `json:"services"`
}

// ResourceClient is the plugin's identity at mcp-auth for one service.
type ResourceClient struct {
	ClientID   string
	KeyID      string
	PrivateKey *rsa.PrivateKey
}

// Service is one proxied MCP backend.
type Service struct {
	ID             string
	ResourceURL    string
	ResourcePath   string
	UpstreamURL    string
	RequiredScopes []string
	Client         ResourceClient
	Headers        []headers.Spec
}

// Config is the validated configuration.
type Config struct {
	Listen                string
	Issuer                string
	TokenEndpoint         string
	JWKSURI               string
	AllowInsecureHTTP     bool
	ASTimeout             time.Duration
	PublicPathPrefix      string
	SessionKey            []byte
	MappingsFile          string
	TokenFile             string
	IntegrationName       string
	ProviderTimeout       time.Duration
	CacheTTL              time.Duration
	CacheMaxEntries       int
	MaxRequestBytes       int64
	ConnectTimeout        time.Duration
	ResponseHeaderTimeout time.Duration
	Services              []Service
}

// Load reads and validates the configuration file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("configuration file %q is unreadable", path)
	}
	return Parse(data)
}

func seconds(v, def, low, high float64, name string) (time.Duration, error) {
	if v == 0 {
		v = def
	}
	if v < low || v > high {
		return 0, fmt.Errorf("%s must be between %v and %v seconds", name, low, high)
	}
	return time.Duration(v * float64(time.Second)), nil
}

func checkURL(raw, name string, allowHTTP bool) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Fragment != "" || u.User != nil || u.RawQuery != "" {
		return nil, fmt.Errorf("%s must be an absolute URL without credentials, query or fragment", name)
	}
	if u.Scheme != "https" && !(allowHTTP && u.Scheme == "http") {
		return nil, fmt.Errorf("%s must use https", name)
	}
	return u, nil
}

// Parse validates raw JSON configuration.
func Parse(data []byte) (*Config, error) {
	var f file
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, errors.New("configuration is not valid JSON of the expected shape")
	}
	c := &Config{AllowInsecureHTTP: f.AllowInsecureHTTP, MappingsFile: f.MappingsFile,
		TokenFile: f.OnePassword.ServiceAccountTokenFile, PublicPathPrefix: f.PublicPathPrefix}
	var err error

	issuer, err := checkURL(f.AuthorizationServer.Issuer, "authorization_server.issuer", c.AllowInsecureHTTP)
	if err != nil {
		return nil, err
	}
	c.Issuer = strings.TrimRight(issuer.String(), "/")
	c.TokenEndpoint = f.AuthorizationServer.TokenEndpoint
	if c.TokenEndpoint == "" {
		c.TokenEndpoint = c.Issuer + "/token"
	}
	c.JWKSURI = f.AuthorizationServer.JWKSURI
	if c.JWKSURI == "" {
		c.JWKSURI = c.Issuer + "/.well-known/jwks.json"
	}
	for name, v := range map[string]string{"token_endpoint": c.TokenEndpoint, "jwks_uri": c.JWKSURI} {
		if _, err := checkURL(v, "authorization_server."+name, c.AllowInsecureHTTP); err != nil {
			return nil, err
		}
	}
	if c.ASTimeout, err = seconds(f.AuthorizationServer.TimeoutSeconds, 5, 0.1, 60, "authorization_server.timeout_seconds"); err != nil {
		return nil, err
	}
	if p := c.PublicPathPrefix; p != "" && (!strings.HasPrefix(p, "/") || strings.HasSuffix(p, "/")) {
		return nil, errors.New("public_path_prefix must be empty, start with '/' and not end with '/'")
	}
	key, err := secrets.ReadSecretFile(f.SessionBindingKeyFile, "session binding key")
	if err != nil {
		return nil, err
	}
	if len(key) < 32 {
		return nil, errors.New("session binding key must be at least 32 characters")
	}
	c.SessionKey = []byte(key)
	if c.MappingsFile == "" || c.TokenFile == "" {
		return nil, errors.New("mappings_file and onepassword.service_account_token_file are required")
	}
	// Fail at startup, not at the first request, if the service-account token
	// is missing or empty. The value is discarded here; only the resolver holds it.
	if _, err := secrets.ReadSecretFile(c.TokenFile, "service account token"); err != nil {
		return nil, err
	}
	c.IntegrationName = f.OnePassword.IntegrationName
	if c.IntegrationName == "" {
		c.IntegrationName = "onepassword-mcp-plugin"
	}
	if c.ProviderTimeout, err = seconds(f.OnePassword.TimeoutSeconds, 5, 0.1, 60, "onepassword.timeout_seconds"); err != nil {
		return nil, err
	}
	ttl := 300.0
	if f.CredentialCache.TTLSeconds != nil {
		ttl = *f.CredentialCache.TTLSeconds
	}
	if ttl < 0 || ttl > 3600 {
		return nil, errors.New("credential_cache.ttl_seconds must be between 0 and 3600")
	}
	c.CacheTTL = time.Duration(ttl * float64(time.Second))
	c.CacheMaxEntries = f.CredentialCache.MaxEntries
	if c.CacheMaxEntries == 0 {
		c.CacheMaxEntries = 1024
	}
	if c.CacheMaxEntries < 1 || c.CacheMaxEntries > 100000 {
		return nil, errors.New("credential_cache.max_entries must be between 1 and 100000")
	}
	c.MaxRequestBytes = f.Limits.MaxRequestBytes
	if c.MaxRequestBytes == 0 {
		c.MaxRequestBytes = 4 << 20
	}
	if c.MaxRequestBytes < 1024 || c.MaxRequestBytes > 64<<20 {
		return nil, errors.New("limits.max_request_bytes must be between 1024 and 67108864")
	}
	if c.ConnectTimeout, err = seconds(f.Limits.UpstreamConnectTimeoutSecs, 5, 0.1, 60, "limits.upstream_connect_timeout_seconds"); err != nil {
		return nil, err
	}
	if c.ResponseHeaderTimeout, err = seconds(f.Limits.UpstreamHeaderTimeoutSecs, 60, 1, 600, "limits.upstream_response_header_timeout_seconds"); err != nil {
		return nil, err
	}
	host, port := f.Listen.Host, f.Listen.Port
	if host == "" {
		host = "0.0.0.0"
	}
	if port == 0 {
		port = 8090
	}
	if port < 1 || port > 65535 {
		return nil, errors.New("listen.port is invalid")
	}
	c.Listen = fmt.Sprintf("%s:%d", host, port)

	if len(f.Services) == 0 {
		return nil, errors.New("services must be a non-empty list")
	}
	ids, resources, clients := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for i, s := range f.Services {
		here := fmt.Sprintf("services[%d]", i)
		if !serviceIDRe.MatchString(s.ID) {
			return nil, fmt.Errorf("%s.id must be lowercase letters, digits and dashes", here)
		}
		resource, err := checkURL(s.ResourceURL, here+".resource_url", c.AllowInsecureHTTP)
		if err != nil {
			return nil, err
		}
		upstream, err := checkURL(s.UpstreamURL, here+".upstream_url", c.AllowInsecureHTTP)
		if err != nil {
			return nil, err
		}
		resourceURL := strings.TrimRight(resource.String(), "/")
		if ids[s.ID] || resources[resourceURL] || clients[s.ResourceClient.ClientID] {
			return nil, fmt.Errorf("%s: id, resource_url and resource_client must be unique per service", here)
		}
		ids[s.ID], resources[resourceURL], clients[s.ResourceClient.ClientID] = true, true, true
		for _, scope := range s.RequiredScopes {
			if scope == "" || strings.ContainsAny(scope, " \t\r\n\"\\") {
				return nil, fmt.Errorf("%s.required_scopes has an invalid scope", here)
			}
		}
		if s.ResourceClient.ClientID == "" || s.ResourceClient.KeyID == "" {
			return nil, fmt.Errorf("%s.resource_client requires client_id and key_id", here)
		}
		pemText, err := secrets.ReadSecretFile(s.ResourceClient.PrivateKeyFile, "resource client key")
		if err != nil {
			return nil, err
		}
		priv, err := parseRSAKey(pemText)
		if err != nil {
			return nil, fmt.Errorf("%s.resource_client: %w", here, err)
		}
		specs, err := headers.Parse(s.Headers, here)
		if err != nil {
			return nil, err
		}
		path := strings.TrimRight(resource.Path, "/")
		if path == "" {
			path = "/"
		}
		c.Services = append(c.Services, Service{
			ID: s.ID, ResourceURL: resourceURL, ResourcePath: path, UpstreamURL: upstream.String(),
			RequiredScopes: s.RequiredScopes,
			Client:         ResourceClient{ClientID: s.ResourceClient.ClientID, KeyID: s.ResourceClient.KeyID, PrivateKey: priv},
			Headers:        specs,
		})
	}
	return c, nil
}

func parseRSAKey(text string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(text))
	if block == nil {
		return nil, errors.New("private key is not PEM encoded")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
		return nil, errors.New("private key must be RSA")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, errors.New("private key is not a valid RSA key")
}

// RequiredFields maps each service id to the credential fields its headers read.
func (c *Config) RequiredFields() map[string][]string {
	out := make(map[string][]string, len(c.Services))
	for _, s := range c.Services {
		out[s.ID] = headers.RequiredFields(s.Headers)
	}
	return out
}
