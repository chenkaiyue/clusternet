# Clusternet

[![GoPkg Widget](https://pkg.go.dev/badge/github.com/clusternet/clusternet.svg)](https://pkg.go.dev/github.com/clusternet/clusternet)
[![License](https://img.shields.io/github/license/clusternet/clusternet)](https://www.apache.org/licenses/LICENSE-2.0.html)
![GoVersion](https://img.shields.io/github/go-mod/go-version/clusternet/clusternet)
[![Go Report Card](https://goreportcard.com/badge/github.com/clusternet/clusternet)](https://goreportcard.com/report/github.com/clusternet/clusternet)
![build](https://github.com/clusternet/clusternet/actions/workflows/ci.yml/badge.svg)
[![Version](https://img.shields.io/github/v/release/clusternet/clusternet)](https://github.com/clusternet/clusternet/releases)

----

Managing Your Clusters (including public, private, hybrid, edge, etc) as easily as Visiting the Internet.

----

Clusternet (**Cluster** Inter**net**) is an open source ***add-on*** that helps you manage thousands of millions of
Kubernetes clusters as easily as visiting the Internet. No matter the clusters are running on public cloud, private
cloud, hybrid cloud, or at the edge, Clusternet lets you manage/visit them all as if they were running locally. This
also help eliminate the need to juggle different management tools for each cluster.

**Clusternet can also help deploy and coordinate applications to multiple clusters from a single set of APIs in a
hosting cluster.**

Clusternet will help setup network tunnels in a configurable way, when your clusters are running in a VPC network, at
the edge, or behind a firewall.

Clusternet also provides a Kubernetes-styled API, where you can continue using the Kubernetes way, such as KubeConfig,
to visit a certain Managed Kubernetes cluster, or a Kubernetes service.

Clusternet is multiple platforms supported now, including

- `darwin/amd64` and `darwin/arm64`;
- `linux/amd64`, `linux/arm64`, `linux/ppc64le`, `linux/s390x`, `linux/386` and `linux/arm`;

----

- [Architecture](#architecture)
- [Concepts](#concepts)
- [Contributing & Developing](#contributing--developing)
- [Getting Started](#getting-started)
    - [Deploying Clusternet](#deploying-clusternet)
        - [Deploying `clusternet-hub` in parent cluster](#deploying-clusternet-hub-in-parent-cluster)
        - [Deploying `clusternet-agent` in child cluster](#deploying-clusternet-agent-in-child-cluster)
    - [Check Cluster Registrations](#check-cluster-registrations)
    - [Check ManagedCluster Status](#check-managedcluster-status)
    - [Visit ManagedCluster With RBAC](#visit-managedcluster-with-rbac)
    - [How to Interact with Clusternet](#how-to-interact-with-clusternet)
    - [Deploying Applications to Multiple Clusters](#deploying-applications-to-multiple-clusters)

----

# Architecture

<img src="./docs/images/clusternet-arch.png" style="width:1000px;"/>

Clusternet is light-weighted that consists of two components, `clusternet-agent` and `clusternet-hub`.

`clusternet-agent` is responsible for

- auto-registering current cluster to a parent cluster as a child cluster, which is also been called `ManagedCluster`;
- reporting heartbeats of current cluster, including Kubernetes version, running platform, `healthz`/`readyz`/`livez`
  status, etc;
- setting up a websocket connection that provides full-duplex communication channels over a single TCP connection to
  parent cluster;

`clusternet-hub` is responsible for

- approving cluster registration requests and creating dedicated resources, such as namespaces, serviceaccounts and RBAC
  rules, for each child cluster;
- serving as an **aggregated apiserver (AA)**, which is used to serve as a websocket server that maintain multiple
  active websocket connections from child clusters;
- providing Kubernstes-styled API to redirect/proxy/upgrade requests to each child cluster;
- coordinating and deploying applications to multiple clusters from a single set of APIs;

> :pushpin: :pushpin: Note:
>
> Since `clusternet-hub` is running as an AA, please make sure that parent apiserver could visit the
> `clusternet-hub` service.

# Concepts

For every Kubernetes cluster that wants to be managed, we call it **child cluster**. The cluster where child clusters
are registerring to, we call it **parent cluster**.

`clusternet-agent` runs in child cluster, while `clusternet-hub` runs in parent cluster.

- `ClusterRegistrationRequest` is an object that `clusternet-agent` creates in parent cluster for child cluster
  registration.
- `ManagedCluster` is an object that `clusternet-hub` creates in parent cluster after
  approving `ClusterRegistrationRequest`.
- `HelmChart` is an object contains a [helm chart](https://helm.sh/docs/topics/charts/) configuration.
- `Subscription` defines the resources that subscribers want to install into clusters. For every matched cluster, a
  corresponding `Base` object will be created in its dedicated namespace.
- `Localization` and `Globalization` will define the overrides with priority, where lower numbers are considered lower
  priority. `Localization` is namespace-scoped resource, while `Globalization` is cluster-scoped.
- `Base` objects will be rendered to `Description` objects with `Globalization` and `Localization` settings applied.
  `Descritpion` is the final resources to be deployed into target child clusters.

<img src="./docs/images/clusternet-apps-concepts.png" style="width:1000px;"/>

# Contributing & Developing

If you want to get participated and become a contributor to Clusternet, please don't hesitate to refer to our
[CONTRIBUTING](CONTRIBUTING.md) document for details.

A [developer guide](./docs/developer-guide.md) is ready to help you

- build binaries for all platforms, such as `darwin/amd64`, `linux/amd64`, `linux/arm64`, etc;
- build docker images for multiple platforms, such as `linux/amd64`, `linux/arm64`, etc;

# Getting Started

## Deploying Clusternet

You need to deploy `clusternet-agent` and `clusternet-hub` in child cluster and parent cluster respectively.

### Deploying `clusternet-hub` in parent cluster

```bash
$ kubectl apply -f deploy/hub
```

And then create a bootstrap token for `clusternet-agent`,

```bash
$ # this will create a bootstrap token 07401b.f395accd246ae52d
$ kubectl apply -f manifests/samples/cluster_bootstrap_token.yaml
```

### Deploying `clusternet-agent` in child cluster

`clusternet-agent` runs in child cluster and helps register self-cluster to parent cluster.

`clusternet-agent` could be configured with below three kinds of `SyncMode` (configured by flag `--cluster-sync-mode`),

- `Push` means that all the resource changes in the parent cluster will be synchronized, pushed and applied to child
  clusters by `clusternet-hub` automatically.
- `Pull` means `clusternet-agent` will watch, synchronize and apply all the resource changes from the parent cluster to
  child cluster.
- `Dual` combines both `Push` and `Pull` mode. This mode is strongly recommended, which is usually used together with
  feature gate `AppPusher`.

Feature gate `AppPusher` works on agent side, which is introduced mainly for below two reasons,

- `SyncMode` is not suggested getting changed after registration, which may bring in inconsistent settings and
  behaviors. That's why `Dual` mode is strong recommended. When `Dual` mode is set, feature gate `AppPusher` provides a
  way to help switch `Push` mode to `Pull` mode without really changing flag `--cluster-sync-mode`, and vice versa.

- For security concerns, such as child cluster security risks, etc.

  When a child cluster has disabled feature gate `AppPusher`, the parent cluster won't deploy any applications to it,
  even if SyncMode `Push` or `Dual` is set. At this time, this child cluster is working like `Pull` mode.

  Resources to be deployed are represented as `Description`, you can run your own controllers as well to watch changes
  of `Description` objects, then distribute and deploy resources.

Upon deploying `clusternet-agent`, a secret that contains token for cluster registration should be created firstly.

```bash
$ # create namespace clusternet-system if not created
$ kubectl create ns clusternet-system
$ # here we use the token created above
$ PARENTURL=https://192.168.10.10 REGTOKEN=07401b.f395accd246ae52d envsubst < ./deploy/templates/clusternet_agent_secret.yaml | kubectl apply -f -
```

The `PARENTURL` above is the apiserver address of the parent cluster that you want to register to, the `https` scheme
must be specified and it is the only one supported at the moment. If the apiserver is not listening on the standard
https port (:443), please specify the port number in the URL to ensure the agent connects to the right endpoint, for
instance, `https://192.168.10.10:6443`.

```bash
$ # before deploying, you could update the SyncMode if needed
$ kubectl apply -f deploy/agent
```

## Check Cluster Registrations

```bash
$ # clsrr is an alias for ClusterRegistrationRequest
$ kubectl get clsrr
NAME                                              CLUSTER ID                             STATUS     AGE
clusternet-dc91021d-2361-4f6d-a404-7c33b9e01118   dc91021d-2361-4f6d-a404-7c33b9e01118   Approved   3d6h
$ kubectl get clsrr clusternet-dc91021d-2361-4f6d-a404-7c33b9e01118 -o yaml
apiVersion: clusters.clusternet.io/v1beta1
kind: ClusterRegistrationRequest
metadata:
  labels:
    clusters.clusternet.io/cluster-id: dc91021d-2361-4f6d-a404-7c33b9e01118
    clusters.clusternet.io/cluster-name: clusternet-cluster-dzqkw
    clusters.clusternet.io/registered-by: clusternet-agent
  name: clusternet-dc91021d-2361-4f6d-a404-7c33b9e01118
spec:
  clusterId: dc91021d-2361-4f6d-a404-7c33b9e01118
  clusterName: clusternet-cluster-dzqkw
  clusterType: EdgeCluster
status:
  caCertificate: REDACTED
  dedicatedNamespace: clusternet-dhxfs
  managedClusterName: clusternet-cluster-dzqkw
  result: Approved
  token: REDACTED
```

After `ClusterRegistrationRequest` is approved, the status will be updated with corresponding credentials that can be
used to access parent cluster if needed. Those credentials have been set with scoped RBAC rules, see blow two rules for
details.

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  annotations:
    clusternet.io/autoupdate: "true"
  labels:
    clusters.clusternet.io/bootstrapping: rbac-defaults
    clusters.clusternet.io/cluster-id: dc91021d-2361-4f6d-a404-7c33b9e01118
    clusternet.io/created-by: clusternet-hub
  name: clusternet-dc91021d-2361-4f6d-a404-7c33b9e01118
rules:
  - apiGroups:
      - clusters.clusternet.io
    resources:
      - clusterregistrationrequests
    verbs:
      - create
      - get
  - apiGroups:
      - proxies.clusternet.io
    resourceNames:
      - dc91021d-2361-4f6d-a404-7c33b9e01118
    resources:
      - sockets
    verbs:
      - '*'
```

and

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  annotations:
    clusternet.io/autoupdate: "true"
  labels:
    clusters.clusternet.io/bootstrapping: rbac-defaults
    clusternet.io/created-by: clusternet-hub
  name: clusternet-managedcluster-role
  namespace: clusternet-dhxfs
rules:
  - apiGroups:
      - '*'
    resources:
      - '*'
    verbs:
      - '*'
```

## Check ManagedCluster Status

```bash
$ # mcls is an alias for ManagedCluster
$ # kubectl get mcls -A
$ # or append "-o wide" to display extra columns
$ kubectl get mcls -A -o wide
NAMESPACE          NAME                       CLUSTER ID                             CLUSTER TYPE   SYNC MODE   KUBERNETES   READYZ   AGE
clusternet-dhxfs   clusternet-cluster-dzqkw   dc91021d-2361-4f6d-a404-7c33b9e01118   EdgeCluster    Dual        v1.19.10     true     7d23h
$ kubectl get mcls -n clusternet-dhxfs   clusternet-cluster-dzqkw -o yaml
apiVersion: clusters.clusternet.io/v1beta1
kind: ManagedCluster
metadata:
  labels:
    clusters.clusternet.io/cluster-id: dc91021d-2361-4f6d-a404-7c33b9e01118
    clusters.clusternet.io/cluster-name: clusternet-cluster-dzqkw
    clusternet.io/created-by: clusternet-agent
  name: clusternet-cluster-dzqkw
  namespace: clusternet-dhxfs
spec:
  clusterId: dc91021d-2361-4f6d-a404-7c33b9e01118
  clusterType: EdgeCluster
  syncMode: Dual
status:
  apiserverURL: http://10.0.0.10:8080
  appPusher: true
  healthz: true
  k8sVersion: v1.19.10
  lastObservedTime: "2021-06-30T08:55:14Z"
  livez: true
  platform: linux/amd64
  readyz: true
```

The status of `ManagedCluster` is updated by `clusternet-agent` every 3 minutes for default, which can be configured by
flag `--cluster-status-update-frequency`.

## Visit ManagedCluster With RBAC

***Clusternet supports visiting all your managed clusters with RBAC.***

There is one prerequisite here, that is `kube-apiserver` should **allow anonymous requests**. The
flag `--anonymous-auth` is set to be `true` by default. So you can just ignore this unless this flag is set to `false`
explicitly.

Actually what you need is to

1. Append `/apis/proxies.clusternet.io/v1alpha1/sockets/<CLUSTER-ID>/proxy/https/<SERVER-URL>`
   or `/apis/proxies.clusternet.io/v1alpha1/sockets/<CLUSTER-ID>/proxy/direct` at the end of original **parent cluster**
   server address

   > - `CLUSTER-ID` is a UUID for your child cluster, which is auto-populated by `clusternet-agent`, such as dc91021d-2361-4f6d-a404-7c33b9e01118. You could get this UUID from objects `ClusterRegistrationRequest`,
       `ManagedCluster`, etc. Also this UUID is labeled with key `clusters.clusternet.io/cluster-id`.
   >
   >- `SERVER-URL` is the apiserver address of your child cluster, it could be `localhost`, `127.0.0.1` and etc, only if
      `clusternet-agent` could access.

   You can follow below commands to help modify above changes.

    ```bash
    $ # suppose your parent cluster kubeconfig locates at /home/demo/.kube/config.parent
    $ kubectl config view --kubeconfig=/home/demo/.kube/config.parent --minify=true --raw=true > ./config-cluster-dc91021d-2361-4f6d-a404-7c33b9e01118
    $
    $ export KUBECONFIG=`pwd`/config-cluster-dc91021d-2361-4f6d-a404-7c33b9e01118
    $ kubectl config view
    apiVersion: v1
    clusters:
    - cluster:
        certificate-authority-data: DATA+OMITTED
        server: https://10.0.0.10:6443
      name: kubernetes
    contexts:
    - context:
        cluster: kubernetes
        user: kubernetes-admin
      name: kubernetes-admin@kubernetes
    current-context: kubernetes-admin@kubernetes
    kind: Config
    preferences: {}
    users:
    - name: kubernetes-admin
      user:
        client-certificate-data: REDACTED
        client-key-data: REDACTED
    $
    $ # suppose your child cluster running at https://demo1.cluster.net
    $ kubectl config set-cluster `kubectl config get-clusters | grep -v NAME` \
      --server=https://10.0.0.10:6443/apis/proxies.clusternet.io/v1alpha1/sockets/dc91021d-2361-4f6d-a404-7c33b9e01118/proxy/https/demo1.cluster.net
    $ # or just use the direct proxy path
    $ kubectl config set-cluster `kubectl config get-clusters | grep -v NAME` \
      --server=https://10.0.0.10:6443/apis/proxies.clusternet.io/v1alpha1/sockets/dc91021d-2361-4f6d-a404-7c33b9e01118/proxy/direct
    ```

   > :pushpin: :pushpin: Note:
   >
   > Clusternet supports both http and https scheme.
   >
   > If you want to use scheme `http` to demonstrate how it works, i.e. `/apis/proxies.clusternet.io/v1alpha1/sockets/<CLUSTER-ID>/proxy/http/<SERVER-URL>`, you can simply ***run a local proxy in your child cluster***, for example,
   >
   > ```bash
   > $ kubectl proxy --address='10.212.0.7' --accept-hosts='^*$'
   > ```
   >
   > Please replace `10.212.0.7` with your real local IP address.
   >
   > Then you can visit child cluster with http scheme. The KubeConfig here would be quite simple,
   >
    ```bash
    apiVersion: v1
    clusters:
    - cluster:
        certificate-authority-data: DATA+OMITTED
        server: https://10.0.0.10:6443/apis/proxies.clusternet.io/v1alpha1/sockets/dc91021d-2361-4f6d-a404-7c33b9e01118/proxy/http/10.212.0.7
      name: kubernetes
    contexts:
    - context:
        cluster: kubernetes
        user: kubernetes-admin
      name: kubernetes-admin@kubernetes
    current-context: kubernetes-admin@kubernetes
    kind: Config
    preferences: {}
    users:
    - name: kubernetes-admin
      user:
        username: system:anonymous
    ```

2. Then update user entry with **credentials from child clusters**

   > :see_no_evil: :see_no_evil: Note:
   >
   > `Clusternet-hub` does not care about those credentials at all, passing them directly to child clusters.

    - If you're using tokens, such
      as [bootstrap tokens](https://kubernetes.io/docs/reference/access-authn-authz/bootstrap-tokens/),
      [ServiceAccount tokens](https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/#use-multiple-service-accounts)
      , etc, please follow below modifications.

      ```bash
      $ export KUBECONFIG=`pwd`/config-cluster-dc91021d-2361-4f6d-a404-7c33b9e01118
      $ # below is what we modified in above step 1
      $ kubectl config view
      apiVersion: v1
      clusters:
      - cluster:
          certificate-authority-data: DATA+OMITTED
          server: https://10.0.0.10:6443/apis/proxies.clusternet.io/v1alpha1/sockets/dc91021d-2361-4f6d-a404-7c33b9e01118/proxy/direct
        name: kubernetes
      contexts:
      - context:
          cluster: kubernetes
          user: kubernetes-admin
        name: kubernetes-admin@kubernetes
      current-context: kubernetes-admin@kubernetes
      kind: Config
      preferences: {}
      users:
      - name: kubernetes-admin
        user:
          client-certificate-data: REDACTED
          client-key-data: REDACTED
      $
      $ # modify user part to below
      $ vim config-cluster-dc91021d-2361-4f6d-a404-7c33b9e01118
        ...
        user:
          username: system:anonymous
          as: clusternet
          as-user-extra:
              clusternet-token:
                  - BASE64-DECODED-PLEASE-CHANGE-ME
      ```

      Please replace `BASE64-DECODED-PLEASE-CHANGE-ME` to a token that valid from **child cluster**. ***Please notice
      the tokens replaced here should be base64 decoded.***

    - If you're using TLS certificates, please follow below modifications.

      ```bash
      $ export KUBECONFIG=`pwd`/config-cluster-dc91021d-2361-4f6d-a404-7c33b9e01118
      $ # below is what we modified in above step 1
      $ kubectl config view
      apiVersion: v1
      clusters:
      - cluster:
          certificate-authority-data: DATA+OMITTED
          server: https://10.0.0.10:6443/apis/proxies.clusternet.io/v1alpha1/sockets/dc91021d-2361-4f6d-a404-7c33b9e01118/proxy/direct
        name: kubernetes
      contexts:
      - context:
          cluster: kubernetes
          user: kubernetes-admin
        name: kubernetes-admin@kubernetes
      current-context: kubernetes-admin@kubernetes
      kind: Config
      preferences: {}
      users:
      - name: kubernetes-admin
        user:
          client-certificate-data: REDACTED
          client-key-data: REDACTED
      $
      $ # modify user part to below
      $ vim config-cluster-dc91021d-2361-4f6d-a404-7c33b9e01118
        ...
        user:
          username: system:anonymous
          as: clusternet
          as-user-extra:
              clusternet-certificate:
                  - CLIENT-CERTIFICATE-DATE-BASE64-ENCODED-PLEASE-CHANGE-ME
              clusternet-privatekey:
                  - CLIENT-KEY-DATE-PLEASE-BASE64-ENCODED-CHANGE-ME
      ```

      Please replace `CLIENT-CERTIFICATE-DATE-BASE64-ENCODED-PLEASE-CHANGE-ME`
      and `CLIENT-KEY-DATE-PLEASE-BASE64-ENCODED-CHANGE-ME`
      with certficate and private key from child cluster. **Please notice the tokens replaced here should be base64
      encoded.**

## How to Interact with Clusternet

Clusternet has provided two ways to help interact with Clusternet.

- kubectl plugin [kubectl-clusternet](https://github.com/clusternet/kubectl-clusternet)
- [use client-go to interact with Clusternet](https://github.com/clusternet/clusternet/blob/main/examples/clientgo/READEME.md)

## Deploying Applications to Multiple Clusters

Clusternet supports deploying applications to multiple clusters from a single set of APIs in a hosting cluster.

> :pushpin: :pushpin: Note:
>
> Feature gate `Deployer` should be enabled by `clusternet-hub`.

First, let's see an exmaple application. Below `Subscription` "app-demo" defines the target child clusters to be
distributed to, and the resources to be deployed with.

```yaml
# examples/applications/subscription.yaml
apiVersion: apps.clusternet.io/v1alpha1
kind: Subscription
metadata:
  name: app-demo
  namespace: default
spec:
  subscribers: # defines the clusters to be distributed to
    - clusterAffinity:
        matchLabels:
          clusters.clusternet.io/cluster-id: dc91021d-2361-4f6d-a404-7c33b9e01118 # PLEASE UPDATE THIS CLUSTER-ID TO YOURS!!!
  feeds: # defines all the resources to be deployed with
    - apiVersion: apps.clusternet.io/v1alpha1
      kind: HelmChart
      name: mysql
      namespace: default
    - apiVersion: v1
      kind: Namespace
      name: foo
    - apiVersion: apps/v1
      kind: Service
      name: my-nginx-svc
      namespace: foo
    - apiVersion: apps/v1
      kind: Deployment
      name: my-nginx
      namespace: foo
```

Before applying this `Subscription`, please
modify [examples/applications/subscription.yaml](https://github.com/clusternet/clusternet/blob/main/examples/applications/subscription.yaml)
with your clusterID.

After installing kubectl plugin [kubectl-clusternet](https://github.com/clusternet/kubectl-clusternet), you could run
below commands to distribute this application to child clusters.

```bash
$ kubectl clusternet apply -f examples/applications/
helmchart.apps.clusternet.io/mysql created
namespace/foo created
deployment.apps/my-nginx created
service/my-nginx-svc created
subscription.apps.clusternet.io/app-demo created
$ # or
$ # kubectl-clusternet apply -f examples/applications/
```

Then you can view the resources just created,

```bash
$ # list Subscription
$ kubectl clusternet get subs -A
NAMESPACE   NAME       AGE
default     app-demo   6m4s
$ kubectl clusternet get chart
NAME             CHART   VERSION   REPO                                 STATUS   AGE
mysql            mysql   8.6.2     https://charts.bitnami.com/bitnami   Found    71s
$ kubectl clusternet get ns
NAME   CREATED AT
foo    2021-08-07T08:50:55Z
$ kubectl clusternet get svc -n foo
NAME           CREATED AT
my-nginx-svc   2021-08-07T08:50:57Z
$ kubectl clusternet get deploy -n foo
NAME       CREATED AT
my-nginx   2021-08-07T08:50:56Z
```

`Clusternet` will help deploy and coordinate applications to multiple clusters. You can check the status by following
commands,

```bash
$ kubectl clusternet get mcls -A
NAMESPACE          NAME                       CLUSTER ID                             SYNC MODE   KUBERNETES   READYZ   AGE
clusternet-5l82l   clusternet-cluster-hx455   dc91021d-2361-4f6d-a404-7c33b9e01118   Dual        v1.21.0      true     5d22h
$ # list Descriptions
$ kubectl clusternet get desc -A
NAMESPACE          NAME               DEPLOYER   STATUS    AGE
clusternet-5l82l   app-demo-generic   Generic    Success   2m55s
clusternet-5l82l   app-demo-helm      Helm       Success   2m55s
$ kubectl describe desc -n clusternet-5l82l   app-demo-generic
...
Status:
  Phase:  Success
Events:
  Type    Reason                Age    From            Message
  ----    ------                ----   ----            -------
  Normal  SuccessfullyDeployed  2m55s  clusternet-hub  Description clusternet-5l82l/app-demo-generic is deployed successfully
$ # list Helm Release
$ # hr is an alias for HelmRelease
$ kubectl clusternet get hr -n clusternet-5l82l
NAME                  CHART       VERSION   REPO                                 STATUS     AGE
helm-demo-mysql       mysql       8.6.2     https://charts.bitnami.com/bitnami   deployed   2m55s
```

You can also verify the installation with Helm command line in your child cluster,

```bash
$ helm ls -n abc
NAME               	NAMESPACE	REVISION	UPDATED                             	STATUS  	CHART            	APP VERSION
helm-demo-mysql    	abc      	1       	2021-07-06 14:34:44.188938 +0800 CST	deployed	mysql-8.6.2      	8.0.25
```
