package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Resolution struct {
	TenantID string `json:"tenant_id"`
	AppType  string `json:"app_type"`
}
type Resolver struct {
	baseURL, token string
	client         *http.Client
}

func NewResolver(baseURL, token string) Resolver {
	return Resolver{baseURL: strings.TrimRight(baseURL, "/"), token: token, client: &http.Client{Timeout: 3 * time.Second}}
}
func (r Resolver) Resolve(ctx context.Context, hostname string) (Resolution, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+"/internal/v1/domains/resolve?hostname="+url.QueryEscape(hostname), nil)
	if err != nil {
		return Resolution{}, err
	}
	request.Header.Set("X-Internal-Token", r.token)
	response, err := r.client.Do(request)
	if err != nil {
		return Resolution{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return Resolution{}, ErrNotFound
	}
	if response.StatusCode != http.StatusOK {
		return Resolution{}, fmt.Errorf("tenant resolver returned %d", response.StatusCode)
	}
	var resolution Resolution
	if err := json.NewDecoder(response.Body).Decode(&resolution); err != nil {
		return Resolution{}, err
	}
	return resolution, nil
}
func (r Resolver) Ready(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := r.Resolve(ctx, "readiness-probe.invalid")
	if err == ErrNotFound {
		return nil
	}
	return err
}

type resolutionError string

func (e resolutionError) Error() string { return string(e) }

const ErrNotFound = resolutionError("tenant domain not found")
