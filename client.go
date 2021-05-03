// Copyright (c) 2019-2020 ubirch GmbH
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"path"

	"github.com/google/uuid"
	"github.com/ubirch/ubirch-protocol-go/ubirch/v2"

	log "github.com/sirupsen/logrus"
)

type Client struct {
	signingServiceURL  string
	keyServiceURL      string
	identityServiceURL string
}

// requestPublicKeys requests a devices public keys at the identity service
// returns a list of the retrieved public key certificates
func (c *Client) requestPublicKeys(id uuid.UUID) ([]ubirch.SignedKeyRegistration, error) {
	endpoint := path.Join(c.keyServiceURL, "current/hardwareId", id.String())
	resp, err := http.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve public key info: %v", err)
	}
	//noinspection GoUnhandledErrorResult
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return []ubirch.SignedKeyRegistration{}, nil
	}

	if httpFailed(resp.StatusCode) {
		respContent, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("retrieving public key info from %s failed: (%s) %s", endpoint, resp.Status, string(respContent))
	}

	respBodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read response body: %v", err)
	}

	var keys []ubirch.SignedKeyRegistration
	err = json.Unmarshal(respBodyBytes, &keys)
	if err != nil {
		return nil, fmt.Errorf("unable to decode key registration info: %v", err)
	}

	return keys, nil
}

// isKeyRegistered sends a request to the identity service to determine
// if a specified public key is registered for the specified UUID
func (c *Client) isKeyRegistered(id uuid.UUID, pubKey []byte) (bool, error) {
	certs, err := c.requestPublicKeys(id)
	if err != nil {
		return false, err
	}

	for _, cert := range certs {
		if cert.PubKeyInfo.PubKey == base64.StdEncoding.EncodeToString(pubKey) {
			return true, nil
		}
	}
	return false, nil
}

func (c *Client) submitKeyRegistration(uid uuid.UUID, cert []byte) error {
	log.Debugf("%s: registering public key at key service", uid)

	keyRegHeader := map[string]string{
		//"x-ubirch-hardware-id": uid.String(),
		"content-type": "application/json",
	}

	resp, err := post(c.keyServiceURL, cert, keyRegHeader)
	if err != nil {
		return fmt.Errorf("error sending key registration: %v", err)
	}

	if httpFailed(resp.StatusCode) {
		return fmt.Errorf("key registration failed: (%d) %q", resp.StatusCode, resp.Content)
	}

	log.Debugf("%s: key registration successful: (%d) %s", uid, resp.StatusCode, string(resp.Content))

	return nil
}

// submitCSR submits a X.509 Certificate Signing Request for the public key to the identity service
func (c *Client) submitCSR(uid uuid.UUID, csr []byte) error {
	log.Debugf("%s: submitting CSR to identity service", uid)

	CSRHeader := map[string]string{"content-type": "application/octet-stream"}

	resp, err := post(c.identityServiceURL, csr, CSRHeader)
	if err != nil {
		return fmt.Errorf("error sending CSR: %v", err)
	}

	if httpFailed(resp.StatusCode) {
		return fmt.Errorf("request to %s failed: (%d) %q", c.identityServiceURL, resp.StatusCode, resp.Content)
	}

	log.Debugf("%s: CSR submitted: (%d) %s", uid, resp.StatusCode, string(resp.Content))

	return nil
}

func (c *Client) sendToUbirchCloudClient(uid uuid.UUID, auth string, upp []byte) (HTTPResponse, error) {
	endpoint := path.Join(c.signingServiceURL, uid.String(), "hash")
	return post(endpoint, upp, UCCHeader(auth))
}

// post submits a message to a backend service
// returns the response or encountered errors
func post(serviceURL string, data []byte, header map[string]string) (HTTPResponse, error) {
	client := &http.Client{Timeout: BackendRequestTimeout}

	req, err := http.NewRequest(http.MethodPost, serviceURL, bytes.NewBuffer(data))
	if err != nil {
		return HTTPResponse{}, fmt.Errorf("can't make new post request: %v", err)
	}

	for k, v := range header {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return HTTPResponse{}, err
	}

	//noinspection GoUnhandledErrorResult
	defer resp.Body.Close()

	respBodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return HTTPResponse{}, err
	}

	return HTTPResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		Content:    respBodyBytes,
	}, nil
}

func UCCHeader(auth string) map[string]string {
	return map[string]string{
		"x-auth-token": auth,
		"content-type": "application/octet-stream",
	}
}

func httpFailed(StatusCode int) bool {
	return !httpSuccess(StatusCode)
}

func httpSuccess(StatusCode int) bool {
	return StatusCode >= 200 && StatusCode < 300
}
