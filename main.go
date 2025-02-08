package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"sigs.k8s.io/sig-storage-lib-external-provisioner/v10/controller"
)

const (
	DefaultProvisionerName = "example.io/my-storage-provisioner"
)

func loadKubeConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	return rest.InClusterConfig()
}

func registerShutdownChannel(cancelFn context.CancelFunc) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		cancelFn()
	}()
}

func main() {
	var kubeconfig string
	flag.StringVar(&kubeconfig, "kubeconfig", "", "location to your kubeconfig file")
	flag.Parse()

	config, err := loadKubeConfig(kubeconfig)
	if err != nil {
		klog.Fatalf("failed to load kubeconfig: %v", err)
	}

	kubeClient, err := clientset.NewForConfig(config)
	if err != nil {
		klog.Fatalf("error creating Kubernetes client: %v", err)
	}

	ctx, cancelFn := context.WithCancel(context.Background())
	defer cancelFn()
	registerShutdownChannel(cancelFn)

	provisioner, err := NewProvisioner(ctx, kubeClient)
	if err != nil {
		klog.Fatalf("error creating storage provisioner: %v", err)
	}

	pc := controller.NewProvisionController(
		klog.FromContext(ctx),
		kubeClient,
		DefaultProvisionerName,
		provisioner,
		controller.LeaderElection(false),
		controller.FailedProvisionThreshold(3),
		controller.FailedDeleteThreshold(3),
		controller.Threadiness(3),
	)
	pc.Run(ctx)
}
