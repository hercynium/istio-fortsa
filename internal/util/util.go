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
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.infra.cloudera.com/sscaffidi/istio-proxy-update-controller/internal/util/istio/proxystatus"
	"github.infra.cloudera.com/sscaffidi/istio-proxy-update-controller/internal/util/istio/tags"
)

type IstioData struct {
	mu            sync.Mutex
	ProxyStatuses []IstioProxyStatusData
	TagInfo       map[string]tags.TagDescription
	LastUpdate    time.Time
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
	ti, err := GetRevisionTagInfo(ctx, kubeClient)
	if err != nil {
		log.Error(err, "Couldn't update tag info")
		return err
	}
	for _, t := range ti {
		id.TagInfo[t.Tag] = t
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
