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

	"github.com/go-chi/chi"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/ubirch/ubirch-client-go/main/auditlogger"

	log "github.com/sirupsen/logrus"
	h "github.com/ubirch/ubirch-client-go/main/adapters/httphelper"
	p "github.com/ubirch/ubirch-client-go/main/prometheus"
)

const (
	AuthHeader = "X-Auth-Token"

	UUIDKey      = "uuid"
	CBORPath     = "/cbor"
	HashEndpoint = "/hash"

	BinType  = "application/octet-stream"
	TextType = "text/plain"
	JSONType = "application/json"
	CBORType = "application/cbor"

	HexEncoding = "hex"

	HashLen = 32
)

var UUIDPath = fmt.Sprintf("/{%s}", UUIDKey)

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
}

func (s *COSEService) directUUID() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, err := getUUID(r)
		if err != nil {
			log.Warn(err)
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		s.handleRequest(w, r, uid)
	}
}

func (s *COSEService) handleRequest(w http.ResponseWriter, r *http.Request, uid uuid.UUID) {
	identity, err := s.GetIdentity(uid)
	if err == ErrNotExist {
		h.Error(uid, w, fmt.Errorf("unknown UUID"), http.StatusNotFound)
		return
	}
	if err != nil {
		log.Errorf("%s: %v", uid, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	err = checkAuth(r, identity.AuthToken)
	if err != nil {
		Error(uid, w, err, http.StatusUnauthorized)
		return
	}

	msg := HTTPRequest{ID: uid}

	msg.Payload, msg.Hash, err = s.getPayloadAndHash(r)
	if err != nil {
		Error(msg.ID, w, err, http.StatusBadRequest)
		return
	}

	timer := prometheus.NewTimer(p.SignatureCreationDuration)
	resp := s.Sign(msg, identity.PrivateKey)
	timer.ObserveDuration()

	sendResponse(w, resp)

	if h.HttpSuccess(resp.StatusCode) {
		infos := fmt.Sprintf("\"hwDeviceId\":\"%s\", \"hash\":\"%s\"", msg.ID, base64.StdEncoding.EncodeToString(msg.Hash[:]))
		auditlogger.AuditLog("create", "COSE", infos)

		p.SignatureCreationCounter.Inc()
	}
}

func (s *COSEService) getPayloadAndHash(r *http.Request) (payload []byte, hash Sha256Sum, err error) {
	rBody, err := readBody(r)
	if err != nil {
		return nil, Sha256Sum{}, err
	}

	if isHashRequest(r) { // request contains hash
		hash, err = getHashFromHashRequest(r.Header, rBody)
		return rBody, hash, err
	} else { // request contains original data
		return s.getPayloadAndHashFromDataRequest(r.Header, rBody)
	}
}

func (s *COSEService) getPayloadAndHashFromDataRequest(header http.Header, data []byte) (payload []byte, hash Sha256Sum, err error) {
	switch ContentType(header) {
	case JSONType:
		data, err = s.GetCBORFromJSON(data)
		if err != nil {
			return nil, Sha256Sum{}, fmt.Errorf("unable to CBOR encode JSON object: %v", err)
		}
		log.Debugf("CBOR encoded JSON: %x", data)

		fallthrough
	case CBORType:
		toBeSigned, err := s.GetSigStructBytes(data)
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

// getUUID returns the UUID parameter from the request URL
func getUUID(r *http.Request) (uuid.UUID, error) {
	uuidParam := chi.URLParam(r, UUIDKey)
	uid, err := uuid.Parse(uuidParam)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid UUID: \"%s\": %v", uuidParam, err)
	}
	return uid, nil
}

// checkAuth checks the auth token from the request header
// Returns error if auth token is not correct
func checkAuth(r *http.Request, correctAuthToken string) error {
	if r.Header.Get(AuthHeader) != correctAuthToken {
		return fmt.Errorf("invalid auth token")
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
