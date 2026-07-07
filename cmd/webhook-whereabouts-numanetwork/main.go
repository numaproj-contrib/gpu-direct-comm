/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	numaflowv1alpha1 "github.com/numaproj-contrib/gpu-direct-comm/api/v1alpha1"
	"github.com/numaproj-contrib/gpu-direct-comm/internal/ipam"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(numaflowv1alpha1.AddToScheme(scheme))
}

func main() {
	var listenAddr string
	var whereaboutsBin string

	flag.StringVar(&listenAddr, "listen-addr", ":8443", "Address to listen on")
	flag.StringVar(&whereaboutsBin, "whereabouts-bin", "/opt/cni/bin/whereabouts", "Path to whereabouts CNI binary")
	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	log := ctrl.Log.WithName("webhook-whereabouts-numanetwork")

	cfg := ctrl.GetConfigOrDie()
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		log.Error(err, "create kubernetes client")
		os.Exit(1)
	}

	handler := &ipam.Handler{
		Client: c,
		Executor: &ipam.WhereaboutsExecutor{
			BinPath: whereaboutsBin,
			CNIPath: filepath.Dir(whereaboutsBin),
		},
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	go func() {
		log.Info("starting server", "addr", listenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error(err, "server error")
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error(err, "shutdown error")
	}
}
