/*
Copyright 2021 The Clusternet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package apiserver

import (
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apiserver/pkg/admission"
	genericapi "k8s.io/apiserver/pkg/endpoints"
	genericdiscovery "k8s.io/apiserver/pkg/endpoints/discovery"
	k8sfeatures "k8s.io/apiserver/pkg/features"
	"k8s.io/apiserver/pkg/registry/rest"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/apiserver/pkg/storageversion"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
	"k8s.io/klog/v2"

	shadowinstall "github.com/clusternet/clusternet/pkg/apis/shadow/install"
	shadowapi "github.com/clusternet/clusternet/pkg/apis/shadow/v1alpha1"
	clusternet "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	informers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions"
	"github.com/clusternet/clusternet/pkg/registry/shadow/template"
)

var (
	// Scheme defines methods for serializing and deserializing API objects.
	Scheme = runtime.NewScheme()
	// Codecs provides methods for retrieving codecs and serializers for specific
	// versions and content types.
	Codecs = serializer.NewCodecFactory(Scheme)
	// ParameterCodec handles versioning of objects that are converted to query parameters.
	ParameterCodec = runtime.NewParameterCodec(Scheme)
)

const (
	clusternetGroupSuffix = ".clusternet.io"
)

func init() {
	shadowinstall.Install(Scheme)

	// we need to add the options to empty v1
	// TODO fix the server code to avoid this
	metav1.AddToGroupVersion(Scheme, schema.GroupVersion{Version: "v1"})
	metav1.AddToGroupVersion(Scheme, metav1.SchemeGroupVersion)

	// TODO: keep the generic API server from wanting this
	unversioned := schema.GroupVersion{Group: "", Version: "v1"}
	Scheme.AddUnversionedTypes(unversioned,
		&metav1.Status{},
		&metav1.APIVersions{},
		&metav1.APIGroupList{},
		&metav1.APIGroup{},
		&metav1.APIResourceList{},
		&metav1.List{},
	)
}

// ShadowAPIServer will make a shadow copy for all the APIs
type ShadowAPIServer struct {
	GenericAPIServer    *genericapiserver.GenericAPIServer
	maxRequestBodyBytes int64
	minRequestTimeout   int

	// admissionControl performs deep inspection of a given request (including content)
	// to set values and determine whether its allowed
	admissionControl admission.Interface

	kubeclient       *kubernetes.Clientset
	clusternetclient *clusternet.Clientset

	clusternetInformerFactory informers.SharedInformerFactory
}

func NewShadowAPIServer(apiserver *genericapiserver.GenericAPIServer,
	maxRequestBodyBytes int64, minRequestTimeout int,
	admissionControl admission.Interface,
	kubeclient *kubernetes.Clientset, clusternetclient *clusternet.Clientset,
	clusternetInformerFactory informers.SharedInformerFactory) *ShadowAPIServer {
	return &ShadowAPIServer{
		GenericAPIServer:          apiserver,
		maxRequestBodyBytes:       maxRequestBodyBytes,
		minRequestTimeout:         minRequestTimeout,
		admissionControl:          admissionControl,
		kubeclient:                kubeclient,
		clusternetclient:          clusternetclient,
		clusternetInformerFactory: clusternetInformerFactory,
	}
}

func (ss *ShadowAPIServer) InstallShadowAPIGroups(cl discovery.DiscoveryInterface) error {
	// TODO: add openapi controller to update openapi spec

	apiGroupResources, err := restmapper.GetAPIGroupResources(cl)
	if err != nil {
		return err
	}

	shadowv1alpha1storage := map[string]rest.Storage{}
	for _, apiGroupResource := range apiGroupResources {
		// no need to duplicate xxx.clusternet.io
		if strings.HasSuffix(apiGroupResource.Group.Name, clusternetGroupSuffix) {
			continue
		}

		// skip shadow group to avoid getting nested
		if apiGroupResource.Group.Name == shadowapi.GroupName {
			continue
		}

		preferredVersion := apiGroupResource.Group.PreferredVersion.Version
		for _, apiresource := range apiGroupResource.VersionedResources[preferredVersion] {
			// register scheme for original GVK
			Scheme.AddKnownTypeWithName(schema.GroupVersion{Group: apiGroupResource.Group.Name, Version: preferredVersion}.WithKind(apiresource.Kind),
				&unstructured.Unstructured{},
			)
			resourceRest := template.NewREST(ss.kubeclient, ss.clusternetclient, ParameterCodec, ss.clusternetInformerFactory)
			resourceRest.SetNamespaceScoped(apiresource.Namespaced)
			resourceRest.SetName(apiresource.Name)
			resourceRest.SetShortNames(apiresource.ShortNames)
			resourceRest.SetKind(apiresource.Kind)
			resourceRest.SetGroup(apiGroupResource.Group.Name)
			resourceRest.SetVersion(preferredVersion)
			shadowv1alpha1storage[apiresource.Name] = resourceRest
		}
	}

	shadowAPIGroupInfo := genericapiserver.NewDefaultAPIGroupInfo(shadowapi.GroupName, Scheme, ParameterCodec, Codecs)
	shadowAPIGroupInfo.PrioritizedVersions = []schema.GroupVersion{
		{
			Group:   shadowapi.GroupName,
			Version: shadowapi.SchemeGroupVersion.Version,
		},
	}
	shadowAPIGroupInfo.VersionedResourcesStorageMap["v1alpha1"] = shadowv1alpha1storage
	return ss.installAPIGroups(&shadowAPIGroupInfo)
}

// Exposes given api groups in the API.
// copied from k8s.io/apiserver/pkg/server/genericapiserver.go and modified
func (ss *ShadowAPIServer) installAPIGroups(apiGroupInfos ...*genericapiserver.APIGroupInfo) error {
	for _, apiGroupInfo := range apiGroupInfos {
		// Do not register empty group or empty version.  Doing so claims /apis/ for the wrong entity to be returned.
		// Catching these here places the error  much closer to its origin
		if len(apiGroupInfo.PrioritizedVersions[0].Group) == 0 {
			return fmt.Errorf("cannot register handler with an empty group for %#v", *apiGroupInfo)
		}
		if len(apiGroupInfo.PrioritizedVersions[0].Version) == 0 {
			return fmt.Errorf("cannot register handler with an empty version for %#v", *apiGroupInfo)
		}
	}

	for _, apiGroupInfo := range apiGroupInfos {
		if err := ss.installAPIResources(genericapiserver.APIGroupPrefix, apiGroupInfo); err != nil {
			return fmt.Errorf("unable to install api resources: %v", err)
		}

		// setup discovery
		// Install the version handler.
		// Add a handler at /apis/<groupName> to enumerate all versions supported by this group.
		apiVersionsForDiscovery := []metav1.GroupVersionForDiscovery{}
		for _, groupVersion := range apiGroupInfo.PrioritizedVersions {
			// Check the config to make sure that we elide versions that don't have any resources
			if len(apiGroupInfo.VersionedResourcesStorageMap[groupVersion.Version]) == 0 {
				continue
			}
			apiVersionsForDiscovery = append(apiVersionsForDiscovery, metav1.GroupVersionForDiscovery{
				GroupVersion: groupVersion.String(),
				Version:      groupVersion.Version,
			})
		}
		preferredVersionForDiscovery := metav1.GroupVersionForDiscovery{
			GroupVersion: apiGroupInfo.PrioritizedVersions[0].String(),
			Version:      apiGroupInfo.PrioritizedVersions[0].Version,
		}
		apiGroup := metav1.APIGroup{
			Name:             apiGroupInfo.PrioritizedVersions[0].Group,
			Versions:         apiVersionsForDiscovery,
			PreferredVersion: preferredVersionForDiscovery,
		}

		ss.GenericAPIServer.DiscoveryGroupManager.AddGroup(apiGroup)
		ss.GenericAPIServer.Handler.GoRestfulContainer.Add(genericdiscovery.NewAPIGroupHandler(ss.GenericAPIServer.Serializer, apiGroup).WebService())
	}
	return nil
}

// installAPIResources is a private method for installing the REST storage backing each api groupversionresource
// copied from k8s.io/apiserver/pkg/server/genericapiserver.go and modified
func (ss *ShadowAPIServer) installAPIResources(apiPrefix string, apiGroupInfo *genericapiserver.APIGroupInfo) error {
	var resourceInfos []*storageversion.ResourceInfo
	for _, groupVersion := range apiGroupInfo.PrioritizedVersions {
		if len(apiGroupInfo.VersionedResourcesStorageMap[groupVersion.Version]) == 0 {
			klog.Warningf("Skipping API %v because it has no resources.", groupVersion)
			continue
		}

		apiGroupVersion := ss.getAPIGroupVersion(apiGroupInfo, groupVersion, apiPrefix)
		if apiGroupInfo.OptionsExternalVersion != nil {
			apiGroupVersion.OptionsExternalVersion = apiGroupInfo.OptionsExternalVersion
		}

		apiGroupVersion.MaxRequestBodyBytes = ss.maxRequestBodyBytes

		r, err := apiGroupVersion.InstallREST(ss.GenericAPIServer.Handler.GoRestfulContainer)
		if err != nil {
			return fmt.Errorf("unable to setup API %v: %v", apiGroupInfo, err)
		}
		resourceInfos = append(resourceInfos, r...)
	}

	if utilfeature.DefaultFeatureGate.Enabled(k8sfeatures.StorageVersionAPI) &&
		utilfeature.DefaultFeatureGate.Enabled(k8sfeatures.APIServerIdentity) {
		// API installation happens before we start listening on the handlers,
		// therefore it is safe to register ResourceInfos here. The handler will block
		// write requests until the storage versions of the targeting resources are updated.
		ss.GenericAPIServer.StorageVersionManager.AddResourceInfo(resourceInfos...)
	}

	return nil
}

// a private method that copied from k8s.io/apiserver/pkg/server/genericapiserver.go and modified
func (ss *ShadowAPIServer) getAPIGroupVersion(apiGroupInfo *genericapiserver.APIGroupInfo, groupVersion schema.GroupVersion, apiPrefix string) *genericapi.APIGroupVersion {
	storage := make(map[string]rest.Storage)
	for k, v := range apiGroupInfo.VersionedResourcesStorageMap[groupVersion.Version] {
		storage[strings.ToLower(k)] = v
	}
	version := ss.newAPIGroupVersion(apiGroupInfo, groupVersion)
	version.Root = apiPrefix
	version.Storage = storage
	return version
}

// a private method that copied from k8s.io/apiserver/pkg/server/genericapiserver.go and modified
func (ss *ShadowAPIServer) newAPIGroupVersion(apiGroupInfo *genericapiserver.APIGroupInfo, groupVersion schema.GroupVersion) *genericapi.APIGroupVersion {
	return &genericapi.APIGroupVersion{
		GroupVersion:     groupVersion,
		MetaGroupVersion: apiGroupInfo.MetaGroupVersion,

		ParameterCodec:        apiGroupInfo.ParameterCodec,
		Serializer:            apiGroupInfo.NegotiatedSerializer,
		Creater:               apiGroupInfo.Scheme, //nolint:misspell
		Convertor:             apiGroupInfo.Scheme,
		ConvertabilityChecker: apiGroupInfo.Scheme,
		UnsafeConvertor:       runtime.UnsafeObjectConvertor(apiGroupInfo.Scheme),
		Defaulter:             apiGroupInfo.Scheme,
		Typer:                 apiGroupInfo.Scheme,
		Linker:                runtime.SelfLinker(meta.NewAccessor()),

		EquivalentResourceRegistry: ss.GenericAPIServer.EquivalentResourceRegistry,

		Admit:             ss.admissionControl,
		MinRequestTimeout: time.Duration(ss.minRequestTimeout) * time.Second,
		Authorizer:        ss.GenericAPIServer.Authorizer,
	}
}
