// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package tags

import (
	"cmp"
	"context"
	"fmt"

	"istio.io/istio/istioctl/pkg/tag"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"k8s.io/client-go/kubernetes"
)

const (
	IstioTagLabel = "istio.io/tag"
)

type TagDescription struct {
	Tag        string   `json:"tag"`
	Revision   string   `json:"revision"`
	Namespaces []string `json:"namespaces"`
}

type uniqTag struct {
	revision, tag string
}

func GetTags(ctx context.Context, kubeClient kubernetes.Interface) ([]TagDescription, error) {
	tagWebhooks, err := tag.GetRevisionWebhooks(ctx, kubeClient)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve revision tags: %v", err)
	}
	if len(tagWebhooks) == 0 {
		fmt.Printf("No Istio revision tag MutatingWebhookConfigurations to list\n")
		return []TagDescription{}, nil
	}
	rawTags := map[uniqTag]TagDescription{}
	for _, wh := range tagWebhooks {
		tagName := tag.GetWebhookTagName(wh)
		tagRevision, err := tag.GetWebhookRevision(wh)
		if err != nil {
			return nil, fmt.Errorf("error parsing revision from webhook %q: %v", wh.Name, err)
		}
		tagNamespaces, err := tag.GetNamespacesWithTag(ctx, kubeClient, tagName)
		if err != nil {
			return nil, fmt.Errorf("error retrieving namespaces for tag %q: %v", tagName, err)
		}
		tagDesc := TagDescription{
			Tag:        tagName,
			Revision:   tagRevision,
			Namespaces: tagNamespaces,
		}
		key := uniqTag{
			revision: tagRevision,
			tag:      tagName,
		}
		rawTags[key] = tagDesc
	}
	for k := range rawTags {
		if k.tag != "" {
			delete(rawTags, uniqTag{revision: k.revision})
		}
	}

	tags := slices.SortFunc(maps.Values(rawTags), func(a, b TagDescription) int {
		if r := cmp.Compare(a.Revision, b.Revision); r != 0 {
			return r
		}
		return cmp.Compare(a.Tag, b.Tag)
	})

	return tags, nil
}
