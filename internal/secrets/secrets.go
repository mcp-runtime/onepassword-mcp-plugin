// Package secrets resolves complete op:// references. The 1Password SDK is
// imported only in this package.
package secrets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/1password/onepassword-sdk-go"
)

var (
	// ErrProviderUnavailable: the provider could not be reached, authenticated
	// with, or answered too slowly.
	ErrProviderUnavailable = errors.New("credential provider unavailable")
	// ErrFieldUnavailable: a mapped field could not be read.
	ErrFieldUnavailable = errors.New("credential field unavailable")
)

// Resolver resolves {field: reference} into {field: value}. Implementations
// return only the sentinel errors above; provider messages are never
// propagated because they can quote references or vault contents.
type Resolver interface {
	Resolve(ctx context.Context, refs map[string]string) (map[string]string, error)
	Ready() bool
}

// ReadSecretFile reads a mounted secret, trimming whitespace.
func ReadSecretFile(path, what string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%s file %q is not readable", what, path)
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", fmt.Errorf("%s file %q is empty", what, path)
	}
	return value, nil
}

type secretsAPI interface {
	Resolve(ctx context.Context, reference string) (string, error)
}

type clientFactory func(ctx context.Context, token, integration, version string) (secretsAPI, error)

func sdkFactory(ctx context.Context, token, integration, version string) (secretsAPI, error) {
	client, err := onepassword.NewClient(ctx,
		onepassword.WithServiceAccountToken(token),
		onepassword.WithIntegrationInfo(integration, version))
	if err != nil {
		return nil, err
	}
	return client.Secrets(), nil
}

// OnePassword resolves references with a read-only service account. The token
// is read once from a mounted file, held only in this value, and never logged,
// printed or returned.
type OnePassword struct {
	token       string
	integration string
	version     string
	timeout     time.Duration
	factory     clientFactory

	mu     sync.Mutex
	client secretsAPI
}

// NewOnePassword reads the service-account token and prepares the resolver.
func NewOnePassword(tokenFile, integration, version string, timeout time.Duration) (*OnePassword, error) {
	token, err := ReadSecretFile(tokenFile, "service account token")
	if err != nil {
		return nil, err
	}
	return &OnePassword{token: token, integration: integration, version: version, timeout: timeout, factory: sdkFactory}, nil
}

// String, GoString and LogValue keep the token out of every print path.
func (o *OnePassword) String() string             { return "OnePassword(<redacted>)" }
func (o *OnePassword) GoString() string           { return o.String() }
func (o *OnePassword) LogValue() slog.Value       { return slog.StringValue(o.String()) }
func (o *OnePassword) Ready() bool                { return o.token != "" }
func (o *OnePassword) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(o.String())) }

func (o *OnePassword) getClient(ctx context.Context) (secretsAPI, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.client != nil {
		return o.client, nil
	}
	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()
	client, err := o.factory(ctx, o.token, o.integration, o.version)
	if err != nil {
		slog.Warn("1password authentication failed")
		return nil, ErrProviderUnavailable
	}
	o.client = client
	return client, nil
}

// Resolve reads every reference concurrently under one bounded timeout. The
// first failure cancels the sibling reads.
func (o *OnePassword) Resolve(ctx context.Context, refs map[string]string) (map[string]string, error) {
	client, err := o.getClient(ctx)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()

	type result struct {
		name, value string
		err         error
	}
	results := make(chan result, len(refs))
	for name, ref := range refs {
		go func() {
			value, err := client.Resolve(ctx, ref)
			results <- result{name, value, err}
		}()
	}
	values := make(map[string]string, len(refs))
	var failure error
	for range refs {
		r := <-results
		switch {
		case r.err != nil && failure == nil:
			failure = ErrFieldUnavailable
			if ctx.Err() != nil {
				failure = ErrProviderUnavailable
			}
			cancel() // stop sibling work
			slog.Warn("1password could not resolve a mapped field", "field", r.name)
		case r.err == nil && r.value == "" && failure == nil:
			failure = ErrFieldUnavailable
			cancel()
		case r.err == nil:
			values[r.name] = r.value
		}
	}
	if failure != nil {
		return nil, failure
	}
	return values, nil
}
