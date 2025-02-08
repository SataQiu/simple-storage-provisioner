package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	k8serror "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"sigs.k8s.io/sig-storage-lib-external-provisioner/v10/controller"
)

var (
	BaseDir = "/var/my-storage-provisioner"
)

type ActionType string

const (
	ActionTypeCreate = "create"
	ActionTypeDelete = "delete"
)

type HostPathProvisioner struct {
	ctx        context.Context
	kubeClient *clientset.Clientset
	namespace  string
}

func NewProvisioner(ctx context.Context, kubeClient *clientset.Clientset) (*HostPathProvisioner, error) {
	namespace := "default"
	if ns := os.Getenv("POD_NAMESPACE"); ns != "" {
		namespace = ns
	}
	return &HostPathProvisioner{
		ctx:        ctx,
		kubeClient: kubeClient,
		namespace:  namespace,
	}, nil
}

func (p *HostPathProvisioner) Provision(_ context.Context, opts controller.ProvisionOptions) (*v1.PersistentVolume, controller.ProvisioningState, error) {
	folderName := strings.Join([]string{opts.PVName, opts.PVC.Namespace, opts.PVC.Name}, "_")
	fullPath := filepath.Join(BaseDir, folderName)

	nodeName := ""
	if opts.SelectedNode != nil {
		nodeName = opts.SelectedNode.Name
	}

	scheduledNodeName, err := p.createHelperPod(ActionTypeCreate, opts.PVName, fullPath, nodeName)
	if err != nil {
		return nil, controller.ProvisioningFinished, err
	}

	fs := v1.PersistentVolumeFilesystem
	hostPathType := v1.HostPathDirectoryOrCreate

	return &v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: opts.PVName,
		},
		Spec: v1.PersistentVolumeSpec{
			PersistentVolumeReclaimPolicy: *opts.StorageClass.ReclaimPolicy,
			AccessModes:                   opts.PVC.Spec.AccessModes,
			VolumeMode:                    &fs,
			Capacity: v1.ResourceList{
				v1.ResourceStorage: opts.PVC.Spec.Resources.Requests[v1.ResourceStorage],
			},
			PersistentVolumeSource: v1.PersistentVolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: fullPath,
					Type: &hostPathType,
				},
			},
			NodeAffinity: &v1.VolumeNodeAffinity{
				Required: &v1.NodeSelector{
					NodeSelectorTerms: []v1.NodeSelectorTerm{
						{
							MatchExpressions: []v1.NodeSelectorRequirement{
								{
									Key:      v1.LabelHostname,
									Operator: v1.NodeSelectorOpIn,
									Values: []string{
										scheduledNodeName,
									},
								},
							},
						},
					},
				},
			},
		},
	}, controller.ProvisioningFinished, nil
}

func (p *HostPathProvisioner) Delete(_ context.Context, pv *v1.PersistentVolume) (err error) {
	path, node, err := p.getPathAndNodeForPV(pv)
	if err != nil {
		return err
	}

	if pv.Spec.PersistentVolumeReclaimPolicy != v1.PersistentVolumeReclaimRetain {
		if _, err := p.createHelperPod(ActionTypeDelete, pv.Name, path, node); err != nil {
			return err
		}
	}

	return nil
}

func (p *HostPathProvisioner) createHelperPod(action ActionType, pvName, fullPath, nodeName string) (scheduledNodeName string, err error) {
	cmd := fmt.Sprintf("mkdir -m 0777 -p %s", fullPath)
	if action == ActionTypeDelete {
		cmd = fmt.Sprintf("rm -rf %s", fullPath)
	}
	helperPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("my-storage-provisioner-helper-%s-%s", action, pvName),
			Namespace: p.namespace,
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:    "provisioner",
					Image:   "busybox:stable",
					Command: []string{"sh", "-c", cmd},
					VolumeMounts: []v1.VolumeMount{
						{
							Name:      "hostpath-volume",
							MountPath: BaseDir,
						},
					},
				},
			},
			Volumes: []v1.Volume{
				{
					Name: "hostpath-volume",
					VolumeSource: v1.VolumeSource{
						HostPath: &v1.HostPathVolumeSource{
							Path: BaseDir,
						},
					},
				},
			},
			NodeName:      nodeName,
			RestartPolicy: v1.RestartPolicyNever,
		},
	}

	if len(helperPod.Name) > 128 {
		helperPod.Name = helperPod.Name[:128]
	}

	_, err = p.kubeClient.CoreV1().Pods(helperPod.Namespace).Create(context.TODO(), helperPod, metav1.CreateOptions{})
	if err != nil && !k8serror.IsAlreadyExists(err) {
		return "", err
	}

	defer func() {
		e := p.kubeClient.CoreV1().Pods(helperPod.Namespace).Delete(context.TODO(), helperPod.Name, metav1.DeleteOptions{})
		if e != nil {
			klog.Errorf("unable to delete the helper pod: %v", e)
		}
	}()

	completed, timeoutSeconds, scheduledNodeName := false, 120, ""
	for i := 0; i < timeoutSeconds; i++ {
		if pod, err := p.kubeClient.CoreV1().Pods(helperPod.Namespace).Get(context.TODO(), helperPod.Name, metav1.GetOptions{}); err != nil {
			return "", err
		} else if pod.Status.Phase == v1.PodSucceeded {
			completed = true
			scheduledNodeName = pod.Spec.NodeName
			break
		}
		time.Sleep(1 * time.Second)
	}
	if !completed {
		return "", fmt.Errorf("create process timeout after %v seconds", timeoutSeconds)
	}

	return scheduledNodeName, nil
}

func (p *HostPathProvisioner) getPathAndNodeForPV(pv *v1.PersistentVolume) (path, node string, err error) {
	if pv.Spec.PersistentVolumeSource.HostPath != nil {
		path = pv.Spec.PersistentVolumeSource.HostPath.Path
	}

	nodeAffinity := pv.Spec.NodeAffinity
	if nodeAffinity == nil {
		return "", "", fmt.Errorf("no NodeAffinity set")
	}
	required := nodeAffinity.Required
	if required == nil {
		return "", "", fmt.Errorf("no NodeAffinity.Required set")
	}

	node = ""
	for _, selectorTerm := range required.NodeSelectorTerms {
		for _, expression := range selectorTerm.MatchExpressions {
			if expression.Key == v1.LabelHostname && expression.Operator == v1.NodeSelectorOpIn {
				if len(expression.Values) != 1 {
					return "", "", fmt.Errorf("multiple values for the node affinity")
				}
				node = expression.Values[0]
				break
			}
		}
		if node != "" {
			break
		}
	}
	if node == "" || path == "" {
		return "", "", fmt.Errorf("cannot find path or affinited node")
	}
	return path, node, nil
}
