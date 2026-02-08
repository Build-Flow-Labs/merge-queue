package github

import (
	"fmt"
	"net/http"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v60/github"
)

// AppConfig holds GitHub App configuration
type AppConfig struct {
	AppID          int64
	PrivateKey     string // PEM content
	PrivateKeyPath string // Path to PEM file
	WebhookSecret  string
}

// NewInstallationClient creates a GitHub client for a specific installation
func NewInstallationClient(config *AppConfig, installationID int64) (*github.Client, error) {
	var tr *ghinstallation.Transport
	var err error

	if config.PrivateKey != "" {
		tr, err = ghinstallation.New(http.DefaultTransport, config.AppID, installationID, []byte(config.PrivateKey))
	} else if config.PrivateKeyPath != "" {
		tr, err = ghinstallation.NewKeyFromFile(http.DefaultTransport, config.AppID, installationID, config.PrivateKeyPath)
	} else {
		return nil, fmt.Errorf("no private key configured")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create installation transport: %w", err)
	}

	return github.NewClient(&http.Client{Transport: tr}), nil
}

// GetInstallationToken returns an access token for git operations
func GetInstallationToken(config *AppConfig, installationID int64) (string, error) {
	var tr *ghinstallation.Transport
	var err error

	if config.PrivateKey != "" {
		tr, err = ghinstallation.New(http.DefaultTransport, config.AppID, installationID, []byte(config.PrivateKey))
	} else if config.PrivateKeyPath != "" {
		tr, err = ghinstallation.NewKeyFromFile(http.DefaultTransport, config.AppID, installationID, config.PrivateKeyPath)
	} else {
		return "", fmt.Errorf("no private key configured")
	}
	if err != nil {
		return "", fmt.Errorf("failed to create installation transport: %w", err)
	}

	token, err := tr.Token(nil)
	if err != nil {
		return "", fmt.Errorf("failed to get token: %w", err)
	}

	return token, nil
}
