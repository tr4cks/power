package ilo

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type PowerState string

const (
	PowerStateOn      PowerState = "On"
	PowerStateOff     PowerState = "Off"
	PowerStateUnknown PowerState = "Unknown"
	PowerReset        PowerState = "Reset"
)

type powerStatus struct {
	PowerState PowerState `json:"PowerState"`
}

type IloClient struct {
	url      *url.URL
	username string
	password string
}

func (c *IloClient) PushPowerButton() error {
	// URL for the iLO endpoint to get power status
	endpoint := c.url.JoinPath("/Systems/1/Actions/ComputerSystem.Reset/")

	// Create the request body to initiate the button press (Action Reset with ResetType PushPowerButton)
	reqBody := map[string]string{
		"ResetType": "PushPowerButton",
	}

	// Encode the JSON data
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("error encoding JSON: %w", err)
	}

	// Create an HTTP POST request to the iLO endpoint for the Reset action
	req, err := http.NewRequest(http.MethodPost, endpoint.String(), bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error creating the request: %w", err)
	}

	// Configure the content type of the request to JSON
	req.Header.Set("Content-Type", "application/json")

	// Add the credentials to the request
	req.SetBasicAuth(c.username, c.password)

	// Ignore SSL certificate verification
	tr := http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := http.Client{Transport: &tr}

	// Send the request to iLO to initiate the button press (Action Reset with ResetType PushPowerButton)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error sending the request: %w", err)
	}

	// Check the response status code
	if resp.StatusCode != http.StatusOK {
		// Read the response body
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("error reading the response body: %w", err)
		}
		return fmt.Errorf("error retrieving server power status (StatusCode: %d, Body: %v)", resp.StatusCode, string(body))
	}

	return nil
}

func (c *IloClient) PowerState() (*PowerState, error) {
	// URL for the iLO endpoint to get power status
	endpoint := c.url.JoinPath("/Systems/1/")

	// Create an HTTP GET request to the iLO endpoint
	req, err := http.NewRequest(http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("error creating the request: %w", err)
	}

	// Add the credentials to the request
	req.SetBasicAuth(c.username, c.password)

	// Ignore SSL certificate verification
	tr := http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := http.Client{Transport: &tr}

	// Send the request to iLO to get power status
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending the request: %w", err)
	}
	defer resp.Body.Close()

	// Check the response status code
	if resp.StatusCode == http.StatusOK {
		// Parse the response body
		var powerStatus powerStatus
		err := json.NewDecoder(resp.Body).Decode(&powerStatus)
		if err != nil {
			return nil, fmt.Errorf("error decoding the JSON response: %w", err)
		}
		return &powerStatus.PowerState, nil
	} else {
		// Read the response body
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("error reading the response body: %w", err)
		}
		return nil, fmt.Errorf("error retrieving server power status (StatusCode: %d, Body: %v)", resp.StatusCode, string(body))
	}
}

func NewClient(baseUrl string, username string, password string) (*IloClient, error) {
	parsedUrl, err := url.Parse(baseUrl)
	parsedUrl.Scheme = "https"
	parsedUrl = parsedUrl.JoinPath("/redfish/v1/")
	if err != nil {
		return nil, fmt.Errorf("error parsing the URL: %w", err)
	}
	return &IloClient{parsedUrl, username, password}, nil
}
