package util

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	xdsstatus "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"
	"istio.io/istio/istioctl/pkg/multixds"
	"istio.io/istio/pilot/pkg/model"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.infra.cloudera.com/sscaffidi/istio-proxy-update-controller/internal/util/istio/proxystatus"
	"github.infra.cloudera.com/sscaffidi/istio-proxy-update-controller/internal/util/istio/tags"
)

type ICUPReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	KubeClient *kubernetes.Clientset
	IstioData  *IstioData
}

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

func GetRevisionTagInfo(ctx context.Context, kubeClient *kubernetes.Clientset) ([]tags.TagDescription, error) {
	log := log.FromContext(ctx)
	// get tag -> revision -> namespaces mapping
	tagInfo, err := tags.GetTags(ctx, kubeClient)
	if err != nil {
		log.Error(err, "Couldn't get Istio Revision Tag Info")
		return nil, err
	}
	return tagInfo, nil
}

// this should be called from the controllers - it will fetch and process the istio data
func GetProxyStatusData(ctx context.Context, kubeClient *kubernetes.Clientset) ([]IstioProxyStatusData, error) {
	log := log.FromContext(ctx)

	var istioProxyStatusData []IstioProxyStatusData

	// see here for how to navigate the proxy-status data:
	//   https://github.com/istio/istio/blob/master/istioctl/pkg/writer/pilot/status.go
	xdsResponses := proxystatus.GetProxyStatus()
	for _, response := range xdsResponses {
		for _, resource := range response.Resources {
			clientConfig := xdsstatus.ClientConfig{}
			err := resource.UnmarshalTo(&clientConfig)
			if err != nil {
				log.Error(err, "could not unmarshal ClientConfig")
				return nil, err
			}
			meta, err := model.ParseMetadata(clientConfig.GetNode().GetMetadata())
			if err != nil {
				log.Error(err, "could not parse node metadata")
				return nil, err
			}
			cp := multixds.CpInfo(response)
			istioProxyStatusData = append(istioProxyStatusData, IstioProxyStatusData{
				ClusterID:           meta.ClusterID.String(),
				IstiodPodName:       cp.ID,
				IstiodPodNamespace:  "istio-system", // TODO: figure out how to get this dynamically
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

func (id *IstioData) RefreshIstioData(ctx context.Context, kubeClient *kubernetes.Clientset) error {
	log := log.FromContext(ctx)

	if !id.mu.TryLock() {
		log.Info("The data is currently being updated")
		return nil
	}
	defer id.mu.Unlock()

	// if it's been less than 10 minutes since the last update, don't update again...
	duration, _ := time.ParseDuration("-10m")
	if id.LastUpdate.After(time.Now().Add(duration)) {
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

	sd, err := GetProxyStatusData(ctx, kubeClient)
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

func (r *IstioData) CheckProxiedPods(ctx context.Context, kubeClient *kubernetes.Clientset) (error, []*corev1.Pod) {
	log := log.FromContext(ctx)
	log.Info("Checking proxied pods")
	var oldPods = []*corev1.Pod{}
	for _, ps := range r.ProxyStatuses {
		confRev := r.RevisionsByNamespace[ps.ProxiedPodNamespace]
		if confRev == "" {
			continue // skip if namespace isn't labeled with a revision
		}
		if confRev != ps.IstiodRevision {
			log.Info("Outdated Pod Found", "pod", ps.ProxiedPodName, "ns", ps.ProxiedPodNamespace, "proxyIstioRev", ps.IstiodRevision, "nsIstioRev", confRev)
			// TODO: here is where we label the pod
			pod, err := kubeClient.CoreV1().Pods(ps.ProxiedPodNamespace).Get(ctx, ps.ProxiedPodName, v1.GetOptions{})
			if err != nil {
				log.Error(err, "Couldn't retrieve pod from api")
				return err, nil
			}
			oldPods = append(oldPods, pod)
		}
	}
	return nil, oldPods
}

func (r *ICUPReconciler) LabelPodsOutdated(ctx context.Context, oldPods []*corev1.Pod) error {
	log := log.FromContext(ctx)
	for _, pod := range oldPods {
		pod.Labels["ipuc.cloudera.com/pod-outdated"] = time.Now().String()
		err := r.Update(ctx, pod)
		if err != nil {
			log.Error(err, "Couldn't update outdated pod label")
			return err
		}
	}
	return nil
}
