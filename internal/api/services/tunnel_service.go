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
	"time"

	"github.com/google/uuid"
	"github.com/pedroborgesdev/tunnerse-api/internal/api/config"
	"github.com/pedroborgesdev/tunnerse-api/internal/api/logger"
	"github.com/pedroborgesdev/tunnerse-api/internal/api/models"
	"github.com/pedroborgesdev/tunnerse-api/internal/api/utils"
	"github.com/pedroborgesdev/tunnerse-api/internal/api/validation"
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
	requestCh       chan *http.Request
	writerCh        chan http.ResponseWriter
	pendingRequests map[string]chan *ResponseWithToken // Token -> canal de resposta
	resetTimer      func()
	stopTimer       chan struct{}
	closed          bool
	mu              sync.Mutex
}

type ResponseWithToken struct {
	Writer http.ResponseWriter
	Resp   *models.ResponseData
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
		requestCh:       make(chan *http.Request),
		writerCh:        make(chan http.ResponseWriter),
		pendingRequests: make(map[string]chan *ResponseWithToken),
		stopTimer:       make(chan struct{}),
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
			inactivityTimer.Stop()
			maxLifetimeTimer.Stop()

			t.mu.Lock()
			t.closed = true
			// Limpa todos os canais de resposta pendentes
			for token, ch := range t.pendingRequests {
				close(ch)
				delete(t.pendingRequests, token)
			}
			t.mu.Unlock()

			s.mux.Lock()
			delete(s.tunnels, tunnelName)
			s.mux.Unlock()

			close(t.requestCh)
			close(t.writerCh)
			close(t.stopTimer)
		}()

		select {
		case <-inactivityTimer.C:
		case <-maxLifetimeTimer.C:
		case <-t.stopTimer:
		}
	}(tunnelName, t)

	return tunnelName, nil
}

func (s *TunnelService) Get(name string, r *http.Request) ([]byte, error) {
	s.mux.RLock()
	tunnel, exists := s.tunnels[name]
	s.mux.RUnlock()

	if !exists {
		return nil, fmt.Errorf("tunnel not found")
	}

	tunnel.mu.Lock()
	if tunnel.closed {
		tunnel.mu.Unlock()
		return nil, fmt.Errorf("tunnel is closed")
	}
	if tunnel.resetTimer != nil {
		tunnel.resetTimer()
	}
	tunnel.mu.Unlock()

	var req *http.Request

	select {
	case req = <-tunnel.requestCh:
		if req == nil {
			return nil, fmt.Errorf("nil request received")
		}
	case <-r.Context().Done():
		return nil, fmt.Errorf("client disconnected; tunnel has a 1-minute grace period")
	}

	// Extrai o token da requisição recebida
	token := req.Header.Get("Tunnerse-Request-Token")
	if token == "" {
		// Se não houver token no header, tenta pegar do contexto
		if tokenVal := req.Context().Value("tunnerse-token"); tokenVal != nil {
			token = tokenVal.(string)
		}
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
		Token:  token, // Inclui o token na resposta
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

	// Log all response headers for debugging
	logger.Log("DEBUG", "Response headers received", []logger.LogDetail{
		{Key: "tunnel", Value: name},
		{Key: "token", Value: resp.Token},
		{Key: "headers", Value: fmt.Sprintf("%+v", resp.Headers)},
	})

	// Valida se o token está presente
	if resp.Token == "" {
		return fmt.Errorf("missing Tunnerse-Request-Token in response")
	}

	tunnel.mu.Lock()
	if tunnel.closed {
		tunnel.mu.Unlock()
		return fmt.Errorf("tunnel is closed")
	}

	// Busca o canal de resposta para este token específico
	responseCh, exists := tunnel.pendingRequests[resp.Token]
	if !exists {
		tunnel.mu.Unlock()
		return fmt.Errorf("no pending request found for token: %s (expired or invalid)", resp.Token)
	}
	delete(tunnel.pendingRequests, resp.Token)
	tunnel.mu.Unlock()

	// Envia a resposta para o canal específico desta requisição
	select {
	case responseCh <- &ResponseWithToken{Resp: &resp}:
		close(responseCh)
	case <-time.After(5 * time.Second):
		close(responseCh)
		return fmt.Errorf("response channel timeout for token: %s", resp.Token)
	}

	return nil
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

	// Gera um token único para esta requisição
	token := uuid.New().String()

	// Cria um canal específico para a resposta desta requisição
	responseCh := make(chan *ResponseWithToken, 1)

	tunnel.mu.Lock()
	if tunnel.closed {
		tunnel.mu.Unlock()
		return fmt.Errorf("tunnel is closed")
	}
	tunnel.pendingRequests[token] = responseCh
	tunnel.mu.Unlock()

	// Cleanup: remove o canal se a resposta não chegar
	defer func() {
		tunnel.mu.Lock()
		delete(tunnel.pendingRequests, token)
		tunnel.mu.Unlock()
	}()

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

	// Adiciona o token ao header da requisição
	clonedRequest.Header.Set("Tunnerse-Request-Token", token)

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

	timeout := time.Duration(config.AppConfig.TUNNEL_REQUEST_TIMEOUT) * time.Second

	tunnel.mu.Lock()
	if tunnel.closed {
		tunnel.mu.Unlock()
		return fmt.Errorf("tunnel is closed")
	}
	requestCh := tunnel.requestCh
	tunnel.mu.Unlock()

	// Envia a requisição
	select {
	case requestCh <- clonedRequest:
	case <-time.After(timeout):
		return fmt.Errorf("timeout")
	case <-r.Context().Done():
		return fmt.Errorf("client disconnected")
	}

	// Aguarda a resposta específica para este token
	select {
	case respData := <-responseCh:
		if respData == nil || respData.Resp == nil {
			return fmt.Errorf("received nil response")
		}

		// Verifica se é um erro da API local
		if tunnerseHeader, ok := respData.Resp.Headers["Tunnerse"]; ok && len(tunnerseHeader) > 0 {
			if tunnerseHeader[0] == "local-api-error" {
				return fmt.Errorf("local-api-error")
			}
		}

		// Decodifica o body base64
		bodyDecoded, err := base64.StdEncoding.DecodeString(respData.Resp.Body)
		if err != nil {
			return fmt.Errorf("failed to decode base64 body: %w", err)
		}

		// Escreve os headers
		for key, values := range respData.Resp.Headers {
			for _, v := range values {
				w.Header().Add(key, v)
			}
		}

		// Escreve o status code e body
		w.WriteHeader(respData.Resp.StatusCode)
		_, err = w.Write(bodyDecoded)
		return err

	case <-time.After(timeout):
		return fmt.Errorf("timeout")
	case <-r.Context().Done():
		return fmt.Errorf("client disconnected")
	}
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
	alreadyClosed := tunnel.closed
	tunnel.closed = true
	tunnel.mu.Unlock()

	if alreadyClosed {
		return nil
	}

	select {
	case tunnel.stopTimer <- struct{}{}:
	default:
	}

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
	s.serveHTML(w, http.StatusRequestTimeout, "tunnel-timeout", "timeout", "408 - tunnel timeout")
}

func (s *TunnelService) LocalError(w http.ResponseWriter) {
	s.serveHTML(w, http.StatusServiceUnavailable, "local-api-error", "localerror", "503 - local api error")
}

func (s *TunnelService) Home(w http.ResponseWriter) {
	s.serveHTML(w, http.StatusOK, "tunnel-working", "running", "Tunnerse is running")
}
