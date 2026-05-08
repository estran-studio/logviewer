package http

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/estran-studio/logviewer/pkg/ty"
)

// TLSConfig holds TLS-related configuration for HTTP clients
type TLSConfig struct {
	// InsecureSkipVerify disables TLS certificate verification (NOT RECOMMENDED for production)
	InsecureSkipVerify bool
	// CACert is a PEM-encoded CA certificate (optional, for custom/self-signed certs)
	CACert string
	// CACertFile is a path to a PEM-encoded CA certificate file (optional)
	CACertFile string
}

// Auth provides an interface for authenticating HTTP requests.
type Auth interface {
	Login(req *http.Request) error
}

// CookieAuth implements Auth using a simple cookie string.
type CookieAuth struct {
	Cookie string
}

// Login sets the Cookie header on the request.
func (c CookieAuth) Login(req *http.Request) error {
	req.Header.Set("Cookie", c.Cookie)
	return nil
}

// HeaderAuth sets fixed headers (like Authorization) on each request.
type HeaderAuth struct {
	Headers ty.MS
}

// Login sets the configured headers on the request.
func (h HeaderAuth) Login(req *http.Request) error {
	for k, v := range h.Headers {
		req.Header.Set(k, v)
	}
	return nil
}

// Client is a wrapper around http.Client with convenience methods for JSON/Data requests.
type Client struct {
	client http.Client
	url    string
}

// Debug controls whether verbose HTTP-level debug logs are emitted. Tests and
// production code can toggle this to avoid leaking secrets into logs.
var Debug = false

// SetDebug sets the package debug flag.
func SetDebug(d bool) {
	Debug = d
}

// DebugEnabled returns whether HTTP debug logging is enabled.
func DebugEnabled() bool {
	return Debug
}

func (c Client) post(path string, headers ty.MS, buf *bytes.Buffer, responseData interface{}, auth Auth) error {
	path = c.url + path

	if Debug {
		log.Printf("[POST]%s %s"+ty.LB, path, buf.String())
	}

	req, err := http.NewRequest("POST", path, buf)
	if err != nil {
		return err
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	if auth != nil {
		if err = auth.Login(req); err != nil {
			log.Printf("authentication setup failed: %s", err.Error())
			return err
		}
	}

	// Log headers but redact sensitive values (Authorization, Cookie, tokens)
	if Debug {
		log.Printf("[POST-HEADERS] %s\n", maskHeaderMap(req.Header))
	}

	res, err := c.client.Do(req)
	if err != nil {
		return err
	}

	if res.Body != nil {
		defer func() { _ = res.Body.Close() }()
	}

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	if res.StatusCode >= 400 {
		log.Printf("error %d  %s"+ty.LB, res.StatusCode, string(resBody))
		return fmt.Errorf("request failed with status code %d: %s", res.StatusCode, string(resBody))
	}

	return json.Unmarshal(resBody, &responseData)
}

// PostData performs a POST request with URL-encoded form data.
func (c Client) PostData(path string, headers ty.MS, body ty.MS, responseData interface{}, auth Auth) error {

	headers["Content-Type"] = "application/x-www-form-urlencoded"

	// Build form-encoded body using url.Values to ensure proper encoding of keys/values
	values := url.Values{}
	for k, v := range body {
		values.Add(k, v)
	}

	encoded := values.Encode()
	buf := bytes.NewBufferString(encoded)

	return c.post(path, headers, buf, responseData, auth)

}

// PostJSON performs a POST request with a JSON body.
func (c Client) PostJSON(path string, headers ty.MS, body interface{}, responseData interface{}, auth Auth) error {

	headers["Content-Type"] = "application/json"

	var buf bytes.Buffer
	encErr := json.NewEncoder(&buf).Encode(body)
	if encErr != nil {
		return encErr
	}

	return c.post(path, headers, &buf, responseData, auth)

}

// Get performs a GET request.
func (c Client) Get(path string, queryParams ty.MS, headers ty.MS, body interface{}, responseData interface{}, auth Auth) error {

	var buf bytes.Buffer

	if body != nil {
		encErr := json.NewEncoder(&buf).Encode(body)
		if encErr != nil {
			return encErr
		}

	}
	path = c.url + path

	q := url.Values{}

	for k, v := range queryParams {
		q.Add(k, v)
	}

	queryParamString := q.Encode()

	if queryParamString != "" {
		path += "?" + queryParamString
	}

	if Debug {
		log.Printf("[GET]%s %s\n", path, buf.String())
	}

	req, err := http.NewRequest("GET", path, &buf)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	if auth != nil {
		if err = auth.Login(req); err != nil {
			log.Printf("authentication setup failed: %s", err.Error())
			return err
		}
	}

	res, getErr := c.client.Do(req)
	if getErr != nil {
		return getErr
	}

	if res.Body != nil {
		defer func() { _ = res.Body.Close() }()
	}

	resBody, readErr := io.ReadAll(res.Body)
	if readErr != nil {
		return readErr
	}

	if res.StatusCode >= 400 {
		log.Printf("error %d  %s"+ty.LB, res.StatusCode, string(resBody))
		return fmt.Errorf("request failed with status code %d: %s", res.StatusCode, string(resBody))
	}

	// Log a truncated GET response body for debugging (avoid huge output)
	if Debug && len(resBody) > 0 {
		s := string(resBody)
		if len(s) > 2000 {
			s = s[:2000] + "...TRUNCATED"
		}
		log.Printf("[GET-RAW] %s", s)
	}

	jsonErr := json.Unmarshal(resBody, &responseData)
	if jsonErr != nil {
		return jsonErr
	}

	return nil
}

// Delete performs a DELETE request.
func (c Client) Delete(path string, headers ty.MS, auth Auth) error {
	path = c.url + path

	if Debug {
		log.Printf("[DELETE]%s"+ty.LB, path)
	}

	req, err := http.NewRequest("DELETE", path, nil)
	if err != nil {
		return err
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	if auth != nil {
		if err = auth.Login(req); err != nil {
			log.Printf("authentication setup failed: %s", err.Error())
			return err
		}
	}

	// Log headers but redact sensitive values (Authorization, Cookie, tokens)
	if Debug {
		log.Printf("[DELETE-HEADERS] %s\n", maskHeaderMap(req.Header))
	}

	res, err := c.client.Do(req)
	if err != nil {
		return err
	}

	if res.Body != nil {
		defer func() { _ = res.Body.Close() }()
	}

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	if res.StatusCode >= 400 {
		log.Printf("error %d  %s"+ty.LB, res.StatusCode, string(resBody))
		return fmt.Errorf("request failed with status code %d: %s", res.StatusCode, string(resBody))
	}

	return nil
}

// GetClient returns a new Client for the given URL with optional TLS configuration.
// If tlsConfig is nil, TLS configuration is read from environment variables.
func GetClient(url string, tlsConfig *TLSConfig) Client {
	// Normalize URL: if scheme is missing, default to https. Also remove
	// any trailing slash to avoid double slashes when appending paths.
	if url != "" {
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			url = "https://" + url
		}
		// remove trailing slashes for consistent concatenation
		url = strings.TrimRight(url, "/")
	}

	spaceClient := getSpaceClient(tlsConfig)

	return Client{
		client: spaceClient,
		url:    url,
	}
}

func getSpaceClient(tlsConfig *TLSConfig) http.Client {
	// If no custom TLS config provided, use environment variables
	if tlsConfig == nil {
		tlsConfig = getTLSConfigFromEnv()
	}

	switch v := http.DefaultTransport.(type) {
	case (*http.Transport):
		customTransport := v.Clone()

		// Build TLS config
		clientTLSConfig := &tls.Config{
			MinVersion: tls.VersionTLS12, // Secure default
		}

		if tlsConfig.InsecureSkipVerify {
			log.Println("[WARN] TLS certificate verification is disabled - this is insecure for production")
			clientTLSConfig.InsecureSkipVerify = true
		} else {
			// Secure default: use system cert pool
			certPool, err := x509.SystemCertPool()
			if err != nil {
				certPool = x509.NewCertPool()
			}

			// Add custom CA if provided
			if tlsConfig.CACert != "" {
				if ok := certPool.AppendCertsFromPEM([]byte(tlsConfig.CACert)); !ok {
					log.Println("[WARN] Failed to parse CA certificate from CACert")
				}
			} else if tlsConfig.CACertFile != "" {
				caCertPEM, err := os.ReadFile(tlsConfig.CACertFile)
				if err != nil {
					log.Printf("[WARN] Failed to read CA cert file %s: %v", tlsConfig.CACertFile, err)
				} else if ok := certPool.AppendCertsFromPEM(caCertPEM); !ok {
					log.Println("[WARN] Failed to parse CA certificate from file")
				}
			}

			clientTLSConfig.RootCAs = certPool
		}

		customTransport.TLSClientConfig = clientTLSConfig
		return http.Client{Transport: customTransport}
	default:
		return http.Client{}
	}
}

// getTLSConfigFromEnv reads TLS configuration from environment variables
func getTLSConfigFromEnv() *TLSConfig {
	cfg := &TLSConfig{}

	// Check for insecure mode (opt-in)
	if os.Getenv("LOGVIEWER_TLS_INSECURE") == "true" {
		cfg.InsecureSkipVerify = true
	}

	// Check for custom CA cert
	cfg.CACert = os.Getenv("LOGVIEWER_CA_CERT")
	cfg.CACertFile = os.Getenv("LOGVIEWER_CA_CERT_FILE")

	return cfg
}

// maskHeaderMap returns a string representation of headers with sensitive
// values redacted (keeps first 4 chars for debugging). This avoids leaking
// secrets into logs while letting us verify headers are present.
func maskHeaderMap(h http.Header) string {
	redacted := make([]string, 0, len(h))
	for k, vals := range h {
		v := ""
		if len(vals) > 0 {
			val := vals[0]
			// Redact common sensitive headers
			switch strings.ToLower(k) {
			case "authorization", "cookie", "x-splunk-token", "x-auth-token":
				if len(val) > 4 {
					v = val[:4] + "...REDACTED"
				} else {
					v = "REDACTED"
				}
			default:
				v = val
			}
		}
		redacted = append(redacted, fmt.Sprintf("%s: %s", k, v))
	}
	return strings.Join(redacted, "; ")
}
