// Package packageproxy converts a package job proxy URL into package-manager-specific settings.
package packageproxy

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Proxy is a validated HTTP or HTTPS proxy used by a package manager process.
type Proxy struct {
	rawURL string
	url    *url.URL
}

// Parse validates a package proxy URL and returns its normalized representation.
func Parse(value string) (Proxy, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Proxy{}, fmt.Errorf("proxy is required")
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return Proxy{}, fmt.Errorf("proxy must be a valid URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return Proxy{}, fmt.Errorf("proxy URL scheme must be http or https")
	}
	if parsed.Hostname() == "" {
		return Proxy{}, fmt.Errorf("proxy URL host is required")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return Proxy{}, fmt.Errorf("proxy URL must not contain a path")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return Proxy{}, fmt.Errorf("proxy URL must not contain a query or fragment")
	}
	if strings.HasSuffix(parsed.Host, ":") {
		return Proxy{}, fmt.Errorf("proxy URL port is required after colon")
	}
	if port := parsed.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return Proxy{}, fmt.Errorf("proxy URL port must be between 1 and 65535")
		}
	}
	return Proxy{rawURL: value, url: parsed}, nil
}

// Environment returns standard proxy variables plus the manager's native proxy setting.
func (p Proxy) Environment(manager string) map[string]string {
	environment := map[string]string{
		"HTTP_PROXY":  p.rawURL,
		"HTTPS_PROXY": p.rawURL,
		"http_proxy":  p.rawURL,
		"https_proxy": p.rawURL,
	}
	switch strings.ToLower(strings.TrimSpace(manager)) {
	case "npm":
		environment["npm_config_proxy"] = p.rawURL
		environment["npm_config_https_proxy"] = p.rawURL
	case "pip":
		environment["PIP_PROXY"] = p.rawURL
	case "yarn":
		environment["YARN_HTTP_PROXY"] = p.rawURL
		environment["YARN_HTTPS_PROXY"] = p.rawURL
	}
	return environment
}

// GradleArguments returns JVM system properties understood by Gradle's dependency transport.
func (p Proxy) GradleArguments() []string {
	port := p.port()
	arguments := make([]string, 0, 8)
	for _, protocol := range []string{"http", "https"} {
		arguments = append(arguments,
			"-D"+protocol+".proxyHost="+p.url.Hostname(),
			"-D"+protocol+".proxyPort="+port,
		)
		if p.url.User != nil {
			arguments = append(arguments, "-D"+protocol+".proxyUser="+p.url.User.Username())
			if password, exists := p.url.User.Password(); exists {
				arguments = append(arguments, "-D"+protocol+".proxyPassword="+password)
			}
		}
	}
	return arguments
}

// MavenSettings returns a minimal settings.xml containing one active proxy.
func (p Proxy) MavenSettings() ([]byte, error) {
	portNumber, _ := strconv.Atoi(p.port())
	settings := mavenSettings{
		Namespace: "http://maven.apache.org/SETTINGS/1.0.0",
		Proxies: []mavenProxy{{
			ID:       "artifact-downloader",
			Active:   true,
			Protocol: p.url.Scheme,
			Host:     p.url.Hostname(),
			Port:     portNumber,
		}},
	}
	if p.url.User != nil {
		settings.Proxies[0].Username = p.url.User.Username()
		settings.Proxies[0].Password, _ = p.url.User.Password()
	}

	var output bytes.Buffer
	output.WriteString(xml.Header)
	encoder := xml.NewEncoder(&output)
	encoder.Indent("", "  ")
	if err := encoder.Encode(settings); err != nil {
		return nil, fmt.Errorf("encode Maven proxy settings: %w", err)
	}
	return output.Bytes(), nil
}

func (p Proxy) port() string {
	if port := p.url.Port(); port != "" {
		return port
	}
	if p.url.Scheme == "https" {
		return "443"
	}
	return "80"
}

type mavenSettings struct {
	XMLName   xml.Name     `xml:"settings"`
	Namespace string       `xml:"xmlns,attr"`
	Proxies   []mavenProxy `xml:"proxies>proxy"`
}

type mavenProxy struct {
	ID       string `xml:"id"`
	Active   bool   `xml:"active"`
	Protocol string `xml:"protocol"`
	Host     string `xml:"host"`
	Port     int    `xml:"port"`
	Username string `xml:"username,omitempty"`
	Password string `xml:"password,omitempty"`
}
