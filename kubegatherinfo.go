package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/schollz/progressbar/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/tools/clientcmd"
	// "k8s.io/kubectl/pkg/drain"
)

// Color Constants and settings
const dark = 30
const light = 37
const red = 31
const green = 32
const yellow = 33

var goodColor = colorString(green, false)

// var brightGreen = colorString(green, true)
var errorColor = colorString(red, false)
var warningColor = colorString(yellow, false)
var normalColor = colorString(light, false)
var white = colorString(light, true)
var darkGray = trueColorString(96, 96, 96)

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}

func colorString(code int, bold bool) string {
	var makeBold int
	if bold {
		makeBold = 1
	} else {
		makeBold = 0
	}

	return fmt.Sprintf("\033[%d;%d;49m", makeBold, code)
}

func trueColorString(r int, g int, b int) string {
	return fmt.Sprintf("\033[38;2;%d;%d;%dm", r, g, b) //, bgr, bgg, bgb)
}

func trueColorStringPlusBackground(r int, g int, b int, bgr int, bgg int, bgb int) string {
	return fmt.Sprintf("\033[38;2;%d;%d;%d;48;2;%d;%d;%dm", r, g, b, bgr, bgg, bgb)
}

type podInfo struct {
	name      string
	namespace string
	ownerName string
	ownerKind string
	// status    podStatusSummary
}

type podStatusSummary struct {
	running      int
	pending      int
	completed    int
	failed       int
	crashlooping int
	other        int
}

type ownerInfo struct {
	namespace string
	name      string
	kind      string
}

type ownerInfoList []ownerInfo

type podsToRestartList []podInfo

func breakoutImage(image string) (string, string, string) {
	r, _ := regexp.Compile("^([0-9a-zA-Z.-]+)/([-0-9a-zA-Z./]+):([-0-9a-zA-Z.]+)$")

	var matches [3]string
	for index, match := range r.FindStringSubmatch(image) {
		if index > 0 {
			matches[index-1] = match
		}
	}
	return matches[0], matches[1], matches[2]
}

func findCiliumVersion(clientset *kubernetes.Clientset, display bool) string {
	//	var warningColor = colorString(33, false)
	var requiredCiliumVersion string

	kubeSystemPodList, _ := clientset.CoreV1().Pods("kube-system").List(context.TODO(), metav1.ListOptions{})
	if len(kubeSystemPodList.Items) == 0 {
		requiredCiliumVersion = ""
	} else {
		// loop pods until we find the cilium-operator whatever pod, and then look up the owner and return it's image.
		for p := 0; p < len(kubeSystemPodList.Items); p++ {
			name := kubeSystemPodList.Items[p].OwnerReferences[0].Name
			if len(name) > 16 {
				if name[:15] == "cilium-operator" {
					_, _, requiredCiliumVersion = breakoutImage(findOwnerImage(clientset, "kube-system", kubeSystemPodList.Items[p].OwnerReferences[0].Name, kubeSystemPodList.Items[p].OwnerReferences[0].Kind))

					if display {
						if requiredCiliumVersion != "" {
							fmt.Printf("%s           Cilium Version: %s%s%s\n", goodColor, white, requiredCiliumVersion, normalColor)
						}
					}

					return requiredCiliumVersion
				}
			}
		}
	}

	return ""
}

func findNginxVersion(clientset *kubernetes.Clientset, display bool) string {
	//	var warningColor = colorString(33, false)
	var requiredNginxVersion string

	nginxPodList, _ := clientset.CoreV1().Pods("nginx-ingress").List(context.TODO(), metav1.ListOptions{})
	if len(nginxPodList.Items) == 0 {
		requiredNginxVersion = ""
	} else {
		// loop pods until we find the nginx-ingres-controller whatever pod, and then look up the owner and return it's image.
		for p := 0; p < len(nginxPodList.Items); p++ {
			name := nginxPodList.Items[p].OwnerReferences[0].Name
			if len(name) > 16 {
				if name[:15] == "nginx-ingress-c" {
					_, _, requiredNginxVersion = breakoutImage(findOwnerImage(clientset, "nginx-ingress", nginxPodList.Items[p].OwnerReferences[0].Name, nginxPodList.Items[p].OwnerReferences[0].Kind))

					if display {
						if requiredNginxVersion != "" {
							fmt.Printf("%s            Nginx Version: %s%s%s\n", goodColor, white, requiredNginxVersion, normalColor)
						}
					}

					return requiredNginxVersion
				}
			}
		}
	}

	return ""
}

func findIstioVersion(clientset *kubernetes.Clientset, display bool) string {
	var warningColor = colorString(33, false)
	var requiredIstioVersion string

	istioPodList, _ := clientset.CoreV1().Pods("istio-system").List(context.TODO(), metav1.ListOptions{})
	if len(istioPodList.Items) == 0 {
		fmt.Printf("%sIstio not running in this cluster!\n", warningColor)
		requiredIstioVersion = ""
	} else {
		_, _, requiredIstioVersion = breakoutImage(istioPodList.Items[0].Spec.Containers[0].Image)
	}

	if display {
		if requiredIstioVersion != "" {
			fmt.Printf("%s            Istio Version: %s%s%s\n", goodColor, white, requiredIstioVersion, normalColor)
		}
	}

	return requiredIstioVersion
}

func validateNodes(clientset *kubernetes.Clientset, desiredVersion string) ([]nodeVersionInfo, bool) {
	var allUpToDate bool
	var nodeVersions []nodeVersionInfo
	allUpToDate = true

	want := majorMinor(desiredVersion)

	nodes, clientErr := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if clientErr != nil {
		panic(clientErr)
	}
	for i := 0; i < len(nodes.Items); i++ {
		version := majorMinor(nodes.Items[i].Status.NodeInfo.KubeletVersion)
		if want != version {
			allUpToDate = false
		}

		nodeVersions = append(nodeVersions, nodeVersionInfo{
			name:     nodes.Items[i].Name,
			version:  nodes.Items[i].Status.NodeInfo.KubeletVersion,
			upToDate: (version == want),
		})
	}

	return nodeVersions, allUpToDate
}

func findOwner(clientset *kubernetes.Clientset, ns string, oName string, oKind string) (string, string) {
	switch oKind {
	case "ReplicaSet":
		replica, repErr := clientset.AppsV1().ReplicaSets(ns).Get(context.TODO(), oName, metav1.GetOptions{})
		if repErr != nil {
			panic(repErr.Error())
		}
		return replica.OwnerReferences[0].Name, "Deployment"
	case "DaemonSet", "StatefulSet", "Job":
		return oName, oKind
	default:
		//fmt.Printf("Could not find resource manager for type %s\n", pod.OwnerReferences[0].Kind)
		//continue
	}
	return "", ""
}

func findOwnerImage(clientset *kubernetes.Clientset, ns string, oName string, oKind string) string {
	if oKind == "Deployment" {
		ds, err := clientset.AppsV1().Deployments(ns).Get(context.TODO(), oName, metav1.GetOptions{})
		if err != nil {
			panic(err)
		}
		oKind = ds.OwnerReferences[0].Kind
		oName = ds.OwnerReferences[0].Name
	}
	switch oKind {
	case "ReplicaSet":
		replica, repErr := clientset.AppsV1().ReplicaSets(ns).Get(context.TODO(), oName, metav1.GetOptions{})
		if repErr != nil {
			panic(repErr.Error())
		}
		return replica.Spec.Template.Spec.Containers[0].Image
	case "DaemonSet":
		ds, err := clientset.AppsV1().DaemonSets(ns).Get(context.TODO(), oName, metav1.GetOptions{})
		if err != nil {
			panic(err)
		}
		return ds.Spec.Template.Spec.Containers[0].Image
	case "StatefulSet":
		sts, err := clientset.AppsV1().StatefulSets(ns).Get(context.TODO(), oName, metav1.GetOptions{})
		if err != nil {
			panic(err)
		}
		return sts.Spec.Template.Spec.Containers[0].Image
	default:
		fmt.Printf("Could not find resource manager for type %s\n", oKind)
		//continue
	}
	return ""
}

func majorMinor(symver string) string {
	versionSpilt := strings.Split(symver, ".")
	return fmt.Sprintf("%s.%s", versionSpilt[0], versionSpilt[1])
}

func repeatChar(char string, count int) string {
	out := ""
	for i := 0; i < count; i++ {
		out = fmt.Sprintf("%s%s", out, char)
	}
	return out
}

type nodeVersionInfo struct {
	name     string
	version  string
	upToDate bool
}
type patchStringValue struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value bool   `json:"value"`
}

func cordonNode(clientset *kubernetes.Clientset, nodeName string) {
	payload := []patchStringValue{{
		Op:    "replace",
		Path:  "/spec/unschedulable",
		Value: true,
	}}
	payloadBytes, _ := json.Marshal(payload)
	_, err := clientset.CoreV1().Nodes().Patch(context.TODO(), nodeName, k8stypes.StrategicMergePatchType, payloadBytes, v1.PatchOptions{})
	if err != nil {
		panic(err)
	}
}

func main() {
	// Parameters
	var kubeConfig *string
	var kubeContext string
	var namespace string
	var fixIt bool
	var versionListOnly bool
	var validateHelmSecrets bool
	var nukeHelmSecrets bool
	var debug bool

	var divider string
	var serverName string
	var requiredIstioVersion string
	var podsToRestart podsToRestartList
	var restarts ownerInfoList
	var upToDateNote string
	var nodesOutOfDate int

	home := homeDir()

	if home != "" {
		kubeConfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeConfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}

	flag.BoolVar(&debug, "d", false, "(optional) Debug")
	flag.BoolVar(&fixIt, "f", false, "(optional) Fix issues found")
	flag.BoolVar(&versionListOnly, "v", false, "(optional) Basic Version List Only")
	flag.BoolVar(&validateHelmSecrets, "s", false, "(optional) Validate Helm Secrets (can add considerable time)")
	flag.StringVar(&namespace, "n", "", "(optional) Namespace for affected checks/fixes")
	flag.BoolVar(&nukeHelmSecrets, "N", false, "(optional) Nuke ALL Helm Secrets")
	flag.StringVar(&kubeContext, "c", "", "(optional) Kubernetes Context to use")
	flag.Parse()

	configOverrides := &clientcmd.ConfigOverrides{}

	if kubeContext != "" {
		configOverrides = &clientcmd.ConfigOverrides{CurrentContext: kubeContext}
	}

	configLoadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: *kubeConfig}

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(configLoadingRules, configOverrides).ClientConfig()
	if err != nil {
		panic(err.Error())
	}

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	disClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	if *kubeConfig != "" {
		if kubeContext != "" {
			serverName = fmt.Sprintf("%s/%s", *kubeConfig, kubeContext)
		} else {
			serverName = fmt.Sprintf("%s", *kubeConfig)
		}
	} else {
		serverName = fmt.Sprintf("%s", kubeContext)
	}

	divider = fmt.Sprintf("%s%s%s\n", darkGray, repeatChar("=", 22+utf8.RuneCountInString(serverName)), normalColor)

	fmt.Printf("%sPulling data from %s...\n%s", normalColor, serverName, divider)

	serverVersionInfo, err := disClient.ServerVersion()
	if err != nil {
		panic(err)
	}
	serverVersion := strings.Split(serverVersionInfo.String(), "-")[0]

	fmt.Printf("%sKubernetes Server Version: %s%s%s\n", goodColor, white, serverVersion, normalColor)

	nodeVersions, isUpToDate := validateNodes(clientset, fmt.Sprintf("%s", serverVersion))
	if !isUpToDate {
		server_majmin := majorMinor(serverVersion)
		for i := 0; i < len(nodeVersions); i++ {
			node_majmin := majorMinor(nodeVersions[i].version)

			if server_majmin != node_majmin {
				nodesOutOfDate++
			}
		}
		upToDateNote = fmt.Sprintf("%s* %d of %d wrong %s", errorColor, nodesOutOfDate, len(nodeVersions), normalColor)
	}
	fmt.Printf("%s  Kubernetes Node Version: %s%s.x%s%s\n", goodColor, white, majorMinor(nodeVersions[0].version), upToDateNote, normalColor)

	findCiliumVersion(clientset, true)
	findNginxVersion(clientset, true)
	requiredIstioVersion = findIstioVersion(clientset, true)

	if !versionListOnly {
		fmt.Printf("\n%sNodes to drain for version Synchronization:\n%s", normalColor, divider)
		if !isUpToDate {
			server_majmin := majorMinor(serverVersion)

			for i := 0; i < len(nodeVersions); i++ {
				node_majmin := majorMinor(nodeVersions[i].version)
				if server_majmin != node_majmin {
					fmt.Printf("%s%s%s - %s%s%s\n", warningColor, nodeVersions[i].version, darkGray, white, nodeVersions[i].name, normalColor)

					if fixIt {
						//_, err := drain.CheckEvictionSupport(clientset)
						if err != nil {
							fmt.Printf("Eviction not supported on current cluster\n")
						} else {
							// Let's drain some nodes!
							cordonNode(clientset, nodeVersions[i].name)
						}
					} else {
						fmt.Printf("Would Drain %s\n", nodeVersions[i].name)
					}
				}
			}
		} else {
			fmt.Printf("%sAll nodes running correct version of Kubernetes %s\n", goodColor, normalColor)
		}

		if validateHelmSecrets {
			var secretIssueFound bool
			var helmSecretCount int
			secretIssueFound = false
			fmt.Printf("\nPulling Helm Secrets for validation...\n%s", divider)
			secretList, _ := clientset.CoreV1().Secrets(namespace).List(context.TODO(), metav1.ListOptions{})
			for _, s := range secretList.Items {
				if strings.Contains(s.Name, "sh.helm.release") {
					helmSecretCount++
					if (s.GetObjectMeta().GetLabels()["status"] == "pending-upgrade") || 
                       (s.GetObjectMeta().GetLabels()["status"] == "pending-install") ||
                       (s.GetObjectMeta().GetLabels()["status"] == "pending-rollback") {
						fmt.Printf("%s: %s -> %s", s.Namespace, s.Name, s.GetObjectMeta().GetLabels()["status"])
						secretIssueFound = true
						if fixIt {
							fmt.Printf(" ... FIXED!\n")
						} else {
							fmt.Printf("\n")
						}
					} else if debug {
						fmt.Printf("%s: %s -> %s\n", s.Namespace, s.Name, s.GetObjectMeta().GetLabels()["status"])
					}
				}
			}

			if !secretIssueFound {
				fmt.Printf("%sAll %d helm secrets are good!%s\n", goodColor, helmSecretCount, normalColor)
			} else {
				fmt.Printf("%sAbove issues found.%s\n", errorColor, normalColor)
			}
		}

		if nukeHelmSecrets {
			var helmSecrets = 0
			fmt.Printf("\nPulling Helm Secrets for deletion...\n%s", divider)
			secretList, _ := clientset.CoreV1().Secrets(namespace).List(context.TODO(), metav1.ListOptions{})
			for _, s := range secretList.Items {
				if strings.Contains(s.Name, "sh.helm.release") {
					helmSecrets++
				}
			}
			if fixIt {
				fmt.Printf("Nuking %d secrets.\n", helmSecrets)
				progressBar := progressbar.Default(int64(helmSecrets))
				for _, s := range secretList.Items {
					if strings.Contains(s.Name, "sh.helm.release") {
						clientset.CoreV1().Secrets(s.Namespace).Delete(context.TODO(), s.Name, metav1.DeleteOptions{})
						progressBar.Add(1)
					}
				}
			} else {
				fmt.Printf("Would nuke %d secrets.\n", helmSecrets)
				for _, s := range secretList.Items {
					if strings.Contains(s.Name, "sh.helm.release") {
						fmt.Printf("Would delete %s/%s\n", s.Namespace, s.Name)
					}
				}
			}
		}

		fmt.Printf("\n%sPods needing restart for Istio Synchronization: \n%s", normalColor, divider)

		podList, _ := clientset.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
		if len(podList.Items) == 0 {
			fmt.Printf("%sWe didn't find ANY pods. Did you forget to authenticate?", errorColor)
			os.Exit(61) //61 == No Data Available
		}

		for i := 0; i < len(podList.Items); i++ {
			var podName = podList.Items[i].Name
			var podNamespace = podList.Items[i].Namespace
			for c := 0; c < len(podList.Items[i].Spec.Containers); c++ {
				//var istioVersion string
				image := podList.Items[i].Spec.Containers[c].Image
				_, imageName, istioTag := breakoutImage(image)

				if imageName == "proxyupgrade" {
					if istioTag != requiredIstioVersion {
						fmt.Printf("%s%s%s: %s%s%s/%s%s%s\n", warningColor, istioTag, darkGray, normalColor, podNamespace, darkGray, white, podName, normalColor)
						pod := podList.Items[i]

						if len(pod.OwnerReferences) == 0 {
							fmt.Printf("Pod %s has no owner", pod.Name)
							continue
						}

						ownerName, ownerKind := findOwner(clientset, podNamespace, pod.OwnerReferences[0].Name, pod.OwnerReferences[0].Kind)

						podsToRestart = append(podsToRestart, podInfo{
							name:      podName,
							namespace: podNamespace,
							ownerName: ownerName,
							ownerKind: ownerKind})

						if !restarts.containsName(ownerName) {
							restarts = append(restarts, ownerInfo{
								name:      ownerName,
								namespace: podNamespace,
								kind:      ownerKind})
						}
					}
				}
			}
		}

		//sort.Sort(podsToRestart)
		if len(restarts) > 0 && !fixIt { // Only show info
			fmt.Printf("\n%sOwners to restart for Istio Synchronization: %d\n%s", normalColor, len(restarts), divider)
			for i := 0; i < len(restarts); i++ {
				fmt.Printf("%sWould restart: %s %s %s/%s %s\n", normalColor, white, restarts[i].kind, restarts[i].namespace, restarts[i].name, normalColor)
			}
		} else if len(restarts) > 0 && fixIt { // Show and Act
			fmt.Printf("\n%sOwners to restart for Istio Synchronization: %d\n%s", normalColor, len(restarts), divider)
			for i := 0; i < len(restarts); i++ {

				fmt.Printf("%sRestarting: %s %s %s/%s %s\n", normalColor, white, restarts[i].kind, restarts[i].namespace, restarts[i].name, normalColor)
				switch restarts[i].kind {
				case "Deployment":
					deploymentsClient := clientset.AppsV1().Deployments(restarts[i].namespace)
					data := fmt.Sprintf(`{"spec": {"template": {"metadata": {"annotations": {"kubectl.kubernetes.io/restartedAt": "%s"}}}}}`, time.Now().Format("20060102150405"))
					deployment, err := deploymentsClient.Patch(context.TODO(), restarts[i].name, k8stypes.StrategicMergePatchType, []byte(data), v1.PatchOptions{})
					if err != nil {
						fmt.Printf("%s %s/%s: FAILED! Could not restart Deployment: %s\n", errorColor, restarts[i].namespace, deployment, err)
					}
				case "DaemonSet":
					dsClient := clientset.AppsV1().DaemonSets(restarts[i].namespace)
					data := fmt.Sprintf(`{"spec": {"template": {"metadata": {"annotations": {"kubectl.kubernetes.io/restartedAt": "%s"}}}}}`, time.Now().Format("20060102150405"))
					ds, err := dsClient.Patch(context.TODO(), restarts[i].name, k8stypes.StrategicMergePatchType, []byte(data), v1.PatchOptions{})
					if err != nil {
						fmt.Printf("%s %s/%s: FAILED! Could not restart DaemonSet: %s\n", errorColor, restarts[i].namespace, ds, err)
					}
				case "StatefulSet":
					stsClient := clientset.AppsV1().StatefulSets(restarts[i].namespace)
					data := fmt.Sprintf(`{"spec": {"template": {"metadata": {"annotations": {"kubectl.kubernetes.io/restartedAt": "%s"}}}}}`, time.Now().Format("20060102150405"))
					sts, err := stsClient.Patch(context.TODO(), restarts[i].name, k8stypes.StrategicMergePatchType, []byte(data), v1.PatchOptions{})
					if err != nil {
						fmt.Printf("%s %s/%s: FAILED! Could not restart StatefulSet: %s\n", errorColor, restarts[i].namespace, sts, err)
					}
				case "Job":
					fmt.Printf("%s %s/%s: Jobs not supported! \n", warningColor, restarts[i].namespace, restarts[i].name)
				}
			}
		}

		if len(restarts) == 0 {
			fmt.Printf("%sAll pods running correct version of Istio %s\n", goodColor, normalColor)
		}
	}
	fmt.Printf("%s", normalColor)
}

func (o ownerInfoList) containsName(n string) bool {
	for i := 0; i < len(o); i++ {
		if o[i].name == n {
			return true
		}
	}
	return false
}

// func (e podsToRestartList) Len() int {
// 	return len(e)
// }

// func (a podsToRestartList) Less(i, j int) bool {
// 	x := a[i].name[len(a[i].name)-5 : len(a[i].name)]
// 	y := a[j].name[len(a[j].name)-5 : len(a[j].name)]
// 	return (x < y)
// }

// func (a podsToRestartList) Swap(i, j int) { a[i], a[j] = a[j], a[i] }

// func restartPod(cs kubernetes.Clientset, pod podInfo) {
// 	fmt.Printf("Restarting %s -> %s\n", pod.namespace, pod.name)
// 	cs.CoreV1().Pods(pod.namespace).Delete(context.TODO(), pod.name, metav1.DeleteOptions{})
// 	time.Sleep(time.Second * 30)
// }
