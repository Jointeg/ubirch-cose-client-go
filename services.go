// Copyright (c) 2021 ubirch GmbH
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
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/google/uuid"

	log "github.com/sirupsen/logrus"
)

const (
	AuthHeader     = "X-Auth-Token"
	TenantHeader   = "X-Tenant"
	CategoryHeader = "X-Category"
	PocHeader      = "X-PoC"

	UUIDKey      = "uuid"
	COSEPath     = "cbor"
	HashEndpoint = "hash"

	BinType  = "application/octet-stream"
	TextType = "text/plain"
	JSONType = "application/json"
	CBORType = "application/cbor"

	HexEncoding = "hex"

	HashLen = 32
)

type Sha256Sum [HashLen]byte

type HTTPRequest struct {
	ID      uuid.UUID
	Hash    Sha256Sum
	Payload []byte
}

type HTTPResponse struct {
	StatusCode int         `json:"statusCode"`
	Header     http.Header `json:"header"`
	Content    []byte      `json:"content"`
}

type COSEService struct {
	*CoseSigner
	identities []Identity
}

var _ Service = (*COSEService)(nil)

func (service *COSEService) handleRequest(w http.ResponseWriter, r *http.Request) {
	var msg HTTPRequest
	var err error

	id, err := getIdentity(r, service.identities)
	if err != nil {
		Error(msg.ID, w, err, http.StatusNotFound)
		return
	}

	err = checkAuth(r, id.Token)
	if err != nil {
		Error(msg.ID, w, err, http.StatusUnauthorized)
		return
	}

	msg.Payload, msg.Hash, err = service.getPayloadAndHash(r)
	if err != nil {
		Error(msg.ID, w, err, http.StatusBadRequest)
		return
	}

	resp := service.Sign(msg)

	sendResponse(w, resp)
}

func (service *COSEService) getPayloadAndHash(r *http.Request) (payload []byte, hash Sha256Sum, err error) {
	rBody, err := readBody(r)
	if err != nil {
		return nil, Sha256Sum{}, err
	}

	if isHashRequest(r) { // request contains hash
		hash, err = getHashFromHashRequest(r.Header, rBody)
		return rBody, hash, err
	} else { // request contains original data
		return service.getPayloadAndHashFromDataRequest(r.Header, rBody)
	}
}

func (service *COSEService) getPayloadAndHashFromDataRequest(header http.Header, data []byte) (payload []byte, hash Sha256Sum, err error) {
	switch ContentType(header) {
	case JSONType:
		data, err = service.GetCBORFromJSON(data)
		if err != nil {
			return nil, Sha256Sum{}, fmt.Errorf("unable to CBOR encode JSON object: %v", err)
		}
		log.Debugf("CBOR encoded JSON: %x", data)

		fallthrough
	case CBORType:
		toBeSigned, err := service.GetSigStructBytes(data)
		if err != nil {
			return nil, Sha256Sum{}, err
		}
		log.Debugf("toBeSigned: %x", toBeSigned)

		hash = sha256.Sum256(toBeSigned)
		return data, hash, err
	default:
		return nil, Sha256Sum{}, fmt.Errorf("invalid content-type for original data: "+
			"expected (\"%s\" | \"%s\")", CBORType, JSONType)
	}
}

// wrapper for http.Error that additionally logs the error message to std.Output
func Error(uid uuid.UUID, w http.ResponseWriter, err error, code int) {
	log.Warnf("%s: %v", uid, err)
	http.Error(w, err.Error(), code)
}

// helper function to get "Content-Type" from request header
func ContentType(header http.Header) string {
	return strings.ToLower(header.Get("Content-Type"))
}

// helper function to get "Content-Transfer-Encoding" from request header
func ContentEncoding(header http.Header) string {
	return strings.ToLower(header.Get("Content-Transfer-Encoding"))
}

// getIdentity matches attributes from the request header with a known identity and returns it
func getIdentity(r *http.Request, identities []Identity) (*Identity, error) {
	t := r.Header.Get(TenantHeader)
	if len(t) == 0 {
		return nil, fmt.Errorf("missing header: %s", TenantHeader)
	}
	cat := r.Header.Get(CategoryHeader)
	if len(cat) == 0 {
		return nil, fmt.Errorf("missing header: %s", CategoryHeader)
	}
	poc := r.Header.Get(PocHeader) // can be empty

	for _, i := range identities {
		if t == i.Tenant && cat == i.Category && poc == i.Poc {
			return &i, nil
		}
	}
	return nil, fmt.Errorf(http.StatusText(http.StatusNotFound))
}

// checkAuth checks the auth token from the request header
// Returns error if auth token is not correct
func checkAuth(r *http.Request, correctAuthToken []byte) error {
	if r.Header.Get(AuthHeader) != base64.StdEncoding.EncodeToString(correctAuthToken) {
		return fmt.Errorf(http.StatusText(http.StatusUnauthorized))
	}
	return nil
}

func readBody(r *http.Request) ([]byte, error) {
	rBody, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read request body: %v", err)
	}
	return rBody, nil
}

func isHashRequest(r *http.Request) bool {
	return strings.HasSuffix(r.URL.Path, HashEndpoint)
}

func getHashFromHashRequest(header http.Header, data []byte) (hash Sha256Sum, err error) {
	switch ContentType(header) {
	case TextType:
		if ContentEncoding(header) == HexEncoding {
			data, err = hex.DecodeString(string(data))
			if err != nil {
				return Sha256Sum{}, fmt.Errorf("decoding hex encoded hash failed: %v (%s)", err, string(data))
			}
		} else {
			data, err = base64.StdEncoding.DecodeString(string(data))
			if err != nil {
				return Sha256Sum{}, fmt.Errorf("decoding base64 encoded hash failed: %v (%s)", err, string(data))
			}
		}
		fallthrough
	case BinType:
		if len(data) != HashLen {
			return Sha256Sum{}, fmt.Errorf("invalid SHA256 hash size: "+
				"expected %d bytes, got %d bytes", HashLen, len(data))
		}

		copy(hash[:], data)
		return hash, nil
	default:
		return Sha256Sum{}, fmt.Errorf("invalid content-type for hash: "+
			"expected (\"%s\" | \"%s\")", BinType, TextType)
	}
}

// forwards response to sender
func sendResponse(w http.ResponseWriter, resp HTTPResponse) {
	for k, v := range resp.Header {
		w.Header().Set(k, v[0])
	}
	w.WriteHeader(resp.StatusCode)
	_, err := w.Write(resp.Content)
	if err != nil {
		log.Errorf("unable to write response: %s", err)
	}
}

func errorResponse(code int, message string) HTTPResponse {
	if message == "" {
		message = http.StatusText(code)
	}
	return HTTPResponse{
		StatusCode: code,
		Header:     http.Header{"Content-Type": {"text/plain; charset=utf-8"}},
		Content:    []byte(message),
	}
}
