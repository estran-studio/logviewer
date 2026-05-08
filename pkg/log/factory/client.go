// Package factory provides helpers to construct log client factories used by
// the application. It exposes `GetLogBackendFactory` which builds lazily
// initialized clients based on configuration.
package factory

import (
	"errors"
	"runtime"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/client/config"
	"github.com/estran-studio/logviewer/pkg/log/impl/cloudwatch"
	"github.com/estran-studio/logviewer/pkg/log/impl/docker"
	"github.com/estran-studio/logviewer/pkg/log/impl/elk/kibana"
	"github.com/estran-studio/logviewer/pkg/log/impl/elk/opensearch"
	"github.com/estran-studio/logviewer/pkg/log/impl/k8s"
	"github.com/estran-studio/logviewer/pkg/log/impl/local"
	splunk "github.com/estran-studio/logviewer/pkg/log/impl/splunk/logclient"
	"github.com/estran-studio/logviewer/pkg/log/impl/ssh"
	"github.com/estran-studio/logviewer/pkg/ty"
)

const (
	defaultDockerHostWindows = "npipe:////./pipe/docker_engine"
	defaultDockerHostUnix    = "unix:///var/run/docker.sock"
)

// LogBackendFactory provides an abstraction for obtaining a configured
// client.LogBackend by name.
type LogBackendFactory interface {
	Get(name string) (*client.LogBackend, error)
}

type logBackendFactory struct {
	clients ty.LazyMap[string, client.LogBackend]
}

func (lcf *logBackendFactory) Get(name string) (*client.LogBackend, error) {
	return lcf.clients.Get(name)
}

// GetLogBackendFactory returns a factory for creating log clients from configuration.
func GetLogBackendFactory(clients config.Clients) (LogBackendFactory, error) {

	logBackendFactory := new(logBackendFactory)
	logBackendFactory.clients = make(ty.LazyMap[string, client.LogBackend])

	for k, v := range clients {
		// IMPORTANT: shadow loop variable so each closure below captures its own copy.
		v := v
		// Resolve environment variables inside client option values (string only)
		v.Options = v.Options.ResolveVariables()
		switch v.Type {
		case "opensearch":
			options := v.Options
			logBackendFactory.clients[k] = ty.GetLazy(func() (*client.LogBackend, error) {
				vv, err := opensearch.GetClient(opensearch.Target{
					Endpoint: options.GetString("endpoint"),
				})
				if err != nil {
					return nil, err
				}

				return &vv, nil
			})
		case "kibana":
			options := v.Options
			logBackendFactory.clients[k] = ty.GetLazy(func() (*client.LogBackend, error) {
				vv, err := kibana.GetClient(kibana.Target{Endpoint: options.GetString("endpoint")})
				if err != nil {
					return nil, err
				}

				return &vv, nil
			})
		case "local":
			logBackendFactory.clients[k] = ty.GetLazy(func() (*client.LogBackend, error) {
				vv, err := local.GetLogClient()
				if err != nil {
					return nil, err
				}

				return &vv, nil
			})
		case "k8s":
			logBackendFactory.clients[k] = ty.GetLazy(func() (*client.LogBackend, error) {
				vv, err := k8s.GetLogClient(k8s.LogClientOptions{
					KubeConfig:            v.Options.GetString("kubeConfig"),
					InsecureSkipTLSVerify: v.Options.GetBool("insecureSkipTLSVerify"),
				})
				if err != nil {
					return nil, err
				}

				return &vv, nil
			})
		case "ssh":
			logBackendFactory.clients[k] = ty.GetLazy(func() (*client.LogBackend, error) {
				user := v.Options.GetString("user")
				addr := v.Options.GetString("addr")
				pk := v.Options.GetString("privateKey")
				vv, err := ssh.GetLogClient(ssh.LogClientOptions{
					User:       user,
					Addr:       addr,
					PrivateKey: pk,
				})
				if err != nil {
					return nil, err
				}

				return &vv, nil
			})
		case "splunk":
			logBackendFactory.clients[k] = ty.GetLazy(func() (*client.LogBackend, error) {
				authOptions := splunk.SplunkAuthOptions{}
				if authMap, ok := v.Options["auth"].(ty.MI); ok {
					authOptions.Header = authMap.GetMS("header")
				}
				vv, err := splunk.GetClient(splunk.SplunkLogSearchClientOptions{
					URL:        v.Options.GetString("url"),
					Auth:       authOptions,
					Headers:    v.Options.GetMS("headers").ResolveVariables(),
					SearchBody: v.Options.GetMS("searchBody").ResolveVariables(),
				})
				if err != nil {
					return nil, err
				}

				return &vv, nil
			})
		case "docker":
			logBackendFactory.clients[k] = ty.GetLazy(func() (*client.LogBackend, error) {
				host := v.Options.GetString("host")
				if host == "" {
					if runtime.GOOS == "windows" {
						host = defaultDockerHostWindows
					} else {
						host = defaultDockerHostUnix
					}
				}
				vv, err := docker.GetLogClient(host)
				return &vv, err
			})
		case "cloudwatch":
			logBackendFactory.clients[k] = ty.GetLazy(func() (*client.LogBackend, error) {
				// Pass the client-specific options to our new factory function
				vv, err := cloudwatch.GetLogClient(v.Options)
				if err != nil {
					return nil, err
				}
				return &vv, nil
			})
		default:
			return nil, errors.New("invalid type for client : " + v.Type)
		}
	}

	return logBackendFactory, nil
}

// GetLogBackendFactory builds a LogBackendFactory from the provided
// configuration, lazily constructing clients on demand.
