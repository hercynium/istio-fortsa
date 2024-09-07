package util

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	xdsstatus "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"
	//"github.com/spf13/pflag"
	//"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/multixds"
	"istio.io/istio/pilot/pkg/model"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.infra.cloudera.com/sscaffidi/istio-proxy-update-controller/internal/util/istio/proxystatus"
	"github.infra.cloudera.com/sscaffidi/istio-proxy-update-controller/internal/util/istio/tags"
)

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

func PrintRevisionTagInfo(ctx context.Context, tagInfo []tags.TagDescription) {
	//log := log.FromContext(ctx)
	for _, t := range tagInfo {
		fmt.Printf("%s\t%s\t%s\n", t.Tag, t.Revision, strings.Join(t.Namespaces, ","))
	}
}

type IstioProxyStatusData struct {
	ProxiedPodName      string
	ProxiedPodNamespace string
	IstiodPodName       string
	IstiodPodNamespace  string
	IstiodRevision      string
	IstiodVersion       string
	ClusterID           string
}

// this should be called from the controllers - it will fetch and process the istio data
func GetProxyStatusData(ctx context.Context, kubeClient *kubernetes.Clientset) ([]IstioProxyStatusData, error) {
	log := log.FromContext(ctx)

	/* 	rootOptions := cli.AddRootFlags(&pflag.FlagSet{})
	   	ictx := cli.NewCLIContext(rootOptions)
	   	icli, err := ictx.CLIClient()
	   	meshInfo, err := icli.GetIstioVersions(context.TODO(), ictx.IstioNamespace())
	   	if err != nil {
	   		return
	   	}
	   	for _, m := range *meshInfo {
	   		fmt.Printf("%v\t%v\t%v\t%v\n", m.Component, m.Revision, m.Info.GitRevision, m.Info.Version)
	   	} */

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
			sd := IstioProxyStatusData{
				ClusterID:           meta.ClusterID.String(),
				IstiodPodName:       cp.ID,
				IstiodPodNamespace:  "istio-system", // TODO: figure out how to get this dynamically
				IstiodRevision:      regexp.MustCompile(`^istiod-(.*)-[^-]+-[^-]+$`).ReplaceAllString(cp.ID, `$1`),
				IstiodVersion:       meta.IstioVersion,
				ProxiedPodName:      strings.TrimSuffix(clientConfig.GetNode().GetId(), "."+meta.Namespace),
				ProxiedPodNamespace: meta.Namespace,
			}
			istioProxyStatusData = append(istioProxyStatusData, sd)
		}
	}
	return istioProxyStatusData, nil
}

func PrintProxyStatusData(ctx context.Context, statusData []IstioProxyStatusData) {
	//log := log.FromContext(ctx)
	for _, sd := range statusData {
		fmt.Printf("[%v, %v, %v, %v]\n", sd.IstiodPodName, sd.IstiodRevision, sd.ProxiedPodName, sd.ProxiedPodNamespace)
	}
}

type IstioData struct {
	ProxyStatuses []IstioProxyStatusData
	TagInfo       []tags.TagDescription
	LastUpdate    time.Time
}

func (id *IstioData) RefreshIstioData(ctx context.Context, req ctrl.Request, kubeClient *kubernetes.Clientset) error {
	log := log.FromContext(ctx)
	// if it's been less than 10 minutes since the last update, don't update again...
	duration, _ := time.ParseDuration("-10m")
	if id.LastUpdate.After(time.Now().Add(duration)) {
		log.Info("Not updating istio data because not enough time has passed")
		return nil
	}

	ti, err := GetRevisionTagInfo(ctx, kubeClient)
	id.TagInfo = ti
	//PrintRevisionTagInfo(ctx, id.TagInfo)

	sd, err := GetProxyStatusData(ctx, kubeClient)
	id.ProxyStatuses = sd
	//PrintProxyStatusData(ctx, id.ProxyStatuses)

	id.LastUpdate = time.Now()

	return err
}
