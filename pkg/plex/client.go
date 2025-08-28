package plex

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
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
	// to "1" or "true".
	httpClient := http.Client{}
	if v := os.Getenv("SKIP_TLS_VERIFICATION"); v == "1" || v == "true" {
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
