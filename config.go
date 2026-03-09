package main

import (
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/labels"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

type Config struct {
	Logger          zap.Options
	Context         string
	LeaderElection  bool
	MetricsAddr     string
	HealthAddr      string
	Instance        string
	WatchNamespaces []string
	WatchSelector   LabelSelector
	TokenTTL        time.Duration
}

func (c *Config) BindFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.Context, "context", "", "Kubernetes context")
	fs.BoolVar(&c.LeaderElection,
		"leader-election",
		true,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager",
	)
	fs.StringVar(&c.MetricsAddr,
		"metrics-bind-address",
		":8080",
		"The address the metric endpoint binds to",
	)
	fs.StringVar(&c.HealthAddr,
		"health-probe-bind-address",
		":8081",
		"The address the probe endpoint binds to",
	)

	fs.StringVar(&c.Instance,
		"instance",
		"",
		"Instance to populate argocd.fleet.agoda.com/instance annotation",
	)
	fs.StringSliceVar(&c.WatchNamespaces,
		"watch-namespaces",
		nil,
		"Namespaces to watch for Cluster resources",
	)
	fs.Var(&c.WatchSelector,
		"watch-selector",
		"Selector to watch for Cluster resources",
	)

	fs.DurationVar(
		&c.TokenTTL,
		"token-ttl",
		1*time.Hour,
		"Service account bearer token TTL",
	)
	fs.Bool(
		"markdown-help",
		false,
		"Print help in markdown",
	)
	_ = fs.MarkHidden("markdown-help")

	gfs := flag.NewFlagSet("argocd-capi-operator", flag.ExitOnError)
	c.Logger.BindFlags(gfs)
	fs.AddGoFlagSet(gfs)
}

func MarkdownHelp(w io.Writer, fs *pflag.FlagSet) {
	_, _ = fmt.Fprintln(w, "| Name | Default | Usage |")
	_, _ = fmt.Fprintln(w, "| --- | --- | --- |")
	fs.VisitAll(func(f *pflag.Flag) {
		if !f.Hidden {
			_, _ = fmt.Fprintln(w, "|", f.Name, "|", f.DefValue, "|", f.Usage, "|")
		}
	})
}

type LabelSelector struct {
	delegate labels.Selector
}

func (s LabelSelector) Selector() labels.Selector {
	if s.delegate == nil {
		return labels.Everything()
	}

	return s.delegate
}

func (s *LabelSelector) Set(data string) error {
	selector, err := labels.Parse(data)
	if err != nil {
		return err
	}

	s.delegate = selector
	return nil
}

func (s *LabelSelector) String() string {
	return s.Selector().String()
}

func (s *LabelSelector) Type() string {
	return "string"
}
