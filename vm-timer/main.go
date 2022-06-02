package main

import (
	"time"
	"flag"
	"log"
	"context"
	"strings"
	// "encoding/json"
	// "fmt"

	// "k8s.io/client-go/rest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type nodeTimer struct {
    Status		bool
    LastStamp 	int64
	Duration	int64
}

type podTimer struct {
    LastStamp 	int64
	Duration	int64
}

var Functions [10]string = [10]string{"get-duration", "get-media-meta", "query-vacancy", "reserve-spot",
										"classify-image-ts", "detect-object-ts", "detect-anomaly", "ingest-data",
											"anonymize-log", "filter-log"}

func main() {

	kubeconfig := flag.String("kubeconfig", "/etc/kubernetes/kubelet.conf", "location to kubeconfig file")
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	clientset, err := kubernetes.NewForConfig(config)
    if err != nil {
        panic(err.Error())
    }

	/*
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	// creates k8s clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	*/
	
	nodeTimerMap := make(map[string]*nodeTimer)
	podTimerMap := make(map[string]*podTimer)

	reconcileInterval := time.Second * 2
	saveFrequency     := 1

	log.Println("Start running VM Timer.")
	log.Println("Reconcile Interval: ", reconcileInterval)
	log.Println("Save Frequency: ", saveFrequency)
	log.Printf("\n")

	counter := 0
	for {
		reconcile(clientset, nodeTimerMap, podTimerMap)
		time.Sleep(reconcileInterval)
		
		counter += 1
		if counter % saveFrequency == 0 {
			//print and save timer information
			getResults(nodeTimerMap, podTimerMap)
		}
		// log.Printf("\n")
	}
}

func reconcile(clientset *kubernetes.Clientset, nodeTimerMap map[string]*nodeTimer, podTimerMap map[string]*podTimer) {
	nodes, err := clientset.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	var podName string

	for _, node := range nodes.Items{
		nodeName := node.Name
		// log.Println(node_idx, " Node name: ", nodeName)

		pods, err := clientset.CoreV1().Pods("openfaas-fn").List(context.Background(), metav1.ListOptions{FieldSelector: "spec.nodeName="+nodeName})
		if err != nil {
			panic(err.Error())
		}

		funcCounter := 0
		for _, pod := range (*pods).Items {
			_, ok := pod.Labels["faas_function"]
			if ok {
				funcCounter += 1
				// log.Println(pod_idx, " appName: ", appName)
				podName = pod.ObjectMeta.GetName()
				curPodTimer, ok := podTimerMap[podName]
				if !ok {
					log.Println(podName, "not found. Will create a entry in the map.")
					podTimerMap[podName] = &podTimer {
						LastStamp: time.Now().Unix(), 	
						Duration: 0,
					}
				} else {
					curPodTimer.Duration += time.Now().Unix() - curPodTimer.LastStamp
					curPodTimer.LastStamp =  time.Now().Unix()
				}
			}
		}
		
		curNodeTimer, ok := nodeTimerMap[nodeName]
		if !ok {
			log.Println(nodeName, "not found. Will create a entry in the map.")
			nodeTimerMap[nodeName] = &nodeTimer {
				Status: funcCounter != 0,
				LastStamp: time.Now().Unix(), 	
				Duration: 0,
			}
		}
		
		curNodeTimer = nodeTimerMap[nodeName]

		if curNodeTimer.Status {
			// last status is running
			// log.Println("Previous status: running")
			curNodeTimer.Status    = (funcCounter != 0)
			curNodeTimer.Duration  += time.Now().Unix() - curNodeTimer.LastStamp
			curNodeTimer.LastStamp = time.Now().Unix() 
		} else {
			// last status is idle
			// log.Println("Previous status: idle")
			curNodeTimer.Status    = (funcCounter != 0)
			curNodeTimer.LastStamp =  time.Now().Unix()
		}
		// log.Println("in func ", fmt.Sprint(*nodeTimerMap[nodeName]))
		if curNodeTimer.Duration != 0 {
			log.Printf("Node: %s: %d func pods.\n", nodeName, funcCounter)
		}
	}

}

func getResults(nodeTimerMap map[string]*nodeTimer, podTimerMap map[string]*podTimer) {
	log.Printf("\n")
	var sum int64 = 0
	for _, nodeTimer := range nodeTimerMap {
		// log.Println(nodeName, fmt.Sprint(nodeTimer))
		sum += nodeTimer.Duration
	}
	log.Println("[TotalVMTime]: ", sum)

	var functionMap map[string]int64 = make(map[string]int64)
	var parts []string
	var funcName string
	for podName, _ := range podTimerMap {
		parts = strings.Split(podName, "-")
		funcName = strings.Join(parts[:len(parts)-2], "-")

		functionMap[funcName] += podTimerMap[podName].Duration
    }

	for funcKey, _ := range functionMap {
		log.Println("[TotalFuncTime]:", funcKey, functionMap[funcKey])
    }
	
	log.Printf("\n")
}