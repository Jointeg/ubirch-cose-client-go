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
	"net/http"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	log "github.com/sirupsen/logrus"
)

const (
	BackendRequestTimeout = 10 * time.Second // time after which requests to the ubirch backend will be canceled
	GatewayTimeout        = 20 * time.Second // time after which the client sends a 504 response if no timely response could be produced
	ShutdownTimeout       = 25 * time.Second // time after which the server will be shut down forcefully if graceful shutdown did not happen before
	ReadTimeout           = 1 * time.Second  // maximum duration for reading the entire request -> low since we only expect requests with small content
	WriteTimeout          = 30 * time.Second // time after which the connection will be closed if response was not written -> this should never happen
	IdleTimeout           = 60 * time.Second // time to wait for the next request when keep-alives are enabled
)

type HTTPServer struct {
	router   *chi.Mux
	addr     string
	TLS      bool
	certFile string
	keyFile  string
}

func NewRouter() *chi.Mux {
	router := chi.NewMux()
	router.Use(middleware.Timeout(GatewayTimeout))
	return router
}

func (srv *HTTPServer) Serve(ctx context.Context) error {
	server := &http.Server{
		Addr:         srv.addr,
		Handler:      srv.router,
		ReadTimeout:  ReadTimeout,
		WriteTimeout: WriteTimeout,
		IdleTimeout:  IdleTimeout,
	}
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())

	go func() {
		<-ctx.Done()
		server.SetKeepAlivesEnabled(false) // disallow clients to create new long-running conns

		shutdownWithTimeoutCtx, _ := context.WithTimeout(shutdownCtx, ShutdownTimeout)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownWithTimeoutCtx); err != nil {
			log.Warnf("could not gracefully shut down server: %s", err)
		} else {
			log.Debug("shut down HTTP server")
		}
	}()

	log.Infof("starting HTTP server")

	var err error
	if srv.TLS {
		err = server.ListenAndServeTLS(srv.certFile, srv.keyFile)
	} else {
		err = server.ListenAndServe()
	}
	if err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("error starting HTTP server: %v", err)
	}

	// wait for server to shut down gracefully
	<-shutdownCtx.Done()
	return nil
}