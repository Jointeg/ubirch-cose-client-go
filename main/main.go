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
	"context"
	"fmt"
	"os"
	"os/signal"
	"path"
	"syscall"

	"github.com/ubirch/ubirch-client-go/main/adapters/handlers"
	"golang.org/x/sync/errgroup"

	log "github.com/sirupsen/logrus"
	h "github.com/ubirch/ubirch-client-go/main/adapters/httphelper"
	prom "github.com/ubirch/ubirch-client-go/main/prometheus"
)

// handle graceful shutdown
func shutdown(cancel context.CancelFunc) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	// block until we receive a SIGINT or SIGTERM
	sig := <-signals
	log.Infof("shutting down after receiving: %v", sig)

	// cancel the go routines contexts
	cancel()
}

var (
	// Version will be replaced with the tagged version during build time
	Version = "local build"
	// Revision will be replaced with the commit hash during build time
	Revision = "unknown"
)

func main() {
	const configFile = "config.json"

	var (
		configDir string
		serverID  = fmt.Sprintf("ubirch-cose-client/%s", Version)
	)

	if len(os.Args) > 1 {
		configDir = os.Args[1]
	}

	log.SetFormatter(&log.JSONFormatter{})
	log.Printf("UBIRCH COSE client (version=%s, revision=%s)", Version, Revision)

	// read configuration
	conf := Config{}
	err := conf.Load(configDir, configFile)
	if err != nil {
		log.Fatalf("ERROR: unable to load configuration: %s", err)
	}

	// create a waitgroup that contains all asynchronous operations
	// a cancellable context is used to stop the operations gracefully
	ctx, cancel := context.WithCancel(context.Background())
	g, ctx := errgroup.WithContext(ctx)

	// set up graceful shutdown handling
	go shutdown(cancel)

	// set up HTTP server
	httpServer := handlers.HTTPServer{
		Router:   handlers.NewRouter(),
		Addr:     conf.TCP_addr,
		TLS:      conf.TLS,
		CertFile: conf.TLS_CertFile,
		KeyFile:  conf.TLS_KeyFile,
	}

	// start HTTP server
	serverReadyCtx, serverReady := context.WithCancel(context.Background())
	g.Go(func() error {
		return httpServer.Serve(ctx, serverReady)
	})
	// wait for server to start
	<-serverReadyCtx.Done()

	// set up metrics
	prom.InitPromMetrics(httpServer.Router)

	// set up endpoint for liveliness checks
	httpServer.Router.Get("/healtz", h.Health(serverID))

	// initialize COSE service
	ctxManager, err := NewFileManager(conf.configDir)
	if err != nil {
		log.Fatal(err)
	}

	client := &ExtendedClient{}
	client.KeyServiceURL = conf.KeyService
	client.IdentityServiceURL = conf.IdentityService
	//todo client.signingServiceURL = conf.SigningService

	protocol, err := NewProtocol(ctxManager, conf.secretBytes, client)
	if err != nil {
		log.Fatal(err)
	}

	idHandler := &IdentityHandler{
		protocol:            protocol,
		subjectCountry:      conf.CSR_Country,
		subjectOrganization: conf.CSR_Organization,
	}

	// generate and register keys for known identities
	err = idHandler.initIdentities(conf.identities)
	if err != nil {
		log.Fatal(err)
	}

	coseSigner, err := NewCoseSigner(protocol)
	if err != nil {
		log.Fatal(err)
	}

	service := &COSEService{
		CoseSigner: coseSigner,
		identities: conf.identities,
	}

	// set up endpoints for COSE signing (UUID as URL parameter)
	directUuidEndpoint := path.Join(UUIDPath, CBORPath) // /<uuid>/cbor
	httpServer.Router.Post(directUuidEndpoint, service.directUUID())

	directUuidHashEndpoint := path.Join(directUuidEndpoint, HashEndpoint) // /<uuid>/cbor/hash
	httpServer.Router.Post(directUuidHashEndpoint, service.directUUID())

	// set up endpoints for COSE signing (UUID via pattern matching)
	matchUuidEndpoint := CBORPath // /cbor
	httpServer.Router.Post(matchUuidEndpoint, service.matchUUID())

	matchUuidHashEndpoint := path.Join(matchUuidEndpoint, HashEndpoint) // /cbor/hash
	httpServer.Router.Post(matchUuidHashEndpoint, service.matchUUID())

	// set up endpoint for readiness checks
	httpServer.Router.Get("/readiness", h.Health(serverID))
	log.Info("ready")

	// wait for all go routines of the waitgroup to return
	if err = g.Wait(); err != nil {
		log.Error(err)
	}

	log.Debug("shut down")
}
