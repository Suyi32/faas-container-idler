package main

import (
	"time"
	"flag"
	"log"
	"context"
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

	reconcileInterval := time.Second * 3
	saveFrequency     := 2

	log.Println("Start running VM Timer.")
	log.Println("Reconcile Interval: ", reconcileInterval)
	log.Println("Save Frequency: ", saveFrequency)
	log.Printf("\n")

	counter := 0
	for {
		reconcile(clientset, nodeTimerMap)
		time.Sleep(reconcileInterval)
		
		counter += 1
		if counter % saveFrequency == 0 {
			//print and save timer information
			getResults(nodeTimerMap)
		}
		// log.Printf("\n")
	}
}

func reconcile(clientset *kubernetes.Clientset, nodeTimerMap map[string]*nodeTimer) {
	nodes, err := clientset.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}

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

func getResults(nodeTimerMap map[string]*nodeTimer) {
	log.Printf("\n")
	var sum int64 = 0
	for _, nodeTimer := range nodeTimerMap {
		// log.Println(nodeName, fmt.Sprint(nodeTimer))
		sum += nodeTimer.Duration
	}
	log.Println("Total VM time: ", sum)

	log.Printf("\n")
}