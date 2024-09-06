package util

import (
	"context"
	"fmt"
	"strings"

	"istio.io/istio/pkg/log"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.infra.cloudera.com/sscaffidi/istio-proxy-update-controller/internal/util/istio/proxystatus"
	"github.infra.cloudera.com/sscaffidi/istio-proxy-update-controller/internal/util/istio/tags"
)

// this should be called from the controllers - it fetch and process the istio data
func ProcessIstioStatusData(ctx context.Context, req ctrl.Request, kubeClient *kubernetes.Clientset) {
	// get tag -> revision -> namespaces mapping
	tagInfo, _ := tags.GetTags(ctx, kubeClient)
	for _, t := range tagInfo {
		log.Debug(fmt.Sprintf("%s\t%s\t%s\n", t.Tag, t.Revision, strings.Join(t.Namespaces, ",")))
	}

	// see here for how to navigate the proxy-status data:
	//   https://github.com/istio/istio/blob/master/istioctl/pkg/writer/pilot/status.go
	xdsResponses := proxystatus.GetProxyStatus()
	for _, r := range xdsResponses {
		log.Debug(fmt.Sprintf("%s", r.String()))
	}
}
