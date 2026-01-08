package expose

import (
	"bufio"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/pedroborgesdev/tunnerse-api/internal/api/logger"
)

var (
	routes       = make(map[string]string)
	redirectList = make([]string, 0)
)

func loadConfig(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var section string
	domainsFound := false
	domainsCount := 0
	redirectsCount := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(line[1 : len(line)-1])
			if section == "domains" {
				domainsFound = true
			}
			continue
		}

		switch section {
		case "domains":
			parts := strings.Split(line, "=")
			if len(parts) != 2 {
				return fmt.Errorf("invalid line on config: %s", line)
			}
			domain := strings.TrimSpace(parts[0])
			port := strings.TrimSpace(parts[1])

			if domain == "" {
				return fmt.Errorf("invalid or null domain")
			}
			if port == "" {
				return fmt.Errorf("invalid or null port")
			}

			routes[domain] = port
			domainsCount++

		case "redirects":
			redirectList = append(redirectList, strings.ToLower(line))
			redirectsCount++
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	if !domainsFound {
		return fmt.Errorf("config file must contain [domains] section")
	}
	if domainsCount == 0 {
		return fmt.Errorf("[domains] section is empty, configure at least one domain")
	}

	return nil
}

func newReverseProxy(target string) *httputil.ReverseProxy {
	u, _ := url.Parse(target)
	return httputil.NewSingleHostReverseProxy(u)
}

func handler(w http.ResponseWriter, r *http.Request) {
	host := strings.ToLower(strings.Split(r.Host, ":")[0])

	for domain, port := range routes {
		domain = strings.ToLower(domain)

		if strings.HasPrefix(domain, "*.") {
			base := strings.TrimPrefix(domain, "*.")
			if strings.HasSuffix(host, "."+base) || host == base {
				target := fmt.Sprintf("http://localhost:%s", port)
				newReverseProxy(target).ServeHTTP(w, r)
				return
			}
		} else if host == domain {
			target := fmt.Sprintf("http://localhost:%s", port)
			newReverseProxy(target).ServeHTTP(w, r)
			return
		}
	}

	http.Error(w, "domain not configured", http.StatusNotFound)
}

func Expose() error {
	err := loadConfig("tunnerse.config")
	if err != nil {
		return fmt.Errorf("error to load config: %v", err)
	}

	go func() {
		http.ListenAndServe(":80", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			url := "https://" + r.Host + r.URL.String()
			http.Redirect(w, r, url, http.StatusMovedPermanently)
		}))
	}()

	server := &http.Server{
		Addr:    ":443",
		Handler: http.HandlerFunc(handler),
	}

	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	certFile := filepath.Join(wd, "certs", "certificates", "tunnerse.com.crt")
	keyFile := filepath.Join(wd, "certs", "certificates", "tunnerse.com.key")

	logger.Log("INFO", "Servidor HTTPS rodando em :443", []logger.LogDetail{
		{Key: "certFile", Value: certFile},
		{Key: "keyFile", Value: keyFile},
	})
	return server.ListenAndServeTLS(certFile, keyFile)
}
