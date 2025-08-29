package plex

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

var ErrNotFound = errors.New("not found")

type Client struct {
	Token string
	URL   *url.URL

	httpClient http.Client
}

func NewClient(serverURL, token string) (*Client, error) {
	parsed, err := url.Parse(serverURL)
	if err != nil {
		return nil, err
	}

	// Configure HTTP client and optional TLS skip verification for self-signed
	// or mismatched certificates. Honor SKIP_TLS_VERIFICATION env var when set
	// to "1" or "true". Also allow a configurable timeout via
	// PLEX_CLIENT_TIMEOUT_SECONDS (seconds). Default to 10s to tolerate
	// slower LAN responses and large libraries.
	httpClient := http.Client{}

	// Default timeout
	timeout := 10 * time.Second
	if v := os.Getenv("PLEX_CLIENT_TIMEOUT_SECONDS"); v != "" {
		if s, err := strconv.Atoi(v); err == nil && s > 0 {
			timeout = time.Duration(s) * time.Second
		}
	}
	httpClient.Timeout = timeout

	if v := os.Getenv("SKIP_TLS_VERIFICATION"); v == "1" || v == "true" {
		// nolint:gosec // InsecureSkipVerify is explicit and controlled by SKIP_TLS_VERIFICATION env var for testing/trusted networks
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	client := &Client{
		Token:      token,
		URL:        parsed,
		httpClient: httpClient,
	}

	return client, nil
}

func (c *Client) NewRequest(method, path string) (*http.Request, error) {
	requestPath, err := url.Parse(path)
	if err != nil {
		return nil, err
	}

	reqURL := c.URL.ResolveReference(requestPath)
	req, err := http.NewRequest(method, reqURL.String(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Token", c.Token)

	return req, nil
}

func (c *Client) Do(request *http.Request, data any) error {
	resp, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == 404 {
		return ErrNotFound
	}

	// Decode directly from the response body into the provided target to
	// avoid buffering the entire body in memory (io.ReadAll -> Unmarshal
	// creates an extra large allocation for big responses).
	if data == nil {
		// Nothing to decode into, just consume and return.
		return nil
	}

	dec := json.NewDecoder(resp.Body)
	return dec.Decode(data)
}

func (c *Client) Get(path string, data any) error {
	req, err := c.NewRequest("GET", path)
	if err != nil {
		return err
	}

	// Pass the target directly (don't take address of the interface parameter).
	return c.Do(req, data)
}

// GetWithHeaders performs a GET request like Get but allows callers to supply
// additional request headers. Useful for Plex headers such as
// X-Plex-Container-Start and X-Plex-Container-Size to request paged or
// container-only responses.
func (c *Client) GetWithHeaders(path string, data any, headers map[string]string) error {
	req, err := c.NewRequest("GET", path)
	if err != nil {
		return err
	}

	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}

	return c.Do(req, data)
}
