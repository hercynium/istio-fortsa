package istiodata

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	envoyDiscovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	envoyStatus "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"

	istioModel "istio.io/istio/pilot/pkg/model"
	istioXds "istio.io/istio/pilot/pkg/xds"
	istioKube "istio.io/istio/pkg/kube"
	istioLog "istio.io/istio/pkg/log"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sClient "k8s.io/client-go/kubernetes"
	k8sClientCmd "k8s.io/client-go/tools/clientcmd"
	k8sCtrlLog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/hercynium/istio-fortsa/internal/util/istio/newproxystatus"
	"github.com/hercynium/istio-fortsa/internal/util/istio/tags"
)

type IstioData struct {
	mu                   sync.Mutex
	ProxyStatuses        []IstioProxyStatusData
	TagInfo              map[string]tags.TagDescription // key is tag name
	LastUpdate           time.Time
	RevisionsByNamespace map[string]string
}

type IstioProxyStatusData struct {
	ClusterID           string
	IstiodPodName       string
	IstiodPodNamespace  string
	IstiodRevision      string
	IstiodVersion       string
	ProxiedPodId        string
	ProxiedPodName      string
	ProxiedPodNamespace string
}

func (id *IstioData) PrintRevisionTagInfo(ctx context.Context) {
	//log := log.FromContext(ctx)
	for _, t := range id.TagInfo {
		fmt.Printf("%s\t%s\t%s\n", t.Tag, t.Revision, strings.Join(t.Namespaces, ","))
	}
}

func (id *IstioData) PrintProxyStatusData(ctx context.Context) {
	//log := log.FromContext(ctx)
	for _, sd := range id.ProxyStatuses {
		fmt.Printf("[%v, %v, %v, %v]\n", sd.IstiodPodName, sd.IstiodRevision, sd.ProxiedPodName, sd.ProxiedPodNamespace)
	}
}

func (id *IstioData) RefreshIstioData(ctx context.Context, kubeClient *k8sClient.Clientset) error {
	log := k8sCtrlLog.FromContext(ctx)

	if !id.mu.TryLock() {
		log.Info("The data is currently being updated")
		return nil
	}
	defer id.mu.Unlock()

	// if it's been less than 10 minutes since the last update, don't update again...
	duration, _ := time.ParseDuration("-10m")
	if id.LastUpdate.After(time.Now().Add(duration)) && false {
		log.Info("Not updating istio data because not enough time has passed")
		return nil
	} else {
		log.Info("Updating Istio Data...")
	}

	if id.TagInfo == nil {
		id.TagInfo = make(map[string]tags.TagDescription) // TODO: make a proper constructor, call it in main.go
	}
	if id.RevisionsByNamespace == nil {
		id.RevisionsByNamespace = make(map[string]string)
	}
	ti, err := GetRevisionTagInfo(ctx, kubeClient)
	if err != nil {
		log.Error(err, "Couldn't update tag info")
		return err
	}
	for _, t := range ti {
		// store the tag info by tag (multiple tags may have the same revision)
		id.TagInfo[t.Tag] = t
		// map of k8s namespace to istio revision
		for _, ns := range t.Namespaces {
			id.RevisionsByNamespace[ns] = t.Revision
		}
	}
	//PrintRevisionTagInfo(ctx, id.TagInfo)

	sd, err := GetNewProxyStatusData(ctx, kubeClient)
	if err != nil {
		log.Error(err, "Couldn't update proxy status data")
		return err
	}
	id.ProxyStatuses = sd
	//PrintProxyStatusData(ctx, id.ProxyStatuses)

	id.LastUpdate = time.Now()
	log.Info("Updated Istio Data")

	return err
}

// TODO: allow passing namespace to limit search to one namespace? Would be useful to call from namespace controller
func (r *IstioData) CheckProxiedPods(ctx context.Context, kubeClient *k8sClient.Clientset) ([]corev1.Pod, error) {
	log := k8sCtrlLog.FromContext(ctx)
	log.Info("Checking proxied pods")
	var oldPods = []corev1.Pod{}
	for _, ps := range r.ProxyStatuses {
		confRev := r.RevisionsByNamespace[ps.ProxiedPodNamespace]
		if confRev == "" {
			continue // skip if namespace isn't labeled with a revision
		}
		// TODO: might be useful to check pod for sidecar.istio.io/inject=false and skip those?
		if confRev != ps.IstiodRevision {
			log.Info("Outdated Pod Found", "pod", ps.ProxiedPodName, "ns", ps.ProxiedPodNamespace, "proxyIstioRev", ps.IstiodRevision, "nsIstioRev", confRev)
			// TODO: here is where we label the pod
			pod, err := kubeClient.CoreV1().Pods(ps.ProxiedPodNamespace).Get(ctx, ps.ProxiedPodName, metav1.GetOptions{})
			if err != nil {
				log.Error(err, "Couldn't retrieve pod from api, continuing...")
				continue //return nil, err
			}
			oldPods = append(oldPods, *pod)
		}
	}
	log.Info(fmt.Sprintf("Found %d outdated pods", len(oldPods)))
	return oldPods, nil
}

func GetRevisionTagInfo(ctx context.Context, kubeClient *k8sClient.Clientset) ([]tags.TagDescription, error) {
	log := k8sCtrlLog.FromContext(ctx)
	// get tag -> revision -> namespaces mapping
	tagInfo, err := tags.GetTags(ctx, kubeClient)
	if err != nil {
		log.Error(err, "Couldn't get Istio Revision Tag Info")
		return nil, err
	}
	return tagInfo, nil
}

// do the same thing as the cli command `istioctl proxy-status` but without shelling out.
func GetNewProxyStatusData(ctx context.Context, _ *k8sClient.Clientset) ([]IstioProxyStatusData, error) {
	log := k8sCtrlLog.FromContext(ctx)

	// istio library calls have their own logging. Configure it here.
	e := istioLog.Configure(defaultIstioLogOptions())
	if e != nil {
		log.Error(e, "Failed to init istio library logger")
		return nil, e
	}

	istioNamespace := "istio-system"

	// TODO: set these options from configuration using something like viper
	timeout, _ := time.ParseDuration("30s")
	centralOpts := newproxystatus.CentralControlPlaneOptions{
		XdsPodPort: 15012,
		Timeout:    timeout,
	}

	kubeClient, e := getDefaultKubeCLIClient()
	if e != nil {
		log.Error(e, "Failed to init kubeClient")
		return nil, e
	}

	xdsRequest := &envoyDiscovery.DiscoveryRequest{
		TypeUrl: istioXds.TypeDebugSyncronization,
	}

	// this isn't really necessary but we're keeping it for compatibility with the code
	// copied from istio's source into the newproxystatus package.
	multiXdsOpts := newproxystatus.Options{
		MessageWriter: os.Stderr,
	}

	// call the code we copied from istioctl proxystatus and modified so we can use it like a library
	xdsResponses, e := newproxystatus.AllRequestAndProcessXds(xdsRequest, centralOpts, istioNamespace, "", "", kubeClient, multiXdsOpts)
	if e != nil {
		log.Error(e, "Failed to get proxyStatus")
		return nil, e
	}

	return getIstioProxyStatusData(xdsResponses, istioNamespace)
}

// config for internal logging in istio code
// code lifted from: https://github.com/istio/istio/blob/1.25.0/istioctl/pkg/root/root.go#L38
func defaultIstioLogOptions() *istioLog.Options {
	o := istioLog.DefaultOptions()
	// Default to warning for everything; we usually don't want logs in istioctl
	o.SetDefaultOutputLevel("all", istioLog.WarnLevel)
	// These scopes are too noisy even at warning level
	o.SetDefaultOutputLevel("validation", istioLog.ErrorLevel)
	o.SetDefaultOutputLevel("processing", istioLog.ErrorLevel)
	o.SetDefaultOutputLevel("kube", istioLog.ErrorLevel)
	return o
}

// get a working default k8s client we can pass to istio library colls
// code lifted from: https://pkg.go.dev/k8s.io/client-go@v0.32.1/tools/clientcmd
func getDefaultKubeCLIClient() (istioKube.CLIClient, error) {
	loadingRules := k8sClientCmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &k8sClientCmd.ConfigOverrides{}
	kubeConfig := k8sClientCmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, err
	}
	return istioKube.NewCLIClient(istioKube.NewClientConfigForRestConfig(config))
}

func getIstioProxyStatusData(xdsResponses map[string]*envoyDiscovery.DiscoveryResponse, istioNamespace string) ([]IstioProxyStatusData, error) {
	istioProxyStatusData := []IstioProxyStatusData{}
	for _, response := range xdsResponses {
		for _, resource := range response.Resources {
			clientConfig := envoyStatus.ClientConfig{}
			err := resource.UnmarshalTo(&clientConfig)
			if err != nil {
				return nil, fmt.Errorf("could not unmarshal ClientConfig: %v", err)
			}
			meta, err := istioModel.ParseMetadata(clientConfig.GetNode().GetMetadata())
			if err != nil {
				return nil, fmt.Errorf("could not parse node metadata: %v", err)
			}
			cp := newproxystatus.CpInfo(response)
			istioProxyStatusData = append(istioProxyStatusData, IstioProxyStatusData{
				ClusterID:           meta.ClusterID.String(),
				IstiodPodName:       cp.ID,
				IstiodPodNamespace:  istioNamespace, // TODO: figure out how to get this dynamically
				IstiodRevision:      regexp.MustCompile(`^istiod-(.*)-[^-]+-[^-]+$`).ReplaceAllString(cp.ID, `$1`),
				IstiodVersion:       meta.IstioVersion,
				ProxiedPodId:        clientConfig.GetNode().GetId(),
				ProxiedPodName:      strings.TrimSuffix(clientConfig.GetNode().GetId(), "."+meta.Namespace),
				ProxiedPodNamespace: meta.Namespace,
			})
		}
	}
	return istioProxyStatusData, nil
}
