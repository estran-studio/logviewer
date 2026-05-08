package factory_test

import (
	"testing"

	"github.com/estran-studio/logviewer/pkg/log/client/config"
	"github.com/estran-studio/logviewer/pkg/log/factory"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/stretchr/testify/assert"
)

func TestGetLogBackendFactory(t *testing.T) {
	clients := config.Clients{
		"local-client": config.Client{
			Type: "local",
		},
		"invalid-client": config.Client{
			Type: "unknown",
		},
	}

	t.Run("creates factory with valid clients", func(t *testing.T) {
		// Note: GetLogBackendFactory only fails if it encounters an invalid type
		// during factory creation (not lazy Get).
		validClients := config.Clients{
			"local": config.Client{Type: "local"},
		}
		f, err := factory.GetLogBackendFactory(validClients)
		assert.NoError(t, err)
		assert.NotNil(t, f)

		// Test lazy retrieval
		b, err := f.Get("local")
		assert.NoError(t, err)
		assert.NotNil(t, b)
	})

	t.Run("fails on invalid client type", func(t *testing.T) {
		f, err := factory.GetLogBackendFactory(clients)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid type for client : unknown")
		assert.Nil(t, f)
	})

	t.Run("docker client initialization", func(t *testing.T) {
		dockerClients := config.Clients{
			"docker": config.Client{
				Type: "docker",
				Options: ty.MI{
					"host": "unix:///tmp/docker.sock",
				},
			},
		}
		f, err := factory.GetLogBackendFactory(dockerClients)
		assert.NoError(t, err)
		assert.NotNil(t, f)

		// We don't necessarily want to call Get("docker") as it might fail if no docker socket exists,
		// but the factory creation itself should work.
	})

	t.Run("splunk client initialization", func(t *testing.T) {
		splunkClients := config.Clients{
			"splunk": config.Client{
				Type: "splunk",
				Options: ty.MI{
					"url": "http://localhost:8089",
				},
			},
		}
		f, err := factory.GetLogBackendFactory(splunkClients)
		assert.NoError(t, err)
		assert.NotNil(t, f)
	})

	t.Run("opensearch client initialization", func(t *testing.T) {
		osClients := config.Clients{
			"opensearch": config.Client{
				Type: "opensearch",
				Options: ty.MI{
					"endpoint": "http://localhost:9200",
				},
			},
		}
		f, err := factory.GetLogBackendFactory(osClients)
		assert.NoError(t, err)
		assert.NotNil(t, f)
	})

	t.Run("kibana client initialization", func(t *testing.T) {
		kbClients := config.Clients{
			"kibana": config.Client{
				Type: "kibana",
				Options: ty.MI{
					"endpoint": "http://localhost:5601",
				},
			},
		}
		f, err := factory.GetLogBackendFactory(kbClients)
		assert.NoError(t, err)
		assert.NotNil(t, f)
	})

	t.Run("k8s client initialization", func(t *testing.T) {
		k8sClients := config.Clients{
			"k8s": config.Client{
				Type: "k8s",
				Options: ty.MI{
					"kubeConfig": "/tmp/config",
				},
			},
		}
		f, err := factory.GetLogBackendFactory(k8sClients)
		assert.NoError(t, err)
		assert.NotNil(t, f)
	})

	t.Run("local client initialization", func(t *testing.T) {
		localClients := config.Clients{
			"local": config.Client{
				Type: "local",
			},
		}
		f, err := factory.GetLogBackendFactory(localClients)
		assert.NoError(t, err)
		assert.NotNil(t, f)

		b, err := f.Get("local")
		assert.NoError(t, err)
		assert.NotNil(t, b)
	})

	t.Run("docker client initialization without host", func(t *testing.T) {
		dockerClients := config.Clients{
			"docker": config.Client{
				Type:    "docker",
				Options: ty.MI{},
			},
		}
		f, err := factory.GetLogBackendFactory(dockerClients)
		assert.NoError(t, err)
		assert.NotNil(t, f)
	})

	t.Run("splunk client with auth", func(t *testing.T) {
		splunkClients := config.Clients{
			"splunk": config.Client{
				Type: "splunk",
				Options: ty.MI{
					"url":  "http://splunk:8089",
					"auth": ty.MS{"Authorization": "Bearer token"},
				},
			},
		}
		f, err := factory.GetLogBackendFactory(splunkClients)
		assert.NoError(t, err)
		assert.NotNil(t, f)
	})

	t.Run("kibana client", func(t *testing.T) {
		kbClients := config.Clients{
			"kibana": config.Client{
				Type:    "kibana",
				Options: ty.MI{"endpoint": "http://kibana:5601"},
			},
		}
		f, err := factory.GetLogBackendFactory(kbClients)
		assert.NoError(t, err)
		assert.NotNil(t, f)
	})

	t.Run("opensearch client", func(t *testing.T) {
		osClients := config.Clients{
			"opensearch": config.Client{
				Type:    "opensearch",
				Options: ty.MI{"endpoint": "http://os:9200"},
			},
		}
		f, err := factory.GetLogBackendFactory(osClients)
		assert.NoError(t, err)
		assert.NotNil(t, f)
	})

	t.Run("k8s client", func(t *testing.T) {
		k8sClients := config.Clients{
			"k8s": config.Client{
				Type:    "k8s",
				Options: ty.MI{"kubeConfig": "/path/to/cfg"},
			},
		}
		f, err := factory.GetLogBackendFactory(k8sClients)
		assert.NoError(t, err)
		assert.NotNil(t, f)
	})

	t.Run("k8s client with insecureSkipTLSVerify", func(t *testing.T) {
		k8sClients := config.Clients{
			"k8s-insecure": config.Client{
				Type: "k8s",
				Options: ty.MI{
					"kubeConfig":            "/path/to/cfg",
					"insecureSkipTLSVerify": true,
				},
			},
		}
		f, err := factory.GetLogBackendFactory(k8sClients)
		assert.NoError(t, err)
		assert.NotNil(t, f)
	})

	t.Run("ssh client initialization", func(t *testing.T) {
		sshClients := config.Clients{
			"ssh": config.Client{
				Type: "ssh",
				Options: ty.MI{
					"user":       "testuser",
					"addr":       "localhost:22",
					"privateKey": "/path/to/key",
				},
			},
		}
		f, err := factory.GetLogBackendFactory(sshClients)
		assert.NoError(t, err)
		assert.NotNil(t, f)
	})

	t.Run("cloudwatch client initialization", func(t *testing.T) {
		cwClients := config.Clients{
			"cloudwatch": config.Client{
				Type: "cloudwatch",
				Options: ty.MI{
					"region": "us-east-1",
				},
			},
		}
		f, err := factory.GetLogBackendFactory(cwClients)
		assert.NoError(t, err)
		assert.NotNil(t, f)
	})
}
