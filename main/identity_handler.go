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
	"fmt"

	"github.com/google/uuid"

	log "github.com/sirupsen/logrus"
)

type IdentityHandler struct {
	protocol            *Protocol
	subjectCountry      string
	subjectOrganization string
}

type Identity struct {
	//Tenant     string    `json:"tenant"`
	//Category   string    `json:"category"`
	//Poc        string    `json:"poc"` // can be empty
	Uid        uuid.UUID `json:"uuid"`
	PrivateKey []byte    `json:"privKey"`
	PublicKey  []byte    `json:"pubKey"`
	AuthToken  string    `json:"token"`
}

func (i *IdentityHandler) initIdentities(identities []Identity) error {
	// create and register keys for identities
	log.Debugf("initializing %d identities...", len(identities))
	for _, id := range identities {
		// check if identity is already initialized
		exists, err := i.protocol.ExistsPrivateKey(id.Uid)
		if err != nil {
			return fmt.Errorf("can not check existing context for %s: %s", id.Uid, err)
		}

		if exists {
			// already initialized
			log.Debugf("%s already initialized (skip)", id.Uid)
			continue
		}

		// make sure identity has an auth token
		if len(id.AuthToken) == 0 {
			return fmt.Errorf("missing auth token for identity %s", id.Uid)
		}

		_, err = i.initIdentity(id)
		if err != nil {
			return err
		}
	}

	return nil
}

func (i *IdentityHandler) initIdentity(id Identity) (csr []byte, err error) {
	log.Infof("initializing new identity %s", id.Uid)

	// generate a new key pair
	privKeyPEM, err := i.protocol.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("generating new key for UUID %s failed: %v", id.Uid, err)
	}

	pubKeyPEM, err := i.protocol.GetPublicKeyFromPrivateKey(privKeyPEM)
	if err != nil {
		return nil, err
	}

	// store key pair
	err = i.protocol.SetPrivateKey(id.Uid, privKeyPEM)
	if err != nil {
		return nil, err
	}

	err = i.protocol.SetPublicKey(id.Uid, pubKeyPEM)
	if err != nil {
		return nil, err
	}

	// register public key at the ubirch backend
	return i.registerPublicKey(privKeyPEM, id.Uid)
}

func (i *IdentityHandler) registerPublicKey(privKeyPEM []byte, uid uuid.UUID) (csr []byte, err error) {
	keyRegistration, err := i.protocol.GetSignedKeyRegistration(privKeyPEM, uid)
	if err != nil {
		return nil, fmt.Errorf("error creating public key certificate: %v", err)
	}
	log.Debugf("%s: key certificate: %s", uid, keyRegistration)

	csr, err = i.protocol.GetCSR(privKeyPEM, uid, i.subjectCountry, i.subjectOrganization)
	if err != nil {
		return nil, fmt.Errorf("creating CSR for UUID %s failed: %v", uid, err)
	}
	log.Debugf("%s: CSR [der]: %x", uid, csr)

	err = i.protocol.SubmitKeyRegistration(uid, keyRegistration, "")
	if err != nil {
		return nil, fmt.Errorf("key registration for UUID %s failed: %v", uid, err)
	}

	go i.submitCSROrLogError(uid, csr)

	return csr, nil
}

func (i *IdentityHandler) submitCSROrLogError(uid uuid.UUID, csr []byte) {
	err := i.protocol.SubmitCSR(uid, csr)
	if err != nil {
		log.Errorf("submitting CSR for UUID %s failed: %v", uid, err)
	}
}
