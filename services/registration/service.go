package registration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

// Service represents the registration service.
type Service struct {
	MetaStore interface {
		ClusterID() (uint64, error)
		NodeID() uint64
	}

	enabled bool
	url     *url.URL
	token   string
	version string

	wg   sync.WaitGroup
	done chan struct{}

	logger *log.Logger
}

// NewService returns a configured registration service.
func NewService(c Config, version string) (*Service, error) {
	url, err := url.Parse(c.URL)
	if err != nil {
		return nil, err
	}

	return &Service{
		enabled: c.Enabled,
		url:     url,
		token:   c.Token,
		version: version,
		done:    make(chan struct{}),
		logger:  log.New(os.Stderr, "[registration] ", log.LstdFlags),
	}, nil
}

// Open starts retention policy enforcement.
func (s *Service) Open() error {
	s.logger.Println("Starting registration service")
	if err := s.registerServer(); err != nil {
		return err
	}

	return nil
}

// Close stops retention policy enforcement.
func (s *Service) Close() error {
	s.logger.Println("registration service terminating")
	close(s.done)
	s.wg.Wait()
	return nil
}

// registerServer registers the server.
func (s *Service) registerServer() error {
	if !s.enabled || s.token == "" {
		return nil
	}
	clusterID, err := s.MetaStore.ClusterID()
	if err != nil {
		s.logger.Printf("failed to retrieve cluster ID for registration: %s", err.Error())
		return err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	j := map[string]interface{}{
		"cluster_id": fmt.Sprintf("%d", clusterID),
		"server_id":  fmt.Sprintf("%d", s.MetaStore.NodeID()),
		"host":       hostname,
		"product":    "influxdb",
		"version":    s.version,
	}
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/api/v1/servers?token=%s", s.url.String(), s.token)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		client := http.Client{Timeout: time.Duration(5 * time.Second)}
		resp, err := client.Post(url, "application/json", bytes.NewBuffer(b))
		if err != nil {
			s.logger.Printf("failed to register server with %s: %s", s.url.String(), err.Error())
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusCreated {
			return
		}
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			s.logger.Printf("failed to read response from registration server: %s", err.Error())
			return
		}
		s.logger.Printf("failed to register server with %s: received code %s, body: %s", s.url.String(), resp.Status, string(body))
	}()
	return nil
}
