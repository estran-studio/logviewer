// Package k8s provides a Kubernetes implementation of the LogClient interface.
package k8s

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/reader"
	"github.com/estran-studio/logviewer/pkg/ty"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	// Import all auth plugins (incl. exec, OIDC, GCP, Azure, etc.) so kubeconfigs
	// referencing them (e.g. auth-provider: oidc) are supported without extra code.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

const (
	// FieldNamespace is the field name for Kubernetes namespace.
	FieldNamespace = "namespace"
	// FieldContainer is the field name for Kubernetes container.
	FieldContainer = "container"
	// FieldPrevious is the field name for fetching previous pod logs.
	FieldPrevious = "previous"
	// FieldPod is the field name for the pod.
	FieldPod = "pod"
	// FieldLabelSelector is the field name for the label selector.
	FieldLabelSelector = "labelSelector"

	// OptionsTimestamp is the key for the timestamp option.
	OptionsTimestamp = "timestamp"
)

// LogClientOptions defines configuration for the Kubernetes client.
type LogClientOptions struct {
	KubeConfig            string `json:"kubeConfig"`
	InsecureSkipTLSVerify bool   `json:"insecureSkipTLSVerify"`
}

/*

* Need to support regex for pod name , to be able to get all pods from a deployment or someting
* similar and get them in the same log flow to maybe parse them afterwards
 */

type k8sLogClient struct {
	clientset *kubernetes.Clientset
}

func (lc k8sLogClient) Get(ctx context.Context, search *client.LogSearch) (client.LogSearchResult, error) {

	namespace := search.Options.GetString(FieldNamespace)
	pod := search.Options.GetString(FieldPod)
	labelSelector := search.Options.GetString(FieldLabelSelector)
	container := search.Options.GetString(FieldContainer)
	previous := search.Options.GetBool(FieldPrevious)
	timestamp := search.Options.GetBool(OptionsTimestamp)

	follow := search.Follow

	// Handle tailLines: if size is not set, use nil to get all logs
	var tailLines *int64
	if search.Size.Set && search.Size.Value > 0 {
		lines := int64(search.Size.Value)
		tailLines = &lines
	}

	// If labelSelector is provided, query multiple pods
	if labelSelector != "" {
		return lc.getLogsFromMultiplePods(ctx, search, namespace, labelSelector, previous, timestamp, follow, tailLines)
	}

	// Single pod query (original behavior)
	if pod == "" {
		return nil, errors.New("either 'pod' or 'labelSelector' must be specified")
	}

	ipod := lc.clientset.CoreV1().Pods(namespace)

	logOptions := v1.PodLogOptions{
		TailLines:  tailLines,
		Follow:     follow,
		Timestamps: timestamp,
		Container:  container,
		Previous:   previous,
	}

	if search.Range.Last.Value != "" {
		lastDuration, err := time.ParseDuration(search.Range.Last.Value)
		if err != nil {
			return nil, err
		}
		seconds := int64(lastDuration.Seconds())
		logOptions.SinceSeconds = &seconds
	} else if search.Range.Gte.Value != "" {
		time, err := time.Parse(time.RFC3339, search.Range.Gte.Value)
		if err != nil {
			return nil, err
		}
		metaTime := metav1.NewTime(time)
		logOptions.SinceTime = &metaTime
	}

	req := ipod.GetLogs(pod, &logOptions)

	podLogs, err2 := req.Stream(ctx)
	if err2 != nil {
		return nil, err2
	}

	scanner := bufio.NewScanner(podLogs)

	return reader.GetLogResult(search, scanner, podLogs)
}

// podNameInjector wraps a LogSearchResult and injects the pod name into each log entry's Fields
type podNameInjector struct {
	inner   client.LogSearchResult
	podName string
}

func (p *podNameInjector) GetSearch() *client.LogSearch {
	return p.inner.GetSearch()
}

func (p *podNameInjector) GetEntries(ctx context.Context) ([]client.LogEntry, chan []client.LogEntry, error) {
	entries, ch, err := p.inner.GetEntries(ctx)
	if err != nil {
		return nil, nil, err
	}

	// Inject pod name into all initial entries
	for i := range entries {
		if entries[i].Fields == nil {
			entries[i].Fields = make(ty.MI)
		}
		entries[i].Fields[FieldPod] = p.podName
	}

	// If there's a channel for streaming entries, wrap it
	if ch != nil {
		wrappedCh := make(chan []client.LogEntry)
		go func() {
			defer close(wrappedCh)
			for batch := range ch {
				for i := range batch {
					if batch[i].Fields == nil {
						batch[i].Fields = make(ty.MI)
					}
					batch[i].Fields[FieldPod] = p.podName
				}
				wrappedCh <- batch
			}
		}()
		return entries, wrappedCh, nil
	}

	return entries, nil, nil
}

func (p *podNameInjector) GetFields(ctx context.Context) (ty.UniSet[string], chan ty.UniSet[string], error) {
	return p.inner.GetFields(ctx)
}

func (p *podNameInjector) GetPaginationInfo() *client.PaginationInfo {
	return p.inner.GetPaginationInfo()
}

func (p *podNameInjector) Err() <-chan error {
	return p.inner.Err()
}

// getLogsFromMultiplePods fetches logs from all pods matching the label selector
// and aggregates them using MultiLogSearchResult
func (lc k8sLogClient) getLogsFromMultiplePods(
	ctx context.Context,
	search *client.LogSearch,
	namespace string,
	labelSelector string,
	_ bool,
	_ bool,
	_ bool,
	_ *int64,
) (client.LogSearchResult, error) {

	// List pods matching the label selector
	podList, err := lc.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, err
	}

	if len(podList.Items) == 0 {
		return nil, errors.New("no pods found matching labelSelector: " + labelSelector)
	}

	// If only one pod, optimize by returning a single result
	if len(podList.Items) == 1 {
		podName := podList.Items[0].Name
		singlePodSearch := *search
		singlePodSearch.Options[FieldPod] = podName
		delete(singlePodSearch.Options, FieldLabelSelector)

		result, err := lc.Get(ctx, &singlePodSearch)
		if err != nil {
			return nil, err
		}

		// Wrap the result to inject pod name, ensuring consistent output with the multi-pod case
		if result != nil {
			return &podNameInjector{
				inner:   result,
				podName: podName,
			}, nil
		}
		return result, nil
	}

	// Create multi-result aggregator
	multiResult, err := client.NewMultiLogSearchResult(search)
	if err != nil {
		return nil, err
	}

	// Query logs from each pod concurrently
	var wg sync.WaitGroup
	for _, pod := range podList.Items {
		wg.Add(1)
		go func(podName string) {
			defer wg.Done()

			// Create a copy of the search for this specific pod
			// Deep copy all map fields to prevent race conditions
			podSearch := *search
			podSearch.Options = ty.MergeM(make(ty.MI, len(search.Options)+1), search.Options)
			podSearch.Options[FieldPod] = podName
			podSearch.Fields = ty.MergeM(make(ty.MS, len(search.Fields)), search.Fields)
			podSearch.FieldsCondition = ty.MergeM(make(ty.MS, len(search.FieldsCondition)), search.FieldsCondition)
			if search.Variables != nil {
				podSearch.Variables = make(map[string]client.VariableDefinition, len(search.Variables))
				for k, v := range search.Variables {
					podSearch.Variables[k] = v
				}
			}

			// Remove labelSelector from options to avoid infinite recursion
			delete(podSearch.Options, FieldLabelSelector)

			// Get logs for this pod
			result, err := lc.Get(ctx, &podSearch)

			// Wrap the result to inject pod name into each log entry
			if result != nil {
				result = &podNameInjector{
					inner:   result,
					podName: podName,
				}
			}

			multiResult.Add(result, err)
		}(pod.Name)
	}

	wg.Wait()
	return multiResult, nil
}

func (lc k8sLogClient) GetFieldValues(ctx context.Context, search *client.LogSearch, fields []string) (map[string][]string, error) {
	// For k8s/text-based backends, we need to run a search and extract field values
	result, err := lc.Get(ctx, search)
	if err != nil {
		return nil, err
	}
	return client.GetFieldValuesFromResult(ctx, result, fields)
}

func ensureKubeconfig(kubeconfig string) error {
	if _, err := os.Stat(kubeconfig); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	// Attempt auto-generation only for integration environment pattern
	// Expect running container named k3s-server
	if os.Getenv("LOGVIEWER_AUTO_K3S") == "false" {
		return errors.New("kubeconfig not found and auto-generation disabled: " + kubeconfig)
	}

	// docker cp k3s-server:/etc/rancher/k3s/k3s.yaml <kubeconfig>
	if err := os.MkdirAll(filepath.Dir(kubeconfig), 0o750); err != nil {
		return err
	}
	cmd := exec.Command("docker", "cp", "k3s-server:/etc/rancher/k3s/k3s.yaml", kubeconfig)
	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.New("failed to copy kubeconfig from k3s-server: " + string(out))
	}
	// Replace 127.0.0.1 with localhost to match compose port mapping semantics
	b, err := os.ReadFile(kubeconfig) //nolint:gosec
	if err == nil {
		updated := make([]byte, 0, len(b))
		updated = append(updated, b...)
		// simple replacement (avoid bringing in strings dep already imported indirectly)
		content := string(updated)
		content = strings.ReplaceAll(content, "127.0.0.1", "localhost")
		if err = os.WriteFile(kubeconfig, []byte(content), 0o600); err != nil {
			return err
		}
	}
	return nil
}

// GetLogClient returns a new Kubernetes log client.
func GetLogClient(options LogClientOptions) (client.LogBackend, error) {
	var kubeconfig string
	if options.KubeConfig == "" {
		kubeconfig = filepath.Join(homedir.HomeDir(), ".kube", "config")
	} else {
		kubeconfig = options.KubeConfig
	}

	if err := ensureKubeconfig(kubeconfig); err != nil {
		return nil, err
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}

	// Apply insecureSkipTLSVerify setting if provided
	if options.InsecureSkipTLSVerify {
		config.Insecure = true
		config.CAData = nil
		config.CAFile = ""
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return k8sLogClient{clientset: clientset}, nil
}
