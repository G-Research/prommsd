// Binary prommsd runs a HTTP server to receive alerts from Alertmanager and
// pass them to an alert hook.
//
// The alert handler will be available at '/alert' on the address this listens
// on (e.g. http://localhost:9799/alert).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime/debug"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/semconv/v1.12.0"

	"github.com/G-Research/prommsd/pkg/alertchecker"
	"github.com/G-Research/prommsd/pkg/alerthook"
	"github.com/G-Research/prommsd/pkg/tracing"
)

var (
	flagListenAddr  = flag.String("listen", ":9799", "Where to listen for HTTP requests")
	flagExternalURL = flag.String("external-url", "", "URL where this is accessible to users")
	flagVersion     = flag.Bool("version", false, "Print version information")
)

func main() {
	flag.Parse()

	if *flagVersion {
		showVersion()
		os.Exit(0)
	}

	ctx := context.Background()

	shutdownTracing, err := tracing.SetProviderFromEnv(
		ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String("prommsd"),
			semconv.ServiceNamespaceKey.String("github.com/G-Research"),
		),
	)
	if err != nil {
		log.Fatalf("Cannot initialise tracing: %v", err)
	}
	defer shutdownTracing(ctx)

	reg := prometheus.DefaultRegisterer
	reg.MustRegister(prometheus.NewBuildInfoCollector())

	externalURL := *flagExternalURL
	if len(externalURL) == 0 {
		if (*flagListenAddr)[0] == ':' {
			externalURL = "http://localhost" + *flagListenAddr
		} else {
			externalURL = "http://" + *flagListenAddr
		}
	}

	alertChecker := alertchecker.New(reg, externalURL)
	alerthook.Serve(*flagListenAddr, alertChecker, reg)
}

func showVersion() {
	if bi, ok := debug.ReadBuildInfo(); ok {
		fmt.Fprintf(os.Stderr, "https://%v version: %v\n", bi.Main.Path, bi.Main.Version)
	} else {
		fmt.Fprintf(os.Stderr, "No version info\n")
	}
}
