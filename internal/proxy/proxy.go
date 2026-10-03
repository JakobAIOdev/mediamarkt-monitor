// Package proxy loads proxy lists and redacts credentials from transport errors.
package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Load returns normalized proxy URLs. An omitted path selects direct requests;
// an explicitly provided empty file is an error.
func Load(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open proxy file: %w", err)
	}
	defer file.Close()
	return readProxies(file)
}

func readProxies(input io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(input)
	var proxies []string
	for line := 1; scanner.Scan(); line++ {
		value := strings.TrimSpace(scanner.Text())
		if value == "" || strings.HasPrefix(value, "#") {
			continue
		}
		proxy, err := normalizeProxy(value)
		if err != nil {
			return nil, fmt.Errorf("proxy file line %d: %w", line, err)
		}
		proxies = append(proxies, proxy)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read proxy file: %w", err)
	}
	if len(proxies) == 0 {
		return nil, fmt.Errorf("proxy file contains no proxies")
	}
	return proxies, nil
}

func normalizeProxy(value string) (string, error) {
	if !strings.Contains(value, "://") {
		parts := strings.SplitN(value, ":", 4)
		if len(parts) == 4 {
			if parts[0] == "" || parts[2] == "" || parts[3] == "" || strings.ContainsAny(parts[0], "/@ \t") {
				return "", fmt.Errorf("invalid proxy; expected host:port:user:pass")
			}
			value = (&url.URL{Scheme: "http", Host: net.JoinHostPort(parts[0], parts[1]), User: url.UserPassword(parts[2], parts[3])}).String()
		} else {
			value = "http://" + value
		}
	}
	u, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("invalid proxy URL")
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return "", fmt.Errorf("proxy scheme must be http, https, socks5, or socks5h")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 || u.Hostname() == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("proxy requires a host and valid port, without path or query")
	}
	return u.String(), nil
}

type redactedProxyError struct {
	err     error
	message string
}

func (e *redactedProxyError) Error() string { return e.message }
func (e *redactedProxyError) Unwrap() error { return e.err }

// RedactError hides proxy credentials while preserving the wrapped error for
// errors.Is and errors.As, including product retry delays.
func RedactError(err error, proxyURL string) error {
	if err == nil || proxyURL == "" {
		return err
	}
	message := strings.ReplaceAll(err.Error(), proxyURL, "[proxy]")
	if u, parseErr := url.Parse(proxyURL); parseErr == nil && u.User != nil {
		message = strings.ReplaceAll(message, u.User.String(), "[proxy credentials]")
		if password, ok := u.User.Password(); ok && password != "" {
			message = strings.ReplaceAll(message, password, "[proxy password]")
		}
	}
	return &redactedProxyError{err: err, message: message}
}
