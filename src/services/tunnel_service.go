package services

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"tunnerse/config"
	"tunnerse/models"
	"tunnerse/utils"

	"time"
	"tunnerse/validation"
)

type TunnelService struct {
	validator *validation.TunnelValidator
	tunnels   map[string]*Tunnel
	mux       sync.RWMutex
}

func NewTunnelService() *TunnelService {
	return &TunnelService{
		validator: validation.NewTunnelValidator(),
		tunnels:   make(map[string]*Tunnel),
	}
}

type Tunnel struct {
	requestCh  chan *http.Request
	responseCh chan []byte
	writerCh   chan http.ResponseWriter
	resetTimer func()
	closed     bool
	mu         sync.Mutex
}

func (s *TunnelService) Register(name string) (string, error) {
	if err := s.validator.ValidateTunnelRegister(name); err != nil {
		return "", err
	}

	var tunnelName string
	for {
		random := utils.RandomCode(3)
		tunnelName = name + "-" + random

		s.mux.RLock()
		_, exists := s.tunnels[tunnelName]
		s.mux.RUnlock()

		if !exists {
			break
		}
	}

	t := &Tunnel{
		requestCh:  make(chan *http.Request),
		responseCh: make(chan []byte),
		writerCh:   make(chan http.ResponseWriter),
	}

	inactivityDuration := time.Duration(config.AppConfig.TUNNEL_INACTIVITY_LIFE_TIME) * time.Second
	maxLifetimeDuration := time.Duration(config.AppConfig.TUNNEL_LIFE_TIME) * time.Second

	inactivityTimer := time.NewTimer(inactivityDuration)
	maxLifetimeTimer := time.NewTimer(maxLifetimeDuration)

	t.resetTimer = func() {
		if !inactivityTimer.Stop() {
			select {
			case <-inactivityTimer.C:
			default:
			}
		}
		inactivityTimer.Reset(inactivityDuration)
	}

	s.mux.Lock()
	s.tunnels[tunnelName] = t
	s.mux.Unlock()

	go func(tunnelName string, t *Tunnel) {
		defer func() {
			s.mux.Lock()
			delete(s.tunnels, tunnelName)
			s.mux.Unlock()
			close(t.requestCh)
			close(t.responseCh)
			close(t.writerCh)
		}()

		select {
		case <-inactivityTimer.C:
		case <-maxLifetimeTimer.C:
		}
	}(tunnelName, t)

	return tunnelName, nil
}

func (s *TunnelService) Get(name string) ([]byte, error) {
	s.mux.RLock()
	tunnel, exists := s.tunnels[name]
	s.mux.RUnlock()

	if !exists {
		return nil, fmt.Errorf("tunnel not found")
	}

	if tunnel.resetTimer != nil {
		tunnel.resetTimer()
	}

	req := <-tunnel.requestCh

	if req == nil {
		return nil, fmt.Errorf("nil request received")
	}

	var bodyBytes []byte
	if req.Body != nil {
		defer req.Body.Close()
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}
	}

	headersCopy := make(map[string][]string, len(req.Header))
	for k, v := range req.Header {
		copied := make([]string, len(v))
		copy(copied, v)
		headersCopy[k] = copied
	}

	sreq := models.SerializableRequest{
		Method: req.Method,
		Path:   req.URL.String(),
		Header: headersCopy,
		Body:   string(bodyBytes),
		Host:   req.Host,
	}

	return json.Marshal(&sreq)
}

func (s *TunnelService) Response(name string, body io.ReadCloser) error {
	defer body.Close()

	s.mux.RLock()
	tunnel, exists := s.tunnels[name]
	s.mux.RUnlock()
	if !exists {
		return fmt.Errorf("tunnel not found")
	}

	var resp models.ResponseData
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return fmt.Errorf("failed to decode response JSON: %w", err)
	}

	bodyDecoded, err := base64.StdEncoding.DecodeString(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to decode base64 body: %w", err)
	}

	var wr http.ResponseWriter
	select {
	case wr = <-tunnel.writerCh:
	case <-time.After(5 * time.Second):
		return fmt.Errorf("writer channel timeout")
	}
	if wr == nil {
		return fmt.Errorf("invalid writer")
	}

	if val := resp.Headers["Tunnerse"]; len(val) > 0 && val[0] == "healtcheck-response" {
		wr.Header().Set("Tunnerse", "healthcheck-conclued")
		wr.WriteHeader(http.StatusNoContent)
		return nil
	}

	for key, values := range resp.Headers {
		for _, v := range values {
			wr.Header().Add(key, v)
		}
	}

	wr.WriteHeader(resp.StatusCode)
	_, err = wr.Write(bodyDecoded)
	return err
}

func (s *TunnelService) Tunnel(name, path string, w http.ResponseWriter, r *http.Request) error {
	if err := s.validator.ValidateTunnelRegister(name); err != nil {
		return err
	}

	s.mux.RLock()
	tunnel, exists := s.tunnels[name]
	s.mux.RUnlock()
	if !exists {
		return fmt.Errorf("tunnel not found")
	}

	tunnel.mu.Lock()
	if tunnel.closed {
		tunnel.mu.Unlock()
		return fmt.Errorf("tunnel is closed")
	}
	if tunnel.resetTimer != nil {
		tunnel.resetTimer()
	}
	tunnel.mu.Unlock()

	var bodyBytes []byte
	if r.Body != nil {
		defer r.Body.Close()
		var err error
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			return fmt.Errorf("failed to read request body: %w", err)
		}
	}

	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	clonedRequest := r.Clone(r.Context())
	clonedRequest.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	if !config.AppConfig.SUBDOMAIN {
		if parts := strings.SplitN(clonedRequest.URL.Path, "/", 3); len(parts) >= 3 {
			clonedRequest.URL.Path = "/" + parts[2]
			clonedRequest.RequestURI = clonedRequest.URL.Path
		} else {
			clonedRequest.URL.Path = "/"
			clonedRequest.RequestURI = "/"
		}
	} else {
		clonedRequest.URL.Path = path
		clonedRequest.RequestURI = path
	}

	defer func() {
		if r := recover(); r != nil {
		}
	}()

	timeout := 5 * time.Second

	select {
	case tunnel.requestCh <- clonedRequest:
	case <-time.After(timeout):
		return fmt.Errorf("timeout")
	}

	select {
	case tunnel.writerCh <- w:
	case <-time.After(timeout):
		return fmt.Errorf("timeout")
	}

	return nil
}

func (s *TunnelService) Close(name string) error {
	s.mux.Lock()
	tunnel, exists := s.tunnels[name]
	if !exists {
		s.mux.Unlock()
		return fmt.Errorf("tunnel not found")
	}
	delete(s.tunnels, name)
	s.mux.Unlock()

	tunnel.mu.Lock()
	tunnel.closed = true
	tunnel.mu.Unlock()

	close(tunnel.requestCh)
	close(tunnel.responseCh)
	close(tunnel.writerCh)

	return nil
}

func (s *TunnelService) serveHTML(w http.ResponseWriter, status int, headerValue, folder, fallbackMsg string) {
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Tunnerse", headerValue)
	w.WriteHeader(status)

	path := filepath.Join("static", fmt.Sprintf("%s.html", folder))

	data, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, fallbackMsg, status)
		return
	}

	w.Write(data)
}

func (s *TunnelService) NotFound(w http.ResponseWriter) {
	s.serveHTML(w, http.StatusNotFound, "tunnel-not-found", "notfound", "404 - tunnel not found")
}

func (s *TunnelService) Timeout(w http.ResponseWriter) {
	s.serveHTML(w, http.StatusNotFound, "tunnel-timeout", "timeout", "404 - tunnel timeout")
}

func (s *TunnelService) Home(w http.ResponseWriter) {
	s.serveHTML(w, http.StatusNotFound, "tunnel-working", "running", "404 - tunnel timeout")
}
