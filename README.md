# kubeGatherInfo

Gather info about your Kubernetes Cluster

# Current capabilities

* Detect current versions of Kubernetes, Nodes, Cilium, Nginx Ingress and Istio 
* Detect and fix Helm secrets stuck in 'pending-upgrade' or other bad states
* Detect and fix pods running mismatched versions of Istio sidecars
* Detect nodes not on the correct version of Kubernetes

# Examples

```
$ ./kubegatherinfo --kubeconfig ~/.kube/myCluster -s
Pulling data from /Users/jdoe/.kube/myCluster...
===================================================
Kubernetes Server Version: v1.26.7
  Kubernetes Node Version: v1.26.x
           Cilium Version: v1.11.17
            Nginx Version: v1.8.1
            Istio Version: 1.17.2

Nodes to drain for version Synchronization:
===================================================
All nodes running correct version of Kubernetes 

Pulling Helm Secrets for validation...
===================================================
All 730 helm secrets are good!

Pods needing restart for Istio Synchronization: 
===================================================
All pods running correct version of Istio 
```


