package main

import (
	"time"
	// "flag"
	"net/http"
	"os"
	"fmt"
	"log"
	"context"
	"io"
	"bytes"
	"bufio"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"k8s.io/client-go/rest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	// "k8s.io/client-go/tools/clientcmd"
)

const metricPort = 8081
const keep_alive_sec = 300
const savePath string = "/home/app/podLog"


func main() {
	log.Println("Start running faas container idler.")

	/*
	kubeconfig := flag.String("kubeconfig", "/home/suyi/.kube/config", "location to kubeconfig file")
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	clientset, err := kubernetes.NewForConfig(config)
    if err != nil {
        panic(err.Error())
    }
	*/

	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	// creates k8s clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	
	podMap := make(map[string][]int64)

	reconcileInterval := time.Second * 10

	client := &http.Client{}

	/*
	inactivityDurationVal := os.Getenv("inactivity_duration")
	if len(inactivityDurationVal) == 0 {
		inactivityDurationVal = "5m"
	}
	inactivityDuration, _ := time.ParseDuration(inactivityDurationVal)
	*/

	for {
		reconcile(client, clientset, podMap)
		time.Sleep(reconcileInterval)
		log.Printf("\n")
	}
}



func reconcile(client *http.Client, clientset *kubernetes.Clientset, podMap map[string][]int64) {
	endpoints, err := clientset.CoreV1().Endpoints("openfaas-fn").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	
	for _, endpoint := range endpoints.Items{
		funcName := endpoint.ObjectMeta.Name
		// fmt.Println("endpointName: ", funcName)
		// fmt.Println("endpoint level: ", ep_idx , endpoint)
		// fmt.Println("subset level: ", ep_idx , endpoint.Subsets)
		for _, subset := range endpoint.Subsets{
			// fmt.Println("address level: ", add_idx ,add.Addresses)
			for _, address := range(subset.Addresses) {
				log.Println("IP level: " , address.IP, address.TargetRef.Name)
				metricURL := fmt.Sprintf("http://%s:%d/metrics", address.IP, metricPort)
				resp, err := client.Get(metricURL)
				if err != nil {
					panic(err.Error())
				}
				defer resp.Body.Close()

				mf, err := parseMF(resp.Body)
				if err != nil {
					log.Fatal(err)
				}


				var total 	 float64
				var inflight float64 

				totalKey := "http_requests_total"
				totalMetrics, found := mf[totalKey] 
				if !found {
					total = 0.0
				} else {
					total = totalMetrics.Metric[0].GetCounter().GetValue()
				}

				inflightKey := "http_requests_in_flight"
				inflightMetrics, found := mf[inflightKey]
				if !found {
					log.Println("http_requests_in_flight not found.")
				} else {
					inflight = inflightMetrics.Metric[0].GetCounter().GetValue()
				}

				log.Println(totalKey, total, inflightKey, inflight)
				getReplicas(funcName, clientset)

				history, found := podMap[address.TargetRef.Name]

				if !found {
					podMap[address.TargetRef.Name] = make([]int64, 2)
					podMap[address.TargetRef.Name][0] = int64(total)
					podMap[address.TargetRef.Name][1] = time.Now().Unix()
				} else {
					log.Println("total: ", history[0], "last: ", history[1])
					ifRemove := getIfRemove(int64(total), int64(inflight), history)
					if ifRemove {
						log.Println("Removing idle pod: ", address.TargetRef.Name, ", Idle From: ", time.Unix(history[1], 0).Format("2006-01-02_15:04:05"), ", To: ", time.Now().Format("2006-01-02_15:04:05"))
						/*
						1. Save logs
						2. Label deleteCost
						3. ScaleDownByOne
						*/
						savePodLog(address.TargetRef.Name, clientset)
						labelDeleteCost(address.TargetRef.Name, clientset)
						scaleDownbyOne(funcName, clientset)
					}
				}

			}
		}
	}
	
	
}

func labelDeleteCost(podName string, clientset *kubernetes.Clientset) {
	pod, _ := clientset.CoreV1().Pods("openfaas-fn").Get(context.TODO(), podName, metav1.GetOptions{}) 

	newPod := pod.DeepCopy()
	ann := newPod.ObjectMeta.Annotations
	ann["controller.kubernetes.io/pod-deletion-cost"] = "-100"
	newPod.ObjectMeta.Annotations = ann

	_, err := clientset.CoreV1().Pods(newPod.ObjectMeta.Namespace).Update(context.TODO(), newPod, metav1.UpdateOptions{})
	if err != nil {
		log.Println(err)
	}

	log.Println("Pod annotation updated: ", pod.ObjectMeta.Name)

}

func scaleDownbyOne(funcName string, clientset *kubernetes.Clientset) {
	deployment, err := clientset.AppsV1().Deployments("openfaas-fn").Get(context.TODO(), funcName, metav1.GetOptions{})
	if err != nil {
        panic(err.Error())
    }
	oldReplicas := *deployment.Spec.Replicas
	newReplicas := int32(oldReplicas - 1)

	log.Printf("Set replicas - %s %s, %d -> %d\n", funcName, "openfaas-fn", oldReplicas, newReplicas)
	
	deployment.Spec.Replicas = &newReplicas
	_, err = clientset.AppsV1().Deployments("openfaas-fn").Update(context.TODO(), deployment, metav1.UpdateOptions{})
	if err != nil {
		log.Println(err)
		panic(err.Error())
	}
}

func savePodLog(podName string, clientset *kubernetes.Clientset) {
	podLogOpts := corev1.PodLogOptions{}
	req := clientset.CoreV1().Pods("openfaas-fn").GetLogs(podName, &podLogOpts)
	podLogs, err := req.Stream(context.TODO())
	if err != nil {
        log.Println("error in opening stream") 
		panic(err.Error())
    }
	defer podLogs.Close()
	buf := new(bytes.Buffer)
    _, err = io.Copy(buf, podLogs)
    if err != nil {
         log.Println("error in copy information from podLogs to buf")
		 panic(err.Error())
    }
    str := buf.String()
	// log.Println("Pod Logs: ")
	// log.Println(str)


	filePath := fmt.Sprintf("%s/%s_%s.log", savePath, podName, time.Now().Format("2006-01-02_15:04:05"))
	f, err := os.Create(filePath)
	if err != nil {
		log.Println("Podlog write error.")
		panic(err.Error())
	}

	defer f.Close()

	w := bufio.NewWriter(f)
	_, err = w.WriteString(str)
	if err != nil {
		log.Println("Podlog write error.")
		panic(err.Error())
	}
	w.Flush()
}

func getReplicas(funcName string, clientset *kubernetes.Clientset) {
	deployment, err := clientset.AppsV1().Deployments("openfaas-fn").Get(context.TODO(), funcName, metav1.GetOptions{})
	if err != nil {
        panic(err.Error())
    }
	Replicas := *deployment.Spec.Replicas
	log.Println("Func: ", funcName, "Replica: ", Replicas)
}


func getIfRemove(total int64, inflight int64, history []int64) bool {
	if inflight == 0 {
		if history[0] == total {
			idleDuration := time.Now().Unix() - history[1]
			if idleDuration >= keep_alive_sec {
				return true
			} else {
				return false
			}
		} else if history[0] < total {
			// means new requests completed in the past reconcile period
			history[0] = total
			history[1] = time.Now().Unix()
			return false
		} else {
			log.Fatal("historic total number of req exceeds the current one, impossible! ")
		}
	} else {
		history[1] = time.Now().Unix()
		return false
	}

	return false
}


func parseMF(reader io.Reader) (map[string]*dto.MetricFamily, error) {
    var parser expfmt.TextParser
    mf, err := parser.TextToMetricFamilies(reader)
    if err != nil {
        return nil, err
    }
    return mf, nil
}
