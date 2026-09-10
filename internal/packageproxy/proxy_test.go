package packageproxy

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseRejectsInvalidProxyURLs(t *testing.T) {
	for _, value := range []string{
		"", "proxy.example.test:8080", "socks5://proxy.example.test:1080",
		"http:///missing-host", "http://proxy.example.test:", "http://proxy.example.test:0", "http://proxy.example.test:65536",
		"http://proxy.example.test/path", "http://proxy.example.test?query=value",
	} {
		t.Run(value, func(t *testing.T) {
			if _, err := Parse(value); err == nil {
				t.Fatalf("Parse(%q) succeeded", value)
			}
		})
	}
}

func TestParseErrorDoesNotExposeProxyCredentials(t *testing.T) {
	_, err := Parse("http://user:top-secret@proxy.example.test:invalid")
	if err == nil {
		t.Fatal("Parse() accepted an invalid proxy port")
	}
	if strings.Contains(err.Error(), "top-secret") {
		t.Fatalf("Parse() exposed proxy credentials: %v", err)
	}
}

func TestEnvironmentUsesNativeManagerSettings(t *testing.T) {
	proxy, err := Parse("http://proxy.example.test:8080")
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]map[string]string{
		"npm": {
			"npm_config_proxy":       "http://proxy.example.test:8080",
			"npm_config_https_proxy": "http://proxy.example.test:8080",
		},
		"pip": {
			"PIP_PROXY": "http://proxy.example.test:8080",
		},
		"yarn": {
			"YARN_HTTP_PROXY":  "http://proxy.example.test:8080",
			"YARN_HTTPS_PROXY": "http://proxy.example.test:8080",
		},
		"gradle": {},
		"mvn":    {},
	}
	for manager, native := range tests {
		t.Run(manager, func(t *testing.T) {
			environment := proxy.Environment(manager)
			for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
				if environment[name] != "http://proxy.example.test:8080" {
					t.Fatalf("%s = %q", name, environment[name])
				}
			}
			for name, value := range native {
				if environment[name] != value {
					t.Fatalf("%s = %q", name, environment[name])
				}
			}
		})
	}
}

func TestGradleArgumentsIncludeProxyCredentials(t *testing.T) {
	proxy, err := Parse("https://user:p%40ss@proxy.example.test")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"-Dhttp.proxyHost=proxy.example.test", "-Dhttp.proxyPort=443",
		"-Dhttp.proxyUser=user", "-Dhttp.proxyPassword=p@ss",
		"-Dhttps.proxyHost=proxy.example.test", "-Dhttps.proxyPort=443",
		"-Dhttps.proxyUser=user", "-Dhttps.proxyPassword=p@ss",
	}
	if got := proxy.GradleArguments(); !reflect.DeepEqual(got, want) {
		t.Fatalf("GradleArguments() = %#v, want %#v", got, want)
	}
}

func TestMavenSettingsEscapesProxyCredentials(t *testing.T) {
	proxy, err := Parse("http://user:p%26ss@proxy.example.test:3128")
	if err != nil {
		t.Fatal(err)
	}
	data, err := proxy.MavenSettings()
	if err != nil {
		t.Fatal(err)
	}
	settings := string(data)
	for _, want := range []string{
		"<protocol>http</protocol>", "<host>proxy.example.test</host>", "<port>3128</port>",
		"<username>user</username>", "<password>p&amp;ss</password>",
	} {
		if !strings.Contains(settings, want) {
			t.Fatalf("MavenSettings() missing %q:\n%s", want, settings)
		}
	}
}
